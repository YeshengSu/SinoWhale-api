package constant

// TokenTagWhitelist 定义 token.tag 的合法取值，空串表示未打标签（缺省）。
// 后续新增标签在此扩展。
var TokenTagWhitelist = []string{"", "agent"}

// IsTokenTagValid 报告 tag 是否在 TokenTagWhitelist 内。
func IsTokenTagValid(tag string) bool {
	for _, valid := range TokenTagWhitelist {
		if valid == tag {
			return true
		}
	}
	return false
}
