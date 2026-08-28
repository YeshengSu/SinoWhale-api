package common

import (
	"math"
	"testing"
)

func TestDollarsToCredits(t *testing.T) {
	tests := []struct {
		name string
		usd  float64
		want float64
	}{
		{"zero", 0, 0},
		{"negative", -1.5, 0},
		{"positive", 9.99, 9.99},
		{"fractional", 0.5, 0.5},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := DollarsToCredits(tc.usd)
			if got != tc.want {
				t.Errorf("DollarsToCredits(%v) = %v, want %v", tc.usd, got, tc.want)
			}
		})
	}
}

func TestDollarsToCredits_NaN(t *testing.T) {
	// NaN：usd != usd 为 true → 短路返回 0。
	if got := DollarsToCredits(math.NaN()); got != 0 {
		t.Errorf("DollarsToCredits(NaN) = %v, want 0", got)
	}
}

func TestQuotaToCredits(t *testing.T) {
	// CreditsPerUSD=1.0 且 QuotaPerUnit=500000：
	// 1 美元 = 500000 quota = 1 积分，所以 quota 直接等于 credits。
	tests := []struct {
		name  string
		quota int
		want  float64
	}{
		{"zero", 0, 0},
		{"negative", -10, 0},
		{"one dollar", int(QuotaPerUnit), 1.0},
		{"ten dollars", int(QuotaPerUnit) * 10, 10.0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := QuotaToCredits(tc.quota)
			if got != tc.want {
				t.Errorf("QuotaToCredits(%v) = %v, want %v", tc.quota, got, tc.want)
			}
		})
	}
}
