package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSeedAgentPlansInsertsThreeBaseTiers verifies that SeedAgentPlans
// creates the three base token plan tiers (lite / plus / max) with the
// agent tag and only the Title / PlanLevel filled — everything else stays
// at the schema default (zero / empty) so the admin can fill pricing in
// later via the management API.
func TestSeedAgentPlansInsertsThreeBaseTiers(t *testing.T) {
	truncateTables(t)

	require.NoError(t, SeedAgentPlans())

	var plans []SubscriptionPlan
	require.NoError(t, DB.Where("tag = ?", PlanTagAgent).
		Order("sort_order desc").
		Find(&plans).Error)

	require.Len(t, plans, 3, "expected lite, plus, max tiers to be seeded")

	gotLevels := []string{plans[0].PlanLevel, plans[1].PlanLevel, plans[2].PlanLevel}
	assert.Equal(t, []string{"max", "plus", "lite"}, gotLevels,
		"sort_order desc should surface max → plus → lite")

	for _, plan := range plans {
		assert.Equal(t, PlanTagAgent, plan.Tag)
		assert.NotEmpty(t, plan.Title, "Title must be set even with no other fields")
		// Everything else should remain at schema defaults.
		assert.Equal(t, float64(0), plan.PriceAmount, "PriceAmount should stay at 0")
		assert.Empty(t, plan.Subtitle, "Subtitle should stay empty")
		assert.Empty(t, plan.StripePriceId)
		assert.Empty(t, plan.AllowedModels)
		assert.Equal(t, int64(0), plan.TotalAmount)
		assert.Equal(t, int64(0), plan.FiveHourLimit)
		assert.Equal(t, int64(0), plan.WeeklyLimit)
		assert.Equal(t, int64(0), plan.MonthlyLimit)
		assert.True(t, plan.Enabled, "seeded plans should be enabled by default")
		require.NotNil(t, plan.AllowBalancePay, "AllowBalancePay default should be normalized")
		assert.True(t, *plan.AllowBalancePay)
	}

	// Spot-check titles match level names.
	titleByLevel := map[string]string{}
	for _, plan := range plans {
		titleByLevel[plan.PlanLevel] = plan.Title
	}
	assert.Equal(t, "Lite", titleByLevel["lite"])
	assert.Equal(t, "Plus", titleByLevel["plus"])
	assert.Equal(t, "Max", titleByLevel["max"])
}

// TestSeedAgentPlansIsIdempotent confirms running SeedAgentPlans twice does
// not duplicate rows — the second call is a no-op for tiers that already
// exist with the same tag.
func TestSeedAgentPlansIsIdempotent(t *testing.T) {
	truncateTables(t)

	require.NoError(t, SeedAgentPlans())
	require.NoError(t, SeedAgentPlans())

	var count int64
	require.NoError(t, DB.Model(&SubscriptionPlan{}).
		Where("tag = ?", PlanTagAgent).
		Count(&count).Error)
	assert.Equal(t, int64(3), count,
		"running SeedAgentPlans twice should still produce exactly 3 rows")
}

// TestSeedAgentPlansPreservesAdminEdits ensures the admin can edit a seeded
// plan's Title (or any other field) and the next seed run will not clobber
// it.
func TestSeedAgentPlansPreservesAdminEdits(t *testing.T) {
	truncateTables(t)

	require.NoError(t, SeedAgentPlans())

	// Admin renames the "Lite" plan and sets a price.
	require.NoError(t, DB.Model(&SubscriptionPlan{}).
		Where("plan_level = ? AND tag = ?", "lite", PlanTagAgent).
		Updates(map[string]interface{}{
			"title":        "Lite (优惠)",
			"price_amount": 9.99,
		}).Error)

	// Subsequent seed run must not touch existing rows.
	require.NoError(t, SeedAgentPlans())

	var lite SubscriptionPlan
	require.NoError(t, DB.Where("plan_level = ? AND tag = ?", "lite", PlanTagAgent).
		First(&lite).Error)
	assert.Equal(t, "Lite (优惠)", lite.Title, "admin edit must survive re-seed")
	assert.InDelta(t, 9.99, lite.PriceAmount, 1e-9)
}