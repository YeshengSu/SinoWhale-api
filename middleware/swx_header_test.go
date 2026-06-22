package middleware

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestIsSafeSWXHeader(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  bool
	}{
		// 合法值
		{"valid snake_case", "user_64f0a1b2c3d4e5", true},
		{"valid kebab-case", "swx-2026-06-18-abc123", true},
		{"valid mixed", "user_64f0a1b2-c3d4e5", true},
		{"valid alphanumeric", "user001", true},
		// 非法值：特殊字符
		{"invalid angle bracket", "<script>alert(1)</script>", false},
		{"invalid space", "user 001", false},
		{"invalid slash", "user/001", false},
		{"invalid dot", "user.001", false},
		{"invalid at", "user@domain", false},
		// 长度边界
		{"empty string", "", false},
		{"128 chars valid", string(make([]byte, 128)), false}, // all zeros = \x00, invalid
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isSafeSWXHeader(tt.value)
			assert.Equal(t, tt.want, got, "isSafeSWXHeader(%q) = %v, want %v", tt.value, got, tt.want)
		})
	}
}

func TestIsSafeSWXHeader_LengthBoundary(t *testing.T) {
	// 128 个合法字符：通过
	valid128 := ""
	for i := 0; i < 128; i++ {
		valid128 += "a"
	}
	assert.True(t, isSafeSWXHeader(valid128), "128 valid chars should pass")

	// 129 个合法字符：拒绝
	valid129 := valid128 + "a"
	assert.False(t, isSafeSWXHeader(valid129), "129 valid chars should be rejected")
}

func TestExtractSWXContext_Disabled(t *testing.T) {
	t.Setenv("SWX_HEADER_ENABLED", "false")
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/", nil)
	c.Request.Header.Set("X-SWX-User-Id", "user_001")
	c.Request.Header.Set("X-SWX-Trace-Id", "trace-abc")

	extractSWXContext(c)

	// 未写入任何 key
	assert.Empty(t, common.GetContextKeyString(c, constant.ContextKeySWXUserId))
	assert.Empty(t, common.GetContextKeyString(c, constant.ContextKeySWXTraceId))
}

func TestExtractSWXContext_AllHeaders(t *testing.T) {
	t.Setenv("SWX_HEADER_ENABLED", "true")
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/", nil)
	c.Request.Header.Set("X-SWX-User-Id", "user_001")
	c.Request.Header.Set("X-SWX-Trace-Id", "trace-abc")
	c.Request.Header.Set("X-SWX-Biz-Type", "image")
	c.Request.Header.Set("X-SWX-Request-Id", "req-1")

	extractSWXContext(c)

	assert.Equal(t, "user_001", common.GetContextKeyString(c, constant.ContextKeySWXUserId))
	assert.Equal(t, "trace-abc", common.GetContextKeyString(c, constant.ContextKeySWXTraceId))
	assert.Equal(t, "image", common.GetContextKeyString(c, constant.ContextKeySWXBizType))
	assert.Equal(t, "req-1", common.GetContextKeyString(c, constant.ContextKeySWXRequestId))
}

func TestExtractSWXContext_PartialHeaders(t *testing.T) {
	t.Setenv("SWX_HEADER_ENABLED", "true")
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/", nil)
	// 仅携带 User-Id 和 Biz-Type
	c.Request.Header.Set("X-SWX-User-Id", "user_002")
	c.Request.Header.Set("X-SWX-Biz-Type", "video")

	extractSWXContext(c)

	assert.Equal(t, "user_002", common.GetContextKeyString(c, constant.ContextKeySWXUserId))
	assert.Equal(t, "video", common.GetContextKeyString(c, constant.ContextKeySWXBizType))
	// 未携带的 key 保持空
	assert.Empty(t, common.GetContextKeyString(c, constant.ContextKeySWXTraceId))
	assert.Empty(t, common.GetContextKeyString(c, constant.ContextKeySWXRequestId))
}

func TestExtractSWXContext_InvalidHeaderSilent(t *testing.T) {
	t.Setenv("SWX_HEADER_ENABLED", "true")
	t.Setenv("SWX_HEADER_STRICT", "false")
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/", nil)
	c.Request.Header.Set("X-SWX-User-Id", "<script>alert(1)</script>")

	extractSWXContext(c)

	// 非法 Header 静默忽略，不写入
	assert.Empty(t, common.GetContextKeyString(c, constant.ContextKeySWXUserId))
	assert.False(t, c.IsAborted())
}

func TestExtractSWXContext_InvalidHeaderStrict(t *testing.T) {
	t.Setenv("SWX_HEADER_ENABLED", "true")
	t.Setenv("SWX_HEADER_STRICT", "true")
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/", nil)
	c.Request.Header.Set("X-SWX-User-Id", "<script>")

	extractSWXContext(c)

	assert.True(t, c.IsAborted())
	assert.Equal(t, 400, w.Code)
}

func TestExtractSWXContext_NoHeaders(t *testing.T) {
	t.Setenv("SWX_HEADER_ENABLED", "true")
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/", nil)

	extractSWXContext(c)

	// 全部为空，无 Abort
	assert.Empty(t, common.GetContextKeyString(c, constant.ContextKeySWXUserId))
	assert.Empty(t, common.GetContextKeyString(c, constant.ContextKeySWXTraceId))
	assert.Empty(t, common.GetContextKeyString(c, constant.ContextKeySWXBizType))
	assert.Empty(t, common.GetContextKeyString(c, constant.ContextKeySWXRequestId))
	assert.False(t, c.IsAborted())
}