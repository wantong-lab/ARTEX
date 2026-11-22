package server

// C2 入站回调中继工具（设计稿 dev-docs/design/2026-11-22-c2-ingress-relay.md）：
// 平台 C2 listener 只绑 127.0.0.1；VPS 侧 chisel client 本地监听 vps_port，
// 目标植入端只回连 VPS 地址，平台经 chisel 收到 C2 连接。

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"

	"github.com/Autumn-27/artex/agent"
	"github.com/Autumn-27/artex/c2"
	"github.com/Autumn-27/artex/db"
	"github.com/Autumn-27/artex/tunnel"
	actool "github.com/Autumn-27/norma/tool"
)

func (h *tunnelRuntime) c2IngressDeployTool() actool.CoreTool {
	return tunnelTool("c2_ingress_deploy",
		"部署 C2 入站回调中继：平台 C2 listener 只绑 127.0.0.1，VPS 侧监听 vps_port 并经 chisel 本地转发到平台；返回植入端回调地址与内容（目标只回连 VPS）。",
		map[string]any{
			"session_id":           map[string]any{"type": "integer", "description": "到 VPS 的立足点会话 id（register_session/隧道会话）"},
			"c2_listener_id":       map[string]any{"type": "integer", "description": "已有 C2 listener id（必须 bind 127.0.0.1）；0=自动创建"},
			"c2_listener_kind":     map[string]any{"type": "string", "enum": []any{"http", "tcp"}, "description": "自动创建时的监听器类型（默认 http）"},
			"vps_host":             map[string]any{"type": "string", "description": "植入端回连的 VPS 公网 host（必填）"},
			"vps_port":             map[string]any{"type": "integer", "description": "VPS 侧监听端口（目标回连端口，必填）"},
			"platform_host":        map[string]any{"type": "string", "description": "VPS 回连平台 chisel server 的公网 host（默认 ARTEX_CALLBACK_ADDR）"},
			"platform_chisel_port": map[string]any{"type": "integer", "description": "平台 chisel server 端口（默认 ARTEX_TUNNEL_PLATFORM_PORT）"},
			"implant_lang":         map[string]any{"type": "string", "enum": []any{"powershell", "python", "bash"}, "description": "HTTP 植入端语言（默认 powershell）；TCP 固定 python"},
		}, []string{"session_id", "vps_host", "vps_port"}, false,
		func(ctx context.Context, in json.RawMessage) (actool.Result, error) {
			var a struct {
				SessionID          int64  `json:"session_id"`
				C2ListenerID       int64  `json:"c2_listener_id"`
				C2ListenerKind     string `json:"c2_listener_kind"`
				VPSHost            string `json:"vps_host"`
				VPSPort            int    `json:"vps_port"`
				PlatformHost       string `json:"platform_host"`
				PlatformChiselPort int    `json:"platform_chisel_port"`
				ImplantLang        string `json:"implant_lang"`
			}
			_ = json.Unmarshal(in, &a)
			if h == nil || h.mgr == nil || h.srv == nil {
				return actool.Errorf("隧道运行时不可用"), nil
			}
			if a.SessionID <= 0 || strings.TrimSpace(a.VPSHost) == "" || a.VPSPort <= 0 {
				return actool.Errorf("session_id / vps_host / vps_port 为必填参数"), nil
			}
			rt := h.srv.c2rt()
			if rt == nil || !rt.enabled() {
				return actool.Errorf("C2 未启用（先 POST /api/c2/enable）"), nil
			}
			listenerID := a.C2ListenerID
			if listenerID <= 0 {
				kind := strings.ToLower(strings.TrimSpace(a.C2ListenerKind))
				if kind == "" {
					kind = db.C2ListenerKindHTTP
				}
				if kind != db.C2ListenerKindHTTP && kind != db.C2ListenerKindTCP {
					return actool.Errorf("c2_listener_kind 仅支持 http 或 tcp"), nil
				}
				port, perr := freeLoopbackPort()
				if perr != nil {
					return actool.Errorf("分配 C2 listener 端口失败：" + perr.Error()), nil
				}
				tok, terr := randomToken()
				if terr != nil {
					return actool.Errorf("生成 C2 token 失败：" + terr.Error()), nil
				}
				id, cerr := rt.store.CreateListener(ctx, &db.C2Listener{
					Name: "ingress-" + strconv.FormatInt(a.SessionID, 10), Kind: kind,
					BindHost: "127.0.0.1", BindPort: port, Token: tok,
				})
				if cerr != nil {
					return actool.Errorf("创建 C2 listener 失败：" + cerr.Error()), nil
				}
				listenerID = id
			}
			l, err := rt.store.GetListener(ctx, listenerID)
			if err != nil || l == nil {
				return actool.Errorf("C2 listener 不存在"), nil
			}
			if !isLoopbackHost(l.BindHost) {
				return actool.Errorf(fmt.Sprintf("C2 ingress 要求 listener bind 127.0.0.1，当前为 %s", l.BindHost)), nil
			}
			if l.Status != db.C2ListenerRunning {
				if err := rt.startOne(ctx, listenerID); err != nil {
					return actool.Errorf("启动 C2 listener 失败：" + err.Error()), nil
				}
			}
			platformHost := strings.TrimSpace(a.PlatformHost)
			if platformHost == "" {
				platformHost = strings.TrimSpace(h.callbackHost())
			}
			if platformHost == "" {
				platformHost = strings.TrimSpace(os.Getenv("ARTEX_TUNNEL_PLATFORM_HOST"))
			}
			if platformHost == "" {
				return actool.Errorf("platform_host 缺失：设 ARTEX_CALLBACK_ADDR 或传 platform_host"), nil
			}
			platformPort := a.PlatformChiselPort
			if platformPort <= 0 {
				platformPort = resolvePlatformChiselPort()
			}
			if platformPort <= 0 {
				return actool.Errorf("platform_chisel_port 缺失：传参或设 ARTEX_TUNNEL_PLATFORM_PORT"), nil
			}
			ri := agent.RunInfoFrom(ctx)
			plan, err := tunnel.PlanIngress(&tunnel.PlanOpts{
				TaskID: ri.TaskID, ViaSessionID: a.SessionID,
				ListenHost: "0.0.0.0", ListenPort: a.VPSPort,
				CallbackHost: platformHost, PlatformChiselPort: platformPort,
				C2CallbackHost: strings.TrimSpace(a.VPSHost), C2ListenerID: listenerID,
				TargetHost: "127.0.0.1", TargetPort: l.BindPort,
				DataDir: h.mgr.PlanDataDir(),
			})
			if err != nil {
				return actool.Errorf("ingress 计划生成失败：" + err.Error()), nil
			}
			rec, err := h.mgr.Deploy(ctx, plan)
			if err != nil {
				return actool.Errorf("C2 ingress 部署失败：" + err.Error()), nil
			}
			callback := net.JoinHostPort(strings.TrimSpace(a.VPSHost), strconv.Itoa(a.VPSPort))
			out := map[string]any{
				"tunnel_id": rec.ID, "state": rec.State, "listener_id": listenerID,
				"listener_kind": l.Kind, "callback_addr": callback,
				"platform_chisel_port": platformPort,
			}
			lang := strings.ToLower(strings.TrimSpace(a.ImplantLang))
			if l.Kind == db.C2ListenerKindHTTP {
				baseURL := "http://" + callback
				out["base_url"] = baseURL
				if lang == "" {
					lang = "powershell"
				}
				switch lang {
				case "powershell":
					out["implant"] = c2.HTTPImplantPowerShell(baseURL, l.Token)
				case "python":
					out["implant"] = c2.HTTPImplantPython(baseURL, l.Token)
				case "bash":
					out["implant"] = c2.HTTPImplantBash(baseURL, l.Token)
				default:
					return actool.Errorf("HTTP implant_lang 仅支持 powershell/python/bash"), nil
				}
				out["implant_lang"] = lang
			} else {
				key := make([]byte, 32)
				if _, err := rand.Read(key); err != nil {
					return actool.Errorf("生成 C2 信封密钥失败"), nil
				}
				id := "ingress-" + strconv.FormatInt(rec.ID, 10)
				out["implant"] = c2.PythonImplant(callback, rt.mgr.Token(), base64.StdEncoding.EncodeToString(key), id)
				out["implant_lang"] = "python"
			}
			b, _ := json.Marshal(out)
			return actool.Text(string(b)), nil
		})
}

func (h *tunnelRuntime) c2IngressTeardownTool() actool.CoreTool {
	return tunnelTool("c2_ingress_teardown",
		"停止 C2 入站中继：停隧道并停止关联的 loopback C2 listener（幂等）。",
		map[string]any{
			"tunnel_id": map[string]any{"type": "integer", "description": "c2_ingress_deploy 返回的 tunnel_id"},
		}, []string{"tunnel_id"}, false,
		func(ctx context.Context, in json.RawMessage) (actool.Result, error) {
			var a struct {
				TunnelID int64 `json:"tunnel_id"`
			}
			_ = json.Unmarshal(in, &a)
			if h == nil || h.mgr == nil || h.srv == nil || a.TunnelID <= 0 {
				return actool.Errorf("tunnel_id 为必填参数"), nil
			}
			rec, err := h.store.Get(ctx, a.TunnelID)
			if err != nil || rec == nil {
				return actool.Errorf(fmt.Sprintf("隧道 %d 不存在", a.TunnelID)), nil
			}
			var plan tunnel.Plan
			_ = json.Unmarshal(rec.DeployParams, &plan)
			if err := h.mgr.Teardown(ctx, a.TunnelID); err != nil {
				return actool.Errorf("停止隧道失败：" + err.Error()), nil
			}
			if plan.C2ListenerID > 0 {
				if rt := h.srv.c2rt(); rt != nil {
					_ = rt.stopOne(ctx, plan.C2ListenerID)
				}
			}
			return actool.Text(fmt.Sprintf(`{"ok":true,"tunnel_id":%d,"listener_id":%d}`, a.TunnelID, plan.C2ListenerID)), nil
		})
}

func resolvePlatformChiselPort() int {
	if v := strings.TrimSpace(os.Getenv("ARTEX_TUNNEL_PLATFORM_PORT")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 65535 {
			return n
		}
	}
	return 0
}

func freeLoopbackPort() (int, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	return port, nil
}

func randomToken() (string, error) {
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func isLoopbackHost(host string) bool {
	h := strings.TrimSpace(host)
	if h == "" || strings.EqualFold(h, "localhost") {
		return true
	}
	ip := net.ParseIP(h)
	return ip != nil && ip.IsLoopback()
}
