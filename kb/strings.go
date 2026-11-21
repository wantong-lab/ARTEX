package kb

import "strings"

// trimSpaceLower 去空白 + 转小写（供 vulnclass 归一与匹配使用）。
func trimSpaceLower(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

// containsLower 在已小写的 haystack 里找 needle（needle 不区分大小写）。
func containsLower(haystack, needle string) bool {
	return strings.Contains(haystack, strings.ToLower(needle))
}
