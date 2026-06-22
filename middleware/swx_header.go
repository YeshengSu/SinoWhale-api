package middleware

import (
	"net/http"
	"os"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"

	"github.com/gin-gonic/gin"
)

// SinoWhaleX 自定义 Header 名称（方案 C）。
const (
	HeaderSWXUserId    = "X-SWX-User-Id"
	HeaderSWXTraceId   = "X-SWX-Trace-Id"
	HeaderSWXBizType   = "X-SWX-Biz-Type"
	HeaderSWXRequestId = "X-SWX-Request-Id"

	swxHeaderMaxLen = 128
)

// extractSWXContext 在 TokenAuth 认证通过后调用，从请求 Header 中提取
// 由 SinoWhaleX 后端注入的用户身份信息，写入 Gin Context。
//
// 行为：
//   - 受 SWX_HEADER_ENABLED=true 控制，关闭时直接 noop。
//   - 仅当 Header 合法（长度/字符白名单）时写入 Context。
//   - 非法 Header 默认静默忽略（同时记一条 SysLog warn），
//     若 SWX_HEADER_STRICT=true 则返回 400 终止请求。
//
// 重要：
//   - 该函数不参与任何鉴权/计费/路由决策，仅作为可观测信号。
//   - 上游 LLM 不会收到任何 X-SWX-* Header。
func extractSWXContext(c *gin.Context) {
	if !swxHeaderEnabled() {
		return
	}

	strict := swxHeaderStrict()

	type headerKey struct {
		header string
		ctxKey constant.ContextKey
	}
	mapping := []headerKey{
		{HeaderSWXUserId, constant.ContextKeySWXUserId},
		{HeaderSWXTraceId, constant.ContextKeySWXTraceId},
		{HeaderSWXBizType, constant.ContextKeySWXBizType},
		{HeaderSWXRequestId, constant.ContextKeySWXRequestId},
	}

	for _, m := range mapping {
		raw := strings.TrimSpace(c.GetHeader(m.header))
		if raw == "" {
			continue
		}
		if !isSafeSWXHeader(raw) {
			if strict {
				c.JSON(http.StatusBadRequest, gin.H{
					"success": false,
					"message": "X-SWX-* header 格式不合法",
					"code":    "swx_header_invalid",
				})
				c.Abort()
				return
			}
			common.SysLog("SWX header ignored due to invalid format: " + m.header)
			continue
		}
		common.SetContextKey(c, m.ctxKey, raw)
	}
}

// isSafeSWXHeader 校验 SWX Header 值：长度 1~128，仅允许字母、数字、下划线、连字符。
func isSafeSWXHeader(v string) bool {
	if len(v) == 0 || len(v) > swxHeaderMaxLen {
		return false
	}
	for _, ch := range v {
		switch {
		case ch == '-' || ch == '_':
		case ch >= '0' && ch <= '9':
		case ch >= 'a' && ch <= 'z':
		case ch >= 'A' && ch <= 'Z':
		default:
			return false
		}
	}
	return true
}

// swxHeaderEnabled 读取 SWX_HEADER_ENABLED 总开关。
func swxHeaderEnabled() bool {
	return strings.EqualFold(strings.TrimSpace(os.Getenv("SWX_HEADER_ENABLED")), "true")
}

// swxHeaderStrict 读取 SWX_HEADER_STRICT 严格模式开关。
func swxHeaderStrict() bool {
	return strings.EqualFold(strings.TrimSpace(os.Getenv("SWX_HEADER_STRICT")), "true")
}

// swxLogQueryRole 读取 SWX_HEADER_LOG_QUERY_ROLE，控制谁可按 SWX 维度查询日志。
// 取值：admin（默认）/ root / any。
func swxLogQueryRole() string {
	v := strings.ToLower(strings.TrimSpace(os.Getenv("SWX_HEADER_LOG_QUERY_ROLE")))
	if v == "" {
		return "admin"
	}
	return v
}
