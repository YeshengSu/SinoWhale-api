package common

// 积分（Credits）展示层助手。所有函数纯展示用，**不**触碰 quota 内部会计。
//
// 设计目标：把"USD 计价"映射为"积分（Credits）"展示给用户，内部仍以
// quota 扣费。汇率由 CreditsPerUSD 常量控制，默认 1.0（1 美元 = 1 积分）；
// 如需换算更人性化（$1=100 积分）只动这个常量即可。

// DollarsToCredits 把美元金额换算为积分数。
//
// 负数与 NaN 安全（NaN 通过 credits=0 短路），输出与 QuotaRound 分离
// —— 本函数只用于展示层四舍五入，quota 扣费仍走 common.QuotaRound。
func DollarsToCredits(usd float64) float64 {
	if usd <= 0 || usd != usd { /* <=0 跳过；usd!=usd 排除 NaN */
		return 0
	}
	return usd * CreditsPerUSD
}

// QuotaToCredits 把原始 quota 整数换算为可展示的积分数。
//
// 与 DollarsToCredits 的关系：quota = USD × QuotaPerUnit → USD = quota/QuotaPerUnit
// → credits = USD × CreditsPerUSD = quota × (CreditsPerUSD / QuotaPerUnit)。
// 当 CreditsPerUSD=1.0 时此函数恒等于原 quota 值（1 积分=1 美元=QuotaPerUnit/QuotaPerUnit 系数）。
func QuotaToCredits(quota int) float64 {
	if quota <= 0 {
		return 0
	}
	return float64(quota) * CreditsPerUSD / QuotaPerUnit
}
