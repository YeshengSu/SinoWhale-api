package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// TestStatusAgentModeFixedFalse 校验 /api/status 中 agent_mode 恒为 false
// （PRD 7.2：AGENT_MODE 已退役，旧开关不再驱动前端）且 phone_verification
// 恒为 true —— 注册页强制手机号 + 短信验证码。字段保留兼容已部署前端。
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
	require.Contains(t, body, `"phone_verification":true`, "register forces phone verification: %s", body)
	require.NotContains(t, body, `"agent_mode":true`, "agent_mode must never be true: %s", body)

	// 其余状态字段不受影响（抽查关键开关仍存在）
	require.Contains(t, body, `"version"`, "version field must remain: %s", body)
	require.Contains(t, body, `"email_verification"`, "email_verification field must remain: %s", body)
}
