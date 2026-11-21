package agent

// 知识库工具目录元数据（M3-02）。handler 在 server/kb_tools.go（需要 kb 句柄）。

func KBToolSeeds() []ToolSeed {
	return []ToolSeed{
		{
			Key:  "kb_search",
			Desc: "检索知识库（历史打法/弹药/负面清单）。FTS5 严格匹配：没有相关结果就返回空，不要凭关键词硬凑；命中后按条目内容复用，不要照抄 payload。",
			Schema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query": map[string]any{"type": "string", "description": "检索词：漏洞类/组件/场景关键词（必填）"},
					"limit": map[string]any{"type": "integer", "description": "返回条数（默认 20，最大 100）"},
				},
				"required": []any{"query"},
			},
			Agents: []string{"worker", "planner", "pentest"},
		},
		{
			Key:  "kb_add",
			Desc: "把一条可复用的打法/弹药/负面经验写入知识库。id 可选：给稳定 id 时重复写入是幂等更新；不给则按标题生成。入库前自动归一类。",
			Schema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"id":         map[string]any{"type": "string", "description": "稳定 id（可选，幂等键）"},
					"title":      map[string]any{"type": "string", "description": "标题（必填）"},
					"category":   map[string]any{"type": "string", "description": "分类：ammo|tactic|negative（可选，自动推断）"},
					"vuln_class": map[string]any{"type": "string", "description": "漏洞类：rce|sqli|upload|deser|idor|..."},
					"product":    map[string]any{"type": "string", "description": "组件/产品（可选）"},
					"cve":        map[string]any{"type": "string", "description": "CVE 编号（可选）"},
					"content":    map[string]any{"type": "string", "description": "正文：完整打法/步骤/负面结论（必填）"},
				},
				"required": []any{"title", "content"},
			},
			Agents: []string{"worker", "planner", "pentest"},
		},
	}
}

// KBToolMeta 按名字查知识库工具目录条目。
func KBToolMeta(name string) (ToolSeed, bool) {
	for _, s := range KBToolSeeds() {
		if s.Key == name {
			return s, true
		}
	}
	return ToolSeed{}, false
}
