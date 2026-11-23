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
		{
			Key:  "c2_ingress_deploy",
			Desc: "部署 C2 入站回调中继：目标只回连 VPS，平台 C2 listener 只绑 127.0.0.1。决策规则：平台有公网映射/VPS 能回连平台时用 mode=forward；平台在 NAT/手机热点、VPS 无法访问平台时用 mode=reverse（平台主动出站连 VPS）；无 VPS 且目标能直接访问平台时不要用本工具，改用平台 listener + 植入端。部署前先确认 VPS 回调端口已放行。",
			Schema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"mode":                 map[string]any{"type": "string", "enum": []any{"forward", "reverse"}, "description": "forward=VPS 回连平台（平台需公网可达）；reverse=平台主动连 VPS（平台无公网/NAT/热点时用）"},
					"session_id":           map[string]any{"type": "integer", "description": "到 VPS 的立足点会话 id"},
					"vps_host":             map[string]any{"type": "string", "description": "VPS 公网 host（目标回连地址；reverse 时也是控制地址）"},
					"vps_port":             map[string]any{"type": "integer", "description": "VPS 回调端口（目标回连端口）"},
					"platform_host":        map[string]any{"type": "string", "description": "forward：平台 chisel 公网 host；reverse 忽略"},
					"platform_chisel_port": map[string]any{"type": "integer", "description": "forward：平台 chisel 端口；reverse：VPS 控制端口（默认 39443）"},
					"c2_listener_id":       map[string]any{"type": "integer", "description": "已有 loopback C2 listener id；0=自动创建"},
					"c2_listener_kind":     map[string]any{"type": "string", "enum": []any{"http", "tcp"}, "description": "自动创建 listener 类型（默认 http）"},
					"implant_lang":         map[string]any{"type": "string", "enum": []any{"powershell", "python", "bash"}, "description": "HTTP 植入端语言（默认 powershell）"},
					"node_label":           map[string]any{"type": "string", "description": "VPS 节点标签（可选）"},
				},
				"required": []any{"session_id", "vps_host", "vps_port"},
			},
			Agents: []string{"worker", "pentest"},
		},
		{
			Key:  "c2_ingress_teardown",
			Desc: "停止 C2 入站中继：停隧道并停止关联 loopback C2 listener。清理残留是 OPSEC 的一部分。",
			Schema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"tunnel_id": map[string]any{"type": "integer", "description": "c2_ingress_deploy 返回的 tunnel_id"},
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
