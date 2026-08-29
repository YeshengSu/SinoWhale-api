package service

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// 订阅计费统一化（PRD: docs/plans/2026-08-29-agent-mode-to-universal.md Task 1）
//
// 核心断言：订阅套餐 allowed_models 白名单的强制执行与 AGENT_MODE 实例开关
// 无关 —— 只要活跃订阅配置了白名单快照，白名单外模型必须 403 + 回滚预扣。
// ---------------------------------------------------------------------------

func setupSubscriptionBilling(t *testing.T) {
	t.Helper()
	// glebarez/sqlite 对同一批表二次 AutoMigrate 会重解析 DDL 并报
	// "invalid DDL, unbalanced brackets"，故仅在缺表时迁移。
	migrator := model.DB.Migrator()
	for _, modelPtr := range []interface{}{
		&model.SubscriptionPlan{},
		&model.UserSubscription{},
		&model.SubscriptionPreConsumeRecord{},
		&model.UserPlanUsage{},
	} {
		if !migrator.HasTable(modelPtr) {
			require.NoError(t, model.DB.AutoMigrate(modelPtr))
		}
	}
	t.Cleanup(func() {
		model.DB.Exec("DELETE FROM subscription_pre_consume_records")
		model.DB.Exec("DELETE FROM user_subscriptions")
		model.DB.Exec("DELETE FROM subscription_plans")
		model.DB.Exec("DELETE FROM user_plan_usages")
		model.DB.Exec("DELETE FROM tokens")
		model.DB.Exec("DELETE FROM users")
	})
}

func newBillingTestContext(t *testing.T) *gin.Context {
	t.Helper()
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
	return c
}

func seedPlanAndSub(t *testing.T, planId, subId, userId int, tag, allowedModelsSnap string) {
	t.Helper()
	plan := &model.SubscriptionPlan{
		Id:               planId,
		Title:            "Test Plan",
		QuotaResetPeriod: "never",
	}
	require.NoError(t, model.DB.Create(plan).Error)
	sub := &model.UserSubscription{
		Id:                subId,
		UserId:            userId,
		PlanId:            planId,
		AmountTotal:       1000,
		AmountUsed:        0,
		Status:            "active",
		StartTime:         time.Now().Unix(),
		EndTime:           time.Now().Add(30 * 24 * time.Hour).Unix(),
		Tag:               tag,
		AllowedModelsSnap: allowedModelsSnap,
	}
	require.NoError(t, model.DB.Create(sub).Error)
}

func withAgentMode(t *testing.T, enabled bool) {
	t.Helper()
	prev := common.AgentModeEnabled
	common.AgentModeEnabled = enabled
	t.Cleanup(func() { common.AgentModeEnabled = prev })
}

func subscriptionAmountUsed(t *testing.T, subId int) int64 {
	t.Helper()
	var used int64
	require.NoError(t, model.DB.Model(&model.UserSubscription{}).
		Where("id = ?", subId).Pluck("amount_used", &used).Error)
	return used
}

func tokenRemainQuota(t *testing.T, tokenId int) int {
	t.Helper()
	var remain int
	require.NoError(t, model.DB.Model(&model.Token{}).
		Where("id = ?", tokenId).Pluck("remain_quota", &remain).Error)
	return remain
}

// TestNewBillingSessionEnforcesAllowedModelsRegardlessOfAgentMode 修复白名单
// 空洞：mode=false（通用实例）下，配置了白名单的订阅同样必须拒绝白名单外模型，
// 且预扣（订阅 + 令牌）必须完整回滚。
func TestNewBillingSessionEnforcesAllowedModelsRegardlessOfAgentMode(t *testing.T) {
	setupSubscriptionBilling(t)
	withAgentMode(t, false) // 关键：通用模式（改造前此分支被 AGENT_MODE 门控跳过）

	seedUser(t, 1, 100000)
	seedToken(t, 1, 1, "sk-test-billing", 100000)
	seedPlanAndSub(t, 1, 1, 1, model.PlanTagAgent, `[{"model":"gpt-4o"}]`)

	c := newBillingTestContext(t)
	relayInfo := &relaycommon.RelayInfo{
		RequestId:       "req-test-whitelist-block",
		UserId:          1,
		TokenId:         1,
		TokenKey:        "sk-test-billing",
		OriginModelName: "claude-3-opus", // 不在白名单内
	}

	session, apiErr := NewBillingSession(c, relayInfo, 100)

	require.Error(t, apiErr, "白名单外模型必须被拒绝（与 AGENT_MODE 无关）")
	assert.Nil(t, session)
	assert.Equal(t, types.ErrorCodeInsufficientUserQuota, apiErr.GetErrorCode())
	assert.Contains(t, apiErr.Error(), "当前套餐不支持模型")

	// Refund 异步执行：轮询等待订阅与令牌预扣回滚到原值。
	require.Eventually(t, func() bool {
		return subscriptionAmountUsed(t, 1) == 0
	}, 3*time.Second, 50*time.Millisecond, "订阅预扣应已回滚")

	require.Eventually(t, func() bool {
		return tokenRemainQuota(t, 1) == 100000
	}, 3*time.Second, 50*time.Millisecond, "令牌预扣应已回滚")
}

// TestNewBillingSessionAllowsModelInWhitelist 白名单内模型正常走订阅扣费。
func TestNewBillingSessionAllowsModelInWhitelist(t *testing.T) {
	setupSubscriptionBilling(t)
	withAgentMode(t, false)

	seedUser(t, 1, 100000)
	seedToken(t, 1, 1, "sk-test-billing", 100000)
	seedPlanAndSub(t, 1, 1, 1, model.PlanTagAgent, `[{"model":"gpt-4o"}]`)

	c := newBillingTestContext(t)
	relayInfo := &relaycommon.RelayInfo{
		RequestId:       "req-test-whitelist-allow",
		UserId:          1,
		TokenId:         1,
		TokenKey:        "sk-test-billing",
		OriginModelName: "gpt-4o",
	}

	session, apiErr := NewBillingSession(c, relayInfo, 100)

	require.Nil(t, apiErr)
	require.NotNil(t, session)
	assert.Equal(t, BillingSourceSubscription, session.funding.Source())
	assert.Equal(t, int64(100), subscriptionAmountUsed(t, 1), "订阅应预扣 100")
}

// TestNewBillingSessionEmptyWhitelistUnrestricted 空白名单快照 = 不限制（存量行为保护）。
func TestNewBillingSessionEmptyWhitelistUnrestricted(t *testing.T) {
	setupSubscriptionBilling(t)
	withAgentMode(t, false)

	seedUser(t, 1, 100000)
	seedToken(t, 1, 1, "sk-test-billing", 100000)
	seedPlanAndSub(t, 1, 1, 1, "", "")

	c := newBillingTestContext(t)
	relayInfo := &relaycommon.RelayInfo{
		RequestId:       "req-test-empty-snap",
		UserId:          1,
		TokenId:         1,
		TokenKey:        "sk-test-billing",
		OriginModelName: "any-model",
	}

	session, apiErr := NewBillingSession(c, relayInfo, 100)

	require.Nil(t, apiErr)
	require.NotNil(t, session)
}

// TestNewBillingSessionAgentTagSubscriptionParticipates 统一订阅匹配：
// agent tag 订阅在通用模式下同样参与 subscription_first 扣费。
func TestNewBillingSessionAgentTagSubscriptionParticipates(t *testing.T) {
	setupSubscriptionBilling(t)
	withAgentMode(t, false)

	seedUser(t, 1, 100000)
	seedToken(t, 1, 1, "sk-test-billing", 100000)
	seedPlanAndSub(t, 1, 1, 1, model.PlanTagAgent, "")

	c := newBillingTestContext(t)
	relayInfo := &relaycommon.RelayInfo{
		RequestId:       "req-test-agent-tag",
		UserId:          1,
		TokenId:         1,
		TokenKey:        "sk-test-billing",
		OriginModelName: "gpt-4o",
	}

	session, apiErr := NewBillingSession(c, relayInfo, 100)

	require.Nil(t, apiErr)
	require.NotNil(t, session)
	assert.Equal(t, BillingSourceSubscription, session.funding.Source())
	assert.Equal(t, int64(100), subscriptionAmountUsed(t, 1))
}
