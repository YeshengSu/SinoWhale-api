package common

import (
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 阿里云「数字验证码」类型模板要求模板变量为纯数字，含字母会被拒绝。
var numericCodePattern = regexp.MustCompile(`^[0-9]+$`)

func TestGenerateNumericCode(t *testing.T) {
	tests := []struct {
		name   string
		length int
	}{
		{name: "sms 6-digit code", length: 6},
		{name: "length 1", length: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code := GenerateNumericCode(tt.length)
			require.Len(t, code, tt.length)
			assert.Regexp(t, numericCodePattern, code)
		})
	}
	// 多次采样，确保不会偶发产生含字母的验证码
	for i := 0; i < 100; i++ {
		assert.Regexp(t, numericCodePattern, GenerateNumericCode(6))
	}
}
