package model

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func migrateUserPlanUsageForTest(t *testing.T) {
	t.Helper()
	require.NoError(t, DB.AutoMigrate(&UserPlanUsage{}))
	t.Cleanup(func() {
		DB.Exec("DELETE FROM user_plan_usages")
	})
}

func TestCreateUserSubscriptionFromPlanTxSnapshotsAgentFields(t *testing.T) {
	truncateTables(t)

	plan := &SubscriptionPlan{
		Id:            9301,
		Title:         "Plus",
		PriceAmount:   10,
		DurationUnit:  SubscriptionDurationMonth,
		DurationValue: 1,
		TotalAmount:   0,
		Tag:           PlanTagAgent,
		PlanLevel:     "plus",
		FiveHourLimit: 100,
		WeeklyLimit:   200,
		MonthlyLimit:  300,
		DowngradeGroup: "default",
		AllowedModels: `[{"model":"gpt-4.1","ratio":1}]`,
	}
	require.NoError(t, DB.Create(plan).Error)

	sub, err := CreateUserSubscriptionFromPlanTx(DB, 4101, plan, "test")
	require.NoError(t, err)
	require.NotNil(t, sub)

	assert.Equal(t, PlanTagAgent, sub.Tag)
	assert.Equal(t, "plus", sub.PlanLevel)
	assert.Equal(t, int64(100), sub.FiveHourLimitSnap)
	assert.Equal(t, int64(200), sub.WeeklyLimitSnap)
	assert.Equal(t, int64(300), sub.MonthlyLimitSnap)
	assert.Equal(t, plan.AllowedModels, sub.AllowedModelsSnap)
	assert.Equal(t, "default", sub.DowngradeGroup)
}

func TestCheckUserPlanWindowLimits(t *testing.T) {
	truncateTables(t)
	migrateUserPlanUsageForTest(t)

	now := GetDBTimestamp()
	startTime := now - 3600
	newSub := func(id int, tag string, five, weekly, monthly int64) *UserSubscription {
		sub := &UserSubscription{
			Id:                id,
			UserId:            4201,
			PlanId:            9401,
			AmountTotal:       0,
			StartTime:         startTime,
			EndTime:           now + 30*24*3600,
			Status:            "active",
			Source:            "test",
			Tag:               tag,
			FiveHourLimitSnap: five,
			WeeklyLimitSnap:   weekly,
			MonthlyLimitSnap:  monthly,
		}
		require.NoError(t, DB.Create(sub).Error)
		return sub
	}
	seedUsage := func(subId int, periodType string, used int64) {
		start, end, err := ComputeCurrentPeriod(periodType, startTime, now, time.Local)
		require.NoError(t, err)
		require.NoError(t, DB.Create(&UserPlanUsage{
			UserId:             4201,
			UserSubscriptionId: subId,
			PeriodType:         periodType,
			PeriodStart:        start,
			PeriodEnd:          end,
			QuotaUsed:          used,
		}).Error)
	}

	t.Run("no usage rows means within quota", func(t *testing.T) {
		sub := newSub(9501, PlanTagAgent, 100, 200, 300)
		assert.Equal(t, "", CheckUserPlanWindowLimits(sub))
	})

	t.Run("five hour window reached returns five hour label", func(t *testing.T) {
		sub := newSub(9502, PlanTagAgent, 100, 200, 300)
		seedUsage(sub.Id, UserPlanUsagePeriodFiveHour, 100)
		assert.Equal(t, "5 小时", CheckUserPlanWindowLimits(sub))
	})

	t.Run("under every limit passes", func(t *testing.T) {
		sub := newSub(9503, PlanTagAgent, 100, 200, 300)
		seedUsage(sub.Id, UserPlanUsagePeriodFiveHour, 99)
		seedUsage(sub.Id, UserPlanUsagePeriodWeekly, 199)
		seedUsage(sub.Id, UserPlanUsagePeriodMonthly, 299)
		assert.Equal(t, "", CheckUserPlanWindowLimits(sub))
	})

	t.Run("monthly window reached returns monthly label", func(t *testing.T) {
		sub := newSub(9504, PlanTagAgent, 100, 200, 300)
		seedUsage(sub.Id, UserPlanUsagePeriodMonthly, 300)
		assert.Equal(t, "月", CheckUserPlanWindowLimits(sub))
	})

	t.Run("zero snapshot means unlimited window", func(t *testing.T) {
		sub := newSub(9505, PlanTagAgent, 0, 0, 300)
		seedUsage(sub.Id, UserPlanUsagePeriodFiveHour, 999999)
		seedUsage(sub.Id, UserPlanUsagePeriodWeekly, 999999)
		assert.Equal(t, "", CheckUserPlanWindowLimits(sub))
	})

	t.Run("non agent subscriptions are never limited", func(t *testing.T) {
		sub := newSub(9506, "", 100, 100, 100)
		seedUsage(sub.Id, UserPlanUsagePeriodFiveHour, 999)
		seedUsage(sub.Id, UserPlanUsagePeriodWeekly, 999)
		seedUsage(sub.Id, UserPlanUsagePeriodMonthly, 999)
		assert.Equal(t, "", CheckUserPlanWindowLimits(sub))
	})
}
