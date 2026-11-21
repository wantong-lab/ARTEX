package agent

// 会话工具的目录元数据（M2-02）。handler 由 server/session_tools.go 提供（需要 DB/
// 会话中枢依赖），这里只登记「可枚举、可绑定、可被 UI 覆盖描述」的元数据，
// 使 BuiltinToolSeeds 能把它们播种进 tools 表按 agent 绑定。schema 与运行时工具
// 保持一致（两处都改的工具约定见 server/session_tools.go 顶部注释）。

// SessionToolSeeds 返回四个会话工具的目录条目，默认绑定 worker + pentest。
func SessionToolSeeds() []ToolSeed {
	return []ToolSeed{
		{
			Key: "register_session",
			Desc: "【立足点主通道】把已拿到的 webshell 登记为平台管理的会话：先无害探针探测马型，成功才落库；" +
				"失败返回明确原因（不伪造）。登记目标 host 会过资产拦截(RoE)。lang: php/phpcmd/jsp/aspx/auto，加密马 phpenc/jspenc/aspxenc 需 secret。",
			Schema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"url":      map[string]any{"type": "string", "description": "webshell 完整 URL，含协议（必填）"},
					"password": map[string]any{"type": "string", "description": "连接密码（POST 字段名，默认 cmd；加密马不需要）"},
					"lang":     map[string]any{"type": "string", "description": "马语言：php / phpcmd / jsp / aspx / auto；phpenc / jspenc / aspxenc"},
					"secret":   map[string]any{"type": "string", "description": "加密马密钥（phpenc/jspenc/aspxenc 必填）"},
					"asset_id": map[string]any{"type": "integer", "description": "对应资产 id（可选）"},
				},
				"required": []any{"url"},
			},
			Agents: []string{"worker", "pentest"},
		},
		{
			Key:  "session_exec",
			Desc: "在已登记会话的目标侧执行一条 shell 命令（stderr 合并进 stdout）。输出上限 16KB。每次执行自动写 host_ledger。",
			Schema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"session_id":      map[string]any{"type": "integer", "description": "会话 id（register_session 返回）"},
					"command":         map[string]any{"type": "string", "description": "要执行的 shell 命令（必填）"},
					"timeout_seconds": map[string]any{"type": "integer", "description": "超时秒数（默认 60，最大 600）"},
				},
				"required": []any{"session_id", "command"},
			},
			Agents: []string{"worker", "pentest"},
		},
		{
			Key:  "session_read",
			Desc: "读会话目标侧文件（上限 1MB）。文本返回 text，二进制返回 base64；自动写 host_ledger。",
			Schema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"session_id": map[string]any{"type": "integer", "description": "会话 id"},
					"path":       map[string]any{"type": "string", "description": "目标侧文件路径（必填）"},
					"max_bytes":  map[string]any{"type": "integer", "description": "最多读取字节数（默认 524288，最大 1048576）"},
				},
				"required": []any{"session_id", "path"},
			},
			Agents: []string{"worker", "pentest"},
		},
		{
			Key:  "session_write",
			Desc: "写文件到会话目标侧（上限 1MB，二进制用 content_b64）。写文件是持久化痕迹：host_ledger 记 artifact=true 并给出 cleanup 命令。",
			Schema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"session_id":  map[string]any{"type": "integer", "description": "会话 id"},
					"path":        map[string]any{"type": "string", "description": "目标侧文件路径（必填）"},
					"content":     map[string]any{"type": "string", "description": "文本内容（与 content_b64 二选一）"},
					"content_b64": map[string]any{"type": "string", "description": "base64 内容（二进制）"},
					"append":      map[string]any{"type": "boolean", "description": "是否追加（默认覆盖）"},
				},
				"required": []any{"session_id", "path"},
			},
			Agents: []string{"worker", "pentest"},
		},
	}
}

// SessionToolMeta 按名字查会话工具目录条目（server 侧构造 handler 时复用 desc/schema）。
func SessionToolMeta(name string) (ToolSeed, bool) {
	for _, s := range SessionToolSeeds() {
		if s.Key == name {
			return s, true
		}
	}
	return ToolSeed{}, false
}
