package server

// 隧道工具（M2-03）：tunnel_deploy / tunnel_list / tunnel_teardown。
// 部署计划（tunnel.Plan）整体持久化在 tunnels.deploy_params，重拉/巡检都靠它；
// 状态变化经 tunnel.Manager.OnStateChange 通知任务级 MITM 热切换上游。

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/Autumn-27/artex/agent"
	"github.com/Autumn-27/artex/db"
	"github.com/Autumn-27/artex/stage"
	"github.com/Autumn-27/artex/tunnel"
	"github.com/Autumn-27/norma/permission"
	actool "github.com/Autumn-27/norma/tool"
)

const (
	tunnelPortMin = 20000
	tunnelPortMax = 20100
)

type tunnelRuntime struct {
	store *db.TunnelStore
	mgr   *tunnel.Manager
}

// resolveToolBinary 解析外部工具：环境变量优先，其次 dataDir/tools/<name>；不存在返回空。
func resolveToolBinary(envKey, dataDir, name string) string {
	if p := strings.TrimSpace(os.Getenv(envKey)); p != "" {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	p := filepath.Join(dataDir, "tools", name)
	if _, err := os.Stat(p); err == nil {
		return p
	}
	return ""
}

// tunnels 惰性初始化隧道运行时（台账 + 部署器 + 健康巡检 + 状态变更回调）。
func (s *Server) tunnels() *tunnelRuntime {
	s.tunnelMu.Lock()
	defer s.tunnelMu.Unlock()
	if s.tunnelRT != nil {
		return s.tunnelRT
	}
	store := db.NewTunnelStore(s.m.pg)
	stageMgr, err := stage.New(context.Background(), filepath.Join(s.m.dir, "stage"))
	if err != nil {
		log.Printf("[tunnel] stage 管理器初始化失败（隧道投递将报错）: %v", err)
	}
	mgr, err := tunnel.NewManager(tunnel.Options{
		Store: store, Stage: stageMgr, Sessions: s.sessions().reg,
		ChiselPath:      resolveToolBinary("ARTEX_CHISEL_PATH", s.m.dir, "chisel"),
		Suo5Path:        resolveToolBinary("ARTEX_SUO5_PATH", s.m.dir, "suo5"),
		Suo5PayloadsDir: filepath.Join(s.m.dir, "tools", "suo5-payloads"),
		DataDir:         s.m.dir,
	})
	if err != nil {
		log.Printf("[tunnel] 运行时初始化失败: %v", err)
		return nil
	}
	mgr.OnStateChange = func(taskID int64) {
		s.taskProxy().Refresh(context.Background(), taskID)
		s.syncTaskProxyToAgents(taskID)
	}
	if s.ctx != nil {
		mgr.StartHealth(s.ctx)
	}
	s.tunnelRT = &tunnelRuntime{store: store, mgr: mgr}
	return s.tunnelRT
}

// taskProxy 惰性初始化任务级 MITM 管理器（端口池 21100-21199）。
func (s *Server) taskProxy() *taskProxyManager {
	s.tunnelMu.Lock()
	defer s.tunnelMu.Unlock()
	if s.taskProxyRT == nil {
		s.taskProxyRT = newTaskProxyManager(s.m.dir, db.NewTunnelStore(s.m.pg), s.m.GlobalProxy, 21100, 21199)
	}
	return s.taskProxyRT
}

// syncTaskProxyToAgents 在隧道状态变化后刷新该任务已缓存 worker 的代理出口。
func (s *Server) syncTaskProxyToAgents(taskID int64) {
	if s.m.pg == nil || taskID <= 0 {
		return
	}
	s.taskAgentMu.Lock()
	b := s.taskAgents[fmt.Sprintf("%d", taskID)]
	s.taskAgentMu.Unlock()
	if b == nil || b.wk == nil {
		return
	}
	addr, ca := s.taskProxy().ForTask(context.Background(), taskID)
	if addr == "" {
		addr, ca = s.m.ProxyAddr(), s.m.ProxyCACert()
	}
	b.wk.SetProxy(addr, ca)
	log.Printf("[taskproxy] 任务 %d worker 代理已刷新为 %s", taskID, addr)
}

func (h *tunnelRuntime) deployTool() actool.CoreTool {
	return tunnelTool("tunnel_deploy",
		"经已登记会话部署一条隧道并登记台账。kind=socks 建 socks5 入口（后续任务 MITM 上游自动切到它）；kind=portfwd 建内网端口转发。"+
			"计划（含认证凭据/远端路径/进程 id）整体存 deploy_params，重拉/巡检都靠它。chisel 二进制缺失时返回明确错误。",
		map[string]any{
			"kind":        map[string]any{"type": "string", "enum": []any{"socks", "portfwd"}, "description": "隧道类型（必填）"},
			"session_id":  map[string]any{"type": "integer", "description": "经哪个立足点会话部署（register_session 返回）"},
			"adapter":     map[string]any{"type": "string", "description": "适配器：chisel（默认）/ suo5"},
			"listen_host": map[string]any{"type": "string", "description": "平台侧监听地址（默认 0.0.0.0）"},
			"target_host": map[string]any{"type": "string", "description": "portfwd：经隧道访问的内网目标 host"},
			"target_port": map[string]any{"type": "integer", "description": "portfwd：内网目标端口"},
		}, []string{"kind", "session_id"}, false,
		func(ctx context.Context, in json.RawMessage) (actool.Result, error) {
			var a struct {
				Kind       string `json:"kind"`
				SessionID  int64  `json:"session_id"`
				Adapter    string `json:"adapter"`
				ListenHost string `json:"listen_host"`
				TargetHost string `json:"target_host"`
				TargetPort int    `json:"target_port"`
			}
			_ = json.Unmarshal(in, &a)
			if h.mgr == nil {
				return actool.Errorf("隧道运行时不可用（台账未初始化）"), nil
			}
			if a.Kind != "socks" && a.Kind != "portfwd" {
				return actool.Errorf("kind 必须是 socks 或 portfwd"), nil
			}
			if a.SessionID <= 0 {
				return actool.Errorf("session_id 为必填参数"), nil
			}
			if a.Adapter == "" {
				a.Adapter = tunnel.AdapterChisel
			}
			used, err := h.mgr.OccupiedPorts(ctx)
			if err != nil {
				return actool.Errorf("读取端口占用失败：" + err.Error()), nil
			}
			ri := agent.RunInfoFrom(ctx)
			opts := &tunnel.PlanOpts{
				TaskID: ri.TaskID, ViaSessionID: a.SessionID, ListenHost: a.ListenHost,
				TargetHost: a.TargetHost, TargetPort: a.TargetPort,
				PortMin: tunnelPortMin, PortMax: tunnelPortMax,
				UsedPorts: used, CanListen: tunnel.CanListen,
				DataDir: filepath.Join(h.mgr.PlanDataDir()),
			}
			if host := h.callbackHost(); host != "" {
				opts.CallbackHost = host
			}
			var plan *tunnel.Plan
			if a.Kind == "socks" {
				plan, err = tunnel.PlanSocks(opts)
			} else {
				plan, err = tunnel.PlanPortfwd(opts)
			}
			if err != nil {
				return actool.Errorf("计划生成失败：" + err.Error()), nil
			}
			plan.Adapter = a.Adapter
			rec, err := h.mgr.Deploy(ctx, plan)
			if err != nil {
				return actool.Errorf("部署失败：" + err.Error()), nil
			}
			out, _ := json.Marshal(map[string]any{
				"tunnel_id": rec.ID, "kind": rec.Kind, "adapter": rec.Adapter,
				"state": rec.State, "listen_port": rec.ListenPort,
				"socks_port": plan.SocksPort, "local_port": plan.LocalPort,
				"entry": h.mgr.EntryAddr(plan),
			})
			return actool.Text(string(out)), nil
		})
}

func (h *tunnelRuntime) callbackHost() string {
	return strings.TrimSpace(os.Getenv("ARTEX_CALLBACK_ADDR"))
}

func (h *tunnelRuntime) listTool() actool.CoreTool {
	return tunnelTool("tunnel_list",
		"列本任务的隧道台账（状态/入口/上游信息），用于判断当前是否有可用 socks 上游。",
		map[string]any{
			"state": map[string]any{"type": "string", "description": "只看某状态（alive/error/stopped/deploying，可选）"},
		}, nil, true,
		func(ctx context.Context, in json.RawMessage) (actool.Result, error) {
			var a struct {
				State string `json:"state"`
			}
			_ = json.Unmarshal(in, &a)
			ri := agent.RunInfoFrom(ctx)
			recs, err := h.store.ListByTask(ctx, ri.TaskID)
			if err != nil {
				return actool.Errorf("读取台账失败：" + err.Error()), nil
			}
			out := []map[string]any{}
			for _, r := range recs {
				if a.State != "" && r.State != a.State {
					continue
				}
				out = append(out, map[string]any{
					"tunnel_id": r.ID, "kind": r.Kind, "adapter": r.Adapter,
					"state": r.State, "listen_port": r.ListenPort,
					"target_host": r.TargetHost, "target_port": r.TargetPort,
					"error": r.Error, "last_check": r.LastCheck,
				})
			}
			b, _ := json.Marshal(map[string]any{"count": len(out), "tunnels": out})
			return actool.Text(string(b)), nil
		})
}

func (h *tunnelRuntime) teardownTool() actool.CoreTool {
	return tunnelTool("tunnel_teardown",
		"停止并清理一条隧道：杀平台侧/目标侧进程、删远端文件、状态置 stopped。清理残留是 OPSEC 的一部分。",
		map[string]any{
			"tunnel_id": map[string]any{"type": "integer", "description": "隧道 id（必填）"},
		}, []string{"tunnel_id"}, false,
		func(ctx context.Context, in json.RawMessage) (actool.Result, error) {
			var a struct {
				TunnelID int64 `json:"tunnel_id"`
			}
			_ = json.Unmarshal(in, &a)
			if a.TunnelID <= 0 {
				return actool.Errorf("tunnel_id 为必填参数"), nil
			}
			if err := h.mgr.Teardown(ctx, a.TunnelID); err != nil {
				return actool.Errorf("停止失败：" + err.Error()), nil
			}
			out, _ := json.Marshal(map[string]any{"ok": true, "tunnel_id": a.TunnelID})
			return actool.Text(string(out)), nil
		})
}

// tools 返回三个隧道工具。
func (h *tunnelRuntime) tools() []actool.CoreTool {
	if h == nil {
		return nil
	}
	return []actool.CoreTool{h.deployTool(), h.listTool(), h.teardownTool()}
}

func tunnelTool(name, desc string, props map[string]any, required []string, readOnly bool, run func(context.Context, json.RawMessage) (actool.Result, error)) actool.CoreTool {
	if seed, ok := agent.TunnelToolMeta(name); ok {
		desc = seed.Desc
		props = nil
		if p, ok := seed.Schema["properties"].(map[string]any); ok {
			props = p
		}
		if req, ok := seed.Schema["required"].([]any); ok {
			required = nil
			for _, v := range req {
				if s, ok := v.(string); ok {
					required = append(required, s)
				}
			}
		}
	}
	schema := map[string]any{"type": "object", "properties": props}
	if len(required) > 0 {
		r := make([]any, len(required))
		for i, v := range required {
			r[i] = v
		}
		schema["required"] = r
	}
	return actool.Build(actool.Spec{
		Name: name, Description: desc, Schema: schema,
		ReadOnly:   func(json.RawMessage) bool { return readOnly },
		Concurrent: func(json.RawMessage) bool { return readOnly },
		Permissions: func(context.Context, json.RawMessage, permission.Context) permission.Decision {
			return permission.Allowed()
		},
		Run: func(ctx context.Context, in json.RawMessage, _ *actool.ToolContext) (actool.Result, error) {
			return run(ctx, in)
		},
	})
}

var _ = sync.Mutex{}
