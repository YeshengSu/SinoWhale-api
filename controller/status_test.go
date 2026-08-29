package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// TestStatusAgentModeFixedFalse 校验 /api/status 中 agent_mode 与 phone_verification
// 固定返回 false（PRD 7.2）：即使旧 AGENT_MODE 开关曾启用，接口也不再驱动前端
// 隐藏登录/注册页。字段保留兼容已部署前端，但值恒为 false。
func TestStatusAgentModeFixedFalse(t *testing.T) {
	gin.SetMode(gin.TestMode)

	prev := common.AgentModeEnabled
	common.AgentModeEnabled = true // 模拟旧的 agent 实例开关；改造后字段必须恒为 false
	t.Cleanup(func() { common.AgentModeEnabled = prev })

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/status", nil)
	GetStatus(ctx)

	require.Equal(t, http.StatusOK, recorder.Code, "status response: %s", recorder.Body.String())
	body := recorder.Body.String()
	require.Contains(t, body, `"agent_mode":false`, "agent_mode must be fixed false: %s", body)
	require.Contains(t, body, `"phone_verification":false`, "phone_verification must be fixed false: %s", body)
	require.NotContains(t, body, `"agent_mode":true`, "agent_mode must never be true: %s", body)
	require.NotContains(t, body, `"phone_verification":true`, "phone_verification must never be true: %s", body)
	// 其余状态字段不受影响（抽查关键开关仍存在）
	require.Contains(t, body, `"version"`, "version field must remain: %s", body)
	require.Contains(t, body, `"email_verification"`, "email_verification field must remain: %s", body)
}
