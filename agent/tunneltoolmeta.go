package agent

// 隧道工具的目录元数据（M2-03）。handler 在 server/tunnel_tools.go（需要台账/部署器
// 依赖），这里只登记可枚举/可绑定的元数据，供 BuiltinToolSeeds 播种。

func TunnelToolSeeds() []ToolSeed {
	return []ToolSeed{
		{
			Key:  "tunnel_deploy",
			Desc: "经已登记会话部署隧道：kind=socks 建 socks5 入口（任务 MITM 上游自动切到它）；kind=portfwd 建内网端口转发。计划整体存 deploy_params，重拉/巡检靠它；chisel 缺失返回明确错误。",
			Schema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"kind":        map[string]any{"type": "string", "enum": []any{"socks", "portfwd"}, "description": "隧道类型（必填）"},
					"session_id":  map[string]any{"type": "integer", "description": "经哪个立足点会话部署"},
					"adapter":     map[string]any{"type": "string", "description": "适配器：chisel（默认）/ suo5"},
					"listen_host": map[string]any{"type": "string", "description": "平台侧监听地址（默认 0.0.0.0）"},
					"target_host": map[string]any{"type": "string", "description": "portfwd：内网目标 host"},
					"target_port": map[string]any{"type": "integer", "description": "portfwd：内网目标端口"},
				},
				"required": []any{"kind", "session_id"},
			},
			Agents: []string{"worker", "pentest"},
		},
		{
			Key:  "tunnel_list",
			Desc: "列本任务隧道台账（状态/入口/错误），用于判断当前 socks 上游是否可用。",
			Schema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"state": map[string]any{"type": "string", "description": "只看某状态（alive/error/stopped/deploying，可选）"},
				},
			},
			Agents: []string{"worker", "pentest"},
		},
		{
			Key:  "tunnel_teardown",
			Desc: "停止并清理隧道：杀双方进程、删远端文件、状态置 stopped。清理残留是 OPSEC 的一部分。",
			Schema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"tunnel_id": map[string]any{"type": "integer", "description": "隧道 id（必填）"},
				},
				"required": []any{"tunnel_id"},
			},
			Agents: []string{"worker", "pentest"},
		},
	}
}

// TunnelToolMeta 按名字查隧道工具目录条目。
func TunnelToolMeta(name string) (ToolSeed, bool) {
	for _, s := range TunnelToolSeeds() {
		if s.Key == name {
			return s, true
		}
	}
	return ToolSeed{}, false
}
