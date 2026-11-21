package agent

// WebShell 工具目录元数据（M4-02）。handler 在 server/webshell_tools.go。

func WebshellToolSeeds() []ToolSeed {
	return []ToolSeed{{
		Key:  "generate_webshell",
		Desc: "生成自研加密 WebShell 马体（php/jsp/aspx）：请求响应全密文、每份独立随机密钥、变量名随机化，可选轻度混淆。返回 content/filename/key_b64；上传后用 register_session(lang=phpenc/jspenc/aspxenc, secret=key_b64) 登记为加密会话。只在平台侧渲染，不触碰目标。",
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"lang":      map[string]any{"type": "string", "description": "马体语言：php / jsp / aspx（必填）"},
				"obfuscate": map[string]any{"type": "boolean", "description": "PHP 马体轻度语法变形（默认 false）"},
				"note":      map[string]any{"type": "string", "description": "备注（随结果返回，便于审计对照，可选）"},
			},
			"required": []any{"lang"},
		},
		Agents: []string{"worker", "pentest"},
	}}
}

// WebshellToolMeta 按名字查 WebShell 工具目录条目。
func WebshellToolMeta(name string) (ToolSeed, bool) {
	for _, s := range WebshellToolSeeds() {
		if s.Key == name {
			return s, true
		}
	}
	return ToolSeed{}, false
}
