/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { zodResolver } from '@hookform/resolvers/zod'
import { Bot, CreditCard, Settings2 } from 'lucide-react'
import { useQuery } from '@tanstack/react-query'
import { useEffect, useState } from 'react'
import { useForm, type Resolver } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import {
  SideDrawerSection,
  sideDrawerContentClassName,
  sideDrawerFooterClassName,
  sideDrawerFormClassName,
  sideDrawerHeaderClassName,
  sideDrawerSwitchItemClassName,
} from '@/components/drawer-layout'
import { MultiSelect, type Option } from '@/components/multi-select'
import { Button } from '@/components/ui/button'
import {
  Form,
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import {
  Sheet,
  SheetClose,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from '@/components/ui/sheet'
import { Switch } from '@/components/ui/switch'

import { getEnabledModels } from '@/features/channels/api'
import {
  createPlan,
  updatePlan,
  createWaffoPancakeSubscriptionProduct,
  listWaffoPancakeSubscriptionProductOptions,
} from '../api'
import {
  getPlanFormSchema,
  PLAN_FORM_DEFAULTS,
  planToFormValues,
  formValuesToPlanPayload,
  type PlanFormValues,
} from '../lib'
import type { PlanRecord } from '../types'
import { useSubscriptions } from './subscriptions-provider'

interface Props {
  open: boolean
  onOpenChange: (open: boolean) => void
  currentRow?: PlanRecord
}

export function SubscriptionsMutateDrawer({
  open,
  onOpenChange,
  currentRow,
}: Props) {
  const { t } = useTranslation()
  const isEdit = !!currentRow?.plan?.id
  const { triggerRefresh } = useSubscriptions()
  const [isSubmitting, setIsSubmitting] = useState(false)
  const [creatingPancakeProduct, setCreatingPancakeProduct] = useState(false)
  const [pancakeProducts, setPancakeProducts] = useState<
    { id: string; name: string; status: string }[]
  >([])

  const schema = getPlanFormSchema(t)
  const form = useForm<PlanFormValues>({
    resolver: zodResolver(schema) as unknown as Resolver<PlanFormValues>,
    defaultValues: PLAN_FORM_DEFAULTS,
  })

  useEffect(() => {
    if (open) {
      if (currentRow?.plan) {
        form.reset(planToFormValues(currentRow.plan))
      } else {
        form.reset(PLAN_FORM_DEFAULTS)
      }
      // Best-effort — empty list still lets the operator use "+ Create".
      listWaffoPancakeSubscriptionProductOptions()
        .then((res) => {
          if (
            res.message === 'success' &&
            typeof res.data === 'object' &&
            res.data &&
            Array.isArray((res.data as { products?: unknown }).products)
          ) {
            setPancakeProducts(
              (res.data as { products: typeof pancakeProducts }).products
            )
          } else {
            setPancakeProducts([])
          }
        })
        .catch(() => setPancakeProducts([]))
    }
  }, [open, currentRow, form])

  // Gate "+ Create on Pancake" on the same checks the mint handler runs.
  const watchedTitle = form.watch('title')
  const watchedPrice = form.watch('price_amount')
  const pancakeCreateReady =
    typeof watchedTitle === 'string' &&
    watchedTitle.trim().length > 0 &&
    Number(watchedPrice ?? 0) > 0

  const payStripe = form.watch('pay_stripe_enabled')
  const payCreem = form.watch('pay_creem_enabled')
  const payWaffo = form.watch('pay_waffo_enabled')

  // Model options come from the enabled channels (abilities table) so a plan
  // can only grant models the backend actually routes.
  const enabledModelsQuery = useQuery({
    queryKey: ['channel_models_enabled'],
    queryFn: getEnabledModels,
    enabled: open,
  })
  const modelOptions: Option[] = (enabledModelsQuery.data?.data ?? []).map(
    (m) => ({ label: m, value: m })
  )

  const onSubmit = async (values: PlanFormValues) => {
    setIsSubmitting(true)
    try {
      const payload = formValuesToPlanPayload(values)
      if (isEdit && currentRow?.plan?.id) {
        const res = await updatePlan(currentRow.plan.id, payload)
        if (res.success) {
          toast.success(t('Update succeeded'))
          onOpenChange(false)
          triggerRefresh()
        }
      } else {
        const res = await createPlan(payload)
        if (res.success) {
          toast.success(t('Create succeeded'))
          onOpenChange(false)
          triggerRefresh()
        }
      }
    } catch {
      toast.error(t('Request failed'))
    } finally {
      setIsSubmitting(false)
    }
  }

  // Mints a Pancake OnetimeProduct (not SubscriptionProduct — see
  // controller) using persisted creds + the form's title/price, then
  // pins the returned PROD_ ID into the form field.
  const handleCreatePancakeProduct = async () => {
    const title = form.getValues('title').trim()
    const priceAmount = Number(form.getValues('price_amount') || 0)
    if (!title) {
      toast.error(t('Plan title is required'))
      return
    }
    if (priceAmount <= 0) {
      toast.error(t('Plan price must be greater than zero'))
      return
    }
    setCreatingPancakeProduct(true)
    try {
      const res = await createWaffoPancakeSubscriptionProduct({
        name: title,
        amount: priceAmount.toFixed(2),
      })
      if (
        res.message === 'success' &&
        typeof res.data === 'object' &&
        res.data
      ) {
        const created = res.data as { product_id: string; product_name: string }
        form.setValue('waffo_pancake_product_id', created.product_id, {
          shouldDirty: true,
        })
        // Refetch from GraphQL so the dropdown reflects authoritative state.
        try {
          const refresh = await listWaffoPancakeSubscriptionProductOptions()
          if (
            refresh.message === 'success' &&
            typeof refresh.data === 'object' &&
            refresh.data &&
            Array.isArray((refresh.data as { products?: unknown }).products)
          ) {
            setPancakeProducts(
              (refresh.data as { products: typeof pancakeProducts }).products
            )
          }
        } catch {
          // Best-effort — form value already points at the new product;
          // raw-ID fallback covers the missing label.
        }
        toast.success(
          `${t('Waffo Pancake product created')}: ${created.product_id}`
        )
      } else {
        const reason = typeof res.data === 'string' ? res.data : undefined
        toast.error(
          reason
            ? `${t('Waffo Pancake product creation failed')}: ${reason}`
            : t('Waffo Pancake product creation failed')
        )
      }
    } catch (err) {
      toast.error(
        `${t('Waffo Pancake product creation failed')}: ${err instanceof Error ? err.message : String(err)}`
      )
    } finally {
      setCreatingPancakeProduct(false)
    }
  }

  const quotaLimitField = (
    name: 'five_hour_limit' | 'weekly_limit' | 'monthly_limit',
    label: string,
    description: string
  ) => (
    <FormField
      control={form.control}
      name={name}
      render={({ field }) => (
        <FormItem>
          <FormLabel>{label}</FormLabel>
          <FormControl>
            <Input
              {...field}
              type='number'
              min={0}
              onChange={(e) =>
                field.onChange(Number.parseInt(e.target.value, 10) || 0)
              }
            />
          </FormControl>
          <FormDescription>{description}</FormDescription>
          <FormMessage />
        </FormItem>
      )}
    />
  )

  return (
    <Sheet
      open={open}
      onOpenChange={(v) => {
        onOpenChange(v)
        if (!v) {
          form.reset()
        }
      }}
    >
      <SheetContent className={sideDrawerContentClassName('sm:max-w-[600px]')}>
        <SheetHeader className={sideDrawerHeaderClassName()}>
          <SheetTitle>
            {isEdit ? t('Update plan info') : t('Create new subscription plan')}
          </SheetTitle>
          <SheetDescription>
            {isEdit
              ? t('Modify existing subscription plan configuration')
              : t(
                  'Fill in the following info to create a new subscription plan'
                )}
          </SheetDescription>
        </SheetHeader>
        <Form {...form}>
          <form
            id='subscription-form'
            onSubmit={form.handleSubmit(onSubmit)}
            className={sideDrawerFormClassName()}
          >
            {/* Basic Info */}
            <SideDrawerSection>
              <h3 className='flex items-center gap-2 text-sm font-medium'>
                <Settings2 className='h-4 w-4' />
                {t('Basic Info')}
              </h3>

              <FormField
                control={form.control}
                name='title'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('Plan Title')}</FormLabel>
                    <FormControl>
                      <Input {...field} placeholder={t('e.g. Basic Plan')} />
                    </FormControl>
                    <FormMessage />
                  </FormItem>
                )}
              />

              <div className='grid grid-cols-1 gap-3 sm:grid-cols-2'>
                <FormField
                  control={form.control}
                  name='price_amount'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>{t('Plan Price')}</FormLabel>
                      <FormControl>
                        <Input
                          {...field}
                          type='number'
                          step='0.01'
                          min={0}
                          onChange={(e) =>
                            field.onChange(Number.parseFloat(e.target.value) || 0)
                          }
                        />
                      </FormControl>
                      <FormDescription>
                        {t(
                          'Amount the user pays to purchase this plan; the actual currency depends on the payment gateway.'
                        )}
                      </FormDescription>
                      <FormMessage />
                    </FormItem>
                  )}
                />

                <FormField
                  control={form.control}
                  name='plan_level'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>{t('Plan Level')}</FormLabel>
                      <Select
                        items={[
                          { value: 'lite', label: t('Lite (入门版)') },
                          { value: 'plus', label: t('Plus (标准版)') },
                          { value: 'max', label: t('Max (旗舰版)') },
                        ]}
                        onValueChange={field.onChange}
                        value={field.value || 'plus'}
                      >
                        <FormControl>
                          <SelectTrigger>
                            <SelectValue placeholder={t('Plus (标准版)')} />
                          </SelectTrigger>
                        </FormControl>
                        <SelectContent alignItemWithTrigger={false}>
                          <SelectGroup>
                            <SelectItem value='lite'>
                              {t('Lite (入门版)')}
                            </SelectItem>
                            <SelectItem value='plus'>
                              {t('Plus (标准版)')}
                            </SelectItem>
                            <SelectItem value='max'>
                              {t('Max (旗舰版)')}
                            </SelectItem>
                          </SelectGroup>
                        </SelectContent>
                      </Select>
                      <FormDescription>
                        {t('用于升级排序：lite < plus < max；不允许降级')}
                      </FormDescription>
                      <FormMessage />
                    </FormItem>
                  )}
                />
              </div>
            </SideDrawerSection>

            {/* Models & Quota */}
            <SideDrawerSection>
              <h3 className='flex items-center gap-2 text-sm font-medium'>
                <Bot className='h-4 w-4' />
                {t('Models & Quota')}
              </h3>

              <FormField
                control={form.control}
                name='allowed_models'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('Allowed models')}</FormLabel>
                    <FormControl>
                      <MultiSelect
                        options={modelOptions}
                        selected={field.value ?? []}
                        onChange={field.onChange}
                        allowCreate
                        placeholder={t('Search or add models...')}
                      />
                    </FormControl>
                    <FormDescription>
                      {t(
                        'Models come from your configured channels; you can also type a custom model name. Empty means no restriction.'
                      )}
                    </FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />

              <div className='grid grid-cols-1 gap-3 sm:grid-cols-3'>
                {quotaLimitField(
                  'five_hour_limit',
                  t('5-hour quota cap'),
                  t('0 = 不限（每 5 小时桶）')
                )}
                {quotaLimitField(
                  'weekly_limit',
                  t('Weekly quota cap'),
                  t('0 = 不限（自然周）')
                )}
                {quotaLimitField(
                  'monthly_limit',
                  t('Monthly quota cap'),
                  t('0 = 不限（购买日起 30 天滚动）；>0 时超量拒绝请求')
                )}
              </div>
            </SideDrawerSection>

            {/* Payment */}
            <SideDrawerSection>
              <h3 className='flex items-center gap-2 text-sm font-medium'>
                <CreditCard className='h-4 w-4' />
                {t('Third-party Payment Config')}
              </h3>

              <div className='flex flex-col gap-3'>
                <FormField
                  control={form.control}
                  name='allow_balance_pay'
                  render={({ field }) => (
                    <FormItem className={sideDrawerSwitchItemClassName()}>
                      <FormLabel className='!mt-0'>
                        {t('Allow balance redemption')}
                      </FormLabel>
                      <FormControl>
                        <Switch
                          checked={field.value}
                          onCheckedChange={field.onChange}
                        />
                      </FormControl>
                    </FormItem>
                  )}
                />

                <FormField
                  control={form.control}
                  name='pay_stripe_enabled'
                  render={({ field }) => (
                    <FormItem className={sideDrawerSwitchItemClassName()}>
                      <FormLabel className='!mt-0'>
                        {t('Enable Stripe payment')}
                      </FormLabel>
                      <FormControl>
                        <Switch
                          checked={field.value}
                          onCheckedChange={field.onChange}
                        />
                      </FormControl>
                    </FormItem>
                  )}
                />
                {payStripe && (
                  <FormField
                    control={form.control}
                    name='stripe_price_id'
                    render={({ field }) => (
                      <FormItem>
                        <FormLabel>Stripe Price ID</FormLabel>
                        <FormControl>
                          <Input {...field} placeholder='price_...' />
                        </FormControl>
                        <FormMessage />
                      </FormItem>
                    )}
                  />
                )}

                <FormField
                  control={form.control}
                  name='pay_creem_enabled'
                  render={({ field }) => (
                    <FormItem className={sideDrawerSwitchItemClassName()}>
                      <FormLabel className='!mt-0'>
                        {t('Enable Creem payment')}
                      </FormLabel>
                      <FormControl>
                        <Switch
                          checked={field.value}
                          onCheckedChange={field.onChange}
                        />
                      </FormControl>
                    </FormItem>
                  )}
                />
                {payCreem && (
                  <FormField
                    control={form.control}
                    name='creem_product_id'
                    render={({ field }) => (
                      <FormItem>
                        <FormLabel>Creem Product ID</FormLabel>
                        <FormControl>
                          <Input {...field} placeholder='prod_...' />
                        </FormControl>
                        <FormMessage />
                      </FormItem>
                    )}
                  />
                )}

                <FormField
                  control={form.control}
                  name='pay_waffo_enabled'
                  render={({ field }) => (
                    <FormItem className={sideDrawerSwitchItemClassName()}>
                      <FormLabel className='!mt-0'>
                        {t('Enable Waffo Pancake payment')}
                      </FormLabel>
                      <FormControl>
                        <Switch
                          checked={field.value}
                          onCheckedChange={field.onChange}
                        />
                      </FormControl>
                    </FormItem>
                  )}
                />
                {payWaffo && (
                  <FormField
                    control={form.control}
                    name='waffo_pancake_product_id'
                    render={({ field }) => {
                      // Raw-ID fallback for IDs not yet in the catalog.
                      const items = pancakeProducts.map((p) => ({
                        value: p.id,
                        label: `${p.name} (${p.id})`,
                      }))
                      if (
                        field.value &&
                        !pancakeProducts.some((p) => p.id === field.value)
                      ) {
                        items.push({ value: field.value, label: field.value })
                      }
                      return (
                        <FormItem>
                          <FormLabel>Waffo Pancake Product ID</FormLabel>
                          <div className='flex gap-2'>
                            <Select
                              items={items}
                              value={field.value || ''}
                              onValueChange={(v) => field.onChange(v)}
                              disabled={items.length === 0}
                            >
                              <SelectTrigger className='w-full flex-1'>
                                <SelectValue
                                  placeholder={t('Select a product')}
                                />
                              </SelectTrigger>
                              <SelectContent>
                                {items.map((item) => (
                                  <SelectItem
                                    key={item.value}
                                    value={item.value}
                                  >
                                    {item.label}
                                  </SelectItem>
                                ))}
                              </SelectContent>
                            </Select>
                            <Button
                              type='button'
                              variant='outline'
                              onClick={handleCreatePancakeProduct}
                              disabled={
                                creatingPancakeProduct || !pancakeCreateReady
                              }
                              className='shrink-0'
                            >
                              {creatingPancakeProduct
                                ? t('Creating...')
                                : `+ ${t('Create')}`}
                            </Button>
                          </div>
                          <FormDescription>
                            {t(
                              'Creates a Pancake product in the saved store using this plan’s title and price. Requires Waffo Pancake to be fully configured in Payment settings first.'
                            )}
                          </FormDescription>
                          <FormMessage />
                        </FormItem>
                      )
                    }}
                  />
                )}

                <FormDescription>
                  {t(
                    'Epay uses the global payment settings and needs no per-plan configuration.'
                  )}
                </FormDescription>
              </div>
            </SideDrawerSection>
          </form>
        </Form>
        <SheetFooter className={sideDrawerFooterClassName()}>
          <SheetClose render={<Button variant='outline' />}>
            {t('Close')}
          </SheetClose>
          <Button
            form='subscription-form'
            type='submit'
            disabled={isSubmitting}
          >
            {isSubmitting ? t('Saving...') : t('Save changes')}
          </Button>
        </SheetFooter>
      </SheetContent>
    </Sheet>
  )
}
