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
import type { TFunction } from 'i18next'
import { z } from 'zod'

import { parseQuotaFromDollars, quotaUnitsToDollars } from '@/lib/format'

import type { SubscriptionPlan, PlanPayload } from '../types'

export function getPlanFormSchema(t: TFunction) {
  return z
    .object({
      title: z.string().min(1, t('Please enter plan title')),
      subtitle: z.string().optional(),
      price_amount: z.coerce.number().min(0, t('Please enter amount')),
      duration_unit: z.enum(['year', 'month', 'day', 'hour', 'custom']),
      duration_value: z.coerce.number().min(1),
      custom_seconds: z.coerce.number().min(0).optional(),
      quota_reset_period: z.enum([
        'never',
        'daily',
        'weekly',
        'monthly',
        'custom',
      ]),
      quota_reset_custom_seconds: z.coerce.number().min(0).optional(),
      enabled: z.boolean(),
      sort_order: z.coerce.number(),
      allow_balance_pay: z.boolean(),
      allow_wallet_overflow: z.boolean(),
      max_purchase_per_user: z.coerce.number().min(0),
      total_amount: z.coerce.number().min(0),
      upgrade_group: z.string().optional(),
      downgrade_group: z.string().optional(),
      stripe_price_id: z.string().optional(),
      creem_product_id: z.string().optional(),
      waffo_pancake_product_id: z.string().optional(),
      // Agent plan extensions
      tag: z.string().optional(),
      plan_level: z.string().optional(),
      five_hour_limit: z.coerce.number().min(0).optional(),
      weekly_limit: z.coerce.number().min(0).optional(),
      monthly_limit: z.coerce.number().min(0).optional(),
      allowed_models: z.array(z.string()).optional(),
      allowed_model_ratios: z.record(z.string(), z.coerce.number()).optional(),
      pay_stripe_enabled: z.boolean().optional(),
      pay_creem_enabled: z.boolean().optional(),
      pay_waffo_enabled: z.boolean().optional(),
    })
    .superRefine((values, ctx) => {
      const requireWhenEnabled = (
        enabled: boolean | undefined,
        id: string | undefined,
        path: 'stripe_price_id' | 'creem_product_id' | 'waffo_pancake_product_id',
        message: string
      ) => {
        if (enabled && !id?.trim()) {
          ctx.addIssue({ code: 'custom', path: [path], message })
        }
      }
      requireWhenEnabled(
        values.pay_stripe_enabled,
        values.stripe_price_id,
        'stripe_price_id',
        t('Please enter the Stripe Price ID')
      )
      requireWhenEnabled(
        values.pay_creem_enabled,
        values.creem_product_id,
        'creem_product_id',
        t('Please enter the Creem Product ID')
      )
      requireWhenEnabled(
        values.pay_waffo_enabled,
        values.waffo_pancake_product_id,
        'waffo_pancake_product_id',
        t('Please enter the Waffo Pancake Product ID')
      )
    })
}

export type PlanFormValues = z.infer<ReturnType<typeof getPlanFormSchema>>

export const PLAN_FORM_DEFAULTS: PlanFormValues = {
  title: '',
  subtitle: '',
  price_amount: 0,
  duration_unit: 'month',
  duration_value: 1,
  custom_seconds: 0,
  quota_reset_period: 'never',
  quota_reset_custom_seconds: 0,
  enabled: true,
  sort_order: 0,
  allow_balance_pay: true,
  allow_wallet_overflow: true,
  max_purchase_per_user: 1,
  total_amount: 0,
  upgrade_group: '',
  downgrade_group: 'default',
  stripe_price_id: '',
  creem_product_id: '',
  waffo_pancake_product_id: '',
  tag: 'agent',
  plan_level: 'plus',
  five_hour_limit: 0,
  weekly_limit: 0,
  monthly_limit: 0,
  allowed_models: [],
  allowed_model_ratios: {},
  pay_stripe_enabled: false,
  pay_creem_enabled: false,
  pay_waffo_enabled: false,
}

export function planToFormValues(plan: SubscriptionPlan): PlanFormValues {
  const selection = allowedModelsJsonToSelection(plan.allowed_models || '')
  return {
    title: plan.title || '',
    subtitle: plan.subtitle || '',
    price_amount: Number(plan.price_amount || 0),
    duration_unit: plan.duration_unit || 'month',
    duration_value: Number(plan.duration_value || 1),
    custom_seconds: Number(plan.custom_seconds || 0),
    quota_reset_period: plan.quota_reset_period || 'never',
    quota_reset_custom_seconds: Number(plan.quota_reset_custom_seconds || 0),
    enabled: plan.enabled !== false,
    sort_order: Number(plan.sort_order || 0),
    allow_balance_pay: plan.allow_balance_pay !== false,
    allow_wallet_overflow: plan.allow_wallet_overflow !== false,
    max_purchase_per_user: Number(plan.max_purchase_per_user || 0),
    total_amount: quotaUnitsToDollars(Number(plan.total_amount || 0)),
    upgrade_group: plan.upgrade_group || '',
    downgrade_group: plan.downgrade_group || 'default',
    stripe_price_id: plan.stripe_price_id || '',
    creem_product_id: plan.creem_product_id || '',
    waffo_pancake_product_id: plan.waffo_pancake_product_id || '',
    tag: plan.tag || 'agent',
    plan_level: plan.plan_level || 'plus',
    five_hour_limit: Number(plan.five_hour_limit || 0),
    weekly_limit: Number(plan.weekly_limit || 0),
    monthly_limit: Number(plan.monthly_limit || 0),
    allowed_models: selection.models,
    allowed_model_ratios: selection.ratios,
    pay_stripe_enabled: !!plan.stripe_price_id,
    pay_creem_enabled: !!plan.creem_product_id,
    pay_waffo_enabled: !!plan.waffo_pancake_product_id,
  }
}

export function formValuesToPlanPayload(values: PlanFormValues): PlanPayload {
  return {
    plan: {
      ...values,
      price_amount: Number(values.price_amount || 0),
      currency: 'USD',
      duration_value: Number(values.duration_value || 0),
      custom_seconds: Number(values.custom_seconds || 0),
      quota_reset_period: values.quota_reset_period || 'never',
      quota_reset_custom_seconds:
        values.quota_reset_period === 'custom'
          ? Number(values.quota_reset_custom_seconds || 0)
          : 0,
      sort_order: Number(values.sort_order || 0),
      max_purchase_per_user: Number(values.max_purchase_per_user || 0),
      total_amount: parseQuotaFromDollars(Number(values.total_amount || 0)),
      upgrade_group: values.upgrade_group || '',
      downgrade_group: values.downgrade_group || 'default',
      tag: values.tag || 'agent',
      plan_level: values.plan_level || 'plus',
      five_hour_limit: Number(values.five_hour_limit || 0),
      weekly_limit: Number(values.weekly_limit || 0),
      monthly_limit: Number(values.monthly_limit || 0),
      stripe_price_id: values.pay_stripe_enabled
        ? (values.stripe_price_id || '').trim()
        : '',
      creem_product_id: values.pay_creem_enabled
        ? (values.creem_product_id || '').trim()
        : '',
      waffo_pancake_product_id: values.pay_waffo_enabled
        ? (values.waffo_pancake_product_id || '').trim()
        : '',
      allowed_models: selectionToAllowedModelsJson(
        values.allowed_models,
        values.allowed_model_ratios
      ),
    },
  }
}

// ----------------------------------------------------------------------------
// Allowed models helpers
// ----------------------------------------------------------------------------
// The backend stores AllowedModels as a JSON-encoded array of {model, ratio}.
// The simplified drawer edits it as a MultiSelect of model names sourced from
// the enabled channels; per-model ratios are preserved from the existing JSON
// (new models default to 1) so editing a plan never silently rewrites ratios.

export interface AllowedModelRow {
  model: string
  ratio: number
}

export interface AllowedModelSelection {
  models: string[]
  ratios: Record<string, number>
}

export function allowedModelsJsonToSelection(
  json: string
): AllowedModelSelection {
  const models: string[] = []
  const ratios: Record<string, number> = {}
  if (!json) return { models, ratios }
  let parsed: unknown
  try {
    parsed = JSON.parse(json)
  } catch {
    return { models, ratios }
  }
  if (!Array.isArray(parsed)) return { models, ratios }
  for (const row of parsed as AllowedModelRow[]) {
    const model = typeof row?.model === 'string' ? row.model.trim() : ''
    if (!model) continue
    const ratio = Number(row?.ratio ?? 1)
    models.push(model)
    ratios[model] = Number.isFinite(ratio) && ratio > 0 ? ratio : 1
  }
  return { models, ratios }
}

export function selectionToAllowedModelsJson(
  models: string[] | undefined,
  ratios: Record<string, number> | undefined
): string {
  const rows = (models ?? [])
    .map((m) => m.trim())
    .filter(Boolean)
    .map((model) => ({ model, ratio: ratios?.[model] ?? 1 }))
  if (rows.length === 0) return ''
  return JSON.stringify(rows)
}
