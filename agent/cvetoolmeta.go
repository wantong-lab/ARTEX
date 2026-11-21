package agent

// CVE 工具目录元数据（M3-03）。handler 在 server/cve_tools.go（需要 CVE 语料库句柄）。

func CVEToolSeeds() []ToolSeed {
	return []ToolSeed{{
		Key:  "cve_search",
		Desc: "检索本地 CVE 语料库（组件/产品/漏洞名关键词）。命中只作线索：版本指纹匹配≠已确认可利用，必须实际触发验证后才能报漏洞；没有结果就按自己的判断继续。",
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query": map[string]any{"type": "string", "description": "检索词：组件/产品/CVE 编号/漏洞类（必填）"},
				"limit": map[string]any{"type": "integer", "description": "返回条数（默认 20，最大 100）"},
			},
			"required": []any{"query"},
		},
		Agents: []string{"worker", "pentest"},
	}}
}

// CVEToolMeta 按名字查 CVE 工具目录条目。
func CVEToolMeta(name string) (ToolSeed, bool) {
	for _, s := range CVEToolSeeds() {
		if s.Key == name {
			return s, true
		}
	}
	return ToolSeed{}, false
}
