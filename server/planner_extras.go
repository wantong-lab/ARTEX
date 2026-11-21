package server

// 平台动态上下文注入（P1-8）：把 KB 三档统计与 CVE 语料量注入 planner system prompt，
// 让规划者知道可用的知识弹药规模，并明确「线索≠已验证」的纪律。

import (
	"fmt"
	"strings"
)

// plannerContextExtras 返回给 planner 的附加系统提示；任何依赖不可用时静默跳过。
func (s *Server) plannerContextExtras() string {
	var lines []string
	if c := s.kbClient(); c != nil {
		if st, err := c.Stats(); err == nil {
			if total := st["_total"]; total > 0 {
				lines = append(lines, fmt.Sprintf(
					"知识库：共 %d 条（ammo=%d tactic=%d negative=%d）。用 kb_search 检索；negative 是已证伪打法，优先参考避免重复踩坑。",
					total, st["ammo"], st["tactic"], st["negative"]))
			}
		}
	}
	if c := s.cveClient(); c != nil {
		if n, err := c.Count(); err == nil && n > 0 {
			lines = append(lines, fmt.Sprintf(
				"本地 CVE 语料：%d 条（cve_search 可查）；版本/CVE 命中只作线索，必须实际触发验证后才能报漏洞。", n))
		}
	}
	return strings.Join(lines, "\n")
}
