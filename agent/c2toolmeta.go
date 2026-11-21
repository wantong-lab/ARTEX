package agent

// C2 工具目录元数据（M4-01）。handler 在 server/c2_tools.go。
// C2 默认关闭；工具在未启用时返回明确错误，不会静默下发。

func C2ToolSeeds() []ToolSeed {
	return []ToolSeed{
		{
			Key:  "c2_list_sessions",
			Desc: "列出 C2 在线植入端会话（默认关闭，需管理员显式启用的内网后渗透面）。返回 session_id 供 c2_exec 使用；没有会话就说明还没有植入端上线，不要臆造。",
			Schema: map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
			Agents: []string{"worker", "pentest"},
		},
		{
			Key:  "c2_exec",
			Desc: "在指定 C2 会话上执行命令并等待结果（受授权范围与 RoE 约束）。session_id 取自 c2_list_sessions；执行内容会落审计台账。",
			Schema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"session_id":  map[string]any{"type": "integer", "description": "会话 id（c2_list_sessions 返回）"},
					"command":     map[string]any{"type": "string", "description": "要执行的命令（必填）"},
					"timeout_sec": map[string]any{"type": "integer", "description": "超时秒数（默认 120，最大 300）"},
				},
				"required": []any{"session_id", "command"},
			},
			Agents: []string{"worker", "pentest"},
		},
		{
			Key:  "c2_task_result",
			Desc: "回读 C2 任务结果：传 task_id 查单条，不传则列最近任务。用于确认异步/中断的任务最终输出。",
			Schema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"task_id": map[string]any{"type": "integer", "description": "任务 id（可选）"},
				},
			},
			Agents: []string{"worker", "pentest"},
		},
	}
}

// C2ToolMeta 按名字查 C2 工具目录条目。
func C2ToolMeta(name string) (ToolSeed, bool) {
	for _, s := range C2ToolSeeds() {
		if s.Key == name {
			return s, true
		}
	}
	return ToolSeed{}, false
}
