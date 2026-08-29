package controller

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupSubscriptionPlanTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	gin.SetMode(gin.TestMode)
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	common.RedisEnabled = false

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err, "failed to open sqlite db")
	model.DB = db
	model.LOG_DB = db
	require.NoError(t, db.AutoMigrate(&model.SubscriptionPlan{}), "failed to migrate subscription_plans")
	t.Cleanup(func() {
		sqlDB, err := db.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
	})
	return db
}

// TestAdminListSubscriptionPlansIncludesAllTags 校验管理员套餐列表不过滤 tag（PRD 7.1）：
// 即使旧 AGENT_MODE 开关开启，agent 与非 agent 套餐都必须全部返回。
func TestAdminListSubscriptionPlansIncludesAllTags(t *testing.T) {
	db := setupSubscriptionPlanTestDB(t)

	seeds := []model.SubscriptionPlan{
		{Title: "agent lite", Tag: "agent", PlanLevel: "lite", Enabled: true, SortOrder: 1},
		{Title: "general basic", Tag: "", PlanLevel: "", Enabled: true, SortOrder: 2},
	}
	for i := range seeds {
		seeds[i].NormalizeDefaults()
		require.NoError(t, db.Create(&seeds[i]).Error, "failed to seed plan")
	}

	prev := common.AgentModeEnabled
	common.AgentModeEnabled = true // 模拟旧的 agent 实例开关；改造后列表必须不过滤
	t.Cleanup(func() { common.AgentModeEnabled = prev })

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/subscription/admin/plans", nil)
	AdminListSubscriptionPlans(ctx)

	require.Equal(t, http.StatusOK, recorder.Code, "plan list response: %s", recorder.Body.String())

	var resp struct {
		Success bool                  `json:"success"`
		Data    []SubscriptionPlanDTO `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &resp), "failed to decode plan list: %s", recorder.Body.String())
	require.True(t, resp.Success, "plan list should succeed: %s", recorder.Body.String())
	require.Len(t, resp.Data, 2, "admin plan list must include both agent and non-agent plans, got %d", len(resp.Data))

	var titles []string
	for _, dto := range resp.Data {
		titles = append(titles, dto.Plan.Title)
	}
	require.ElementsMatch(t, []string{"agent lite", "general basic"}, titles,
		"plan list must not filter by tag")
}
