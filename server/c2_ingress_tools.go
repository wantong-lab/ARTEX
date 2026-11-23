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

	"github.com/Autumn-27/artex/tunnel"
	actool "github.com/Autumn-27/norma/tool"
)

func (h *tunnelRuntime) c2IngressDeployTool() actool.CoreTool {
	return tunnelTool("c2_ingress_deploy",
		"部署 C2 入站回调中继：平台 C2 listener 只绑 127.0.0.1，目标只回连 VPS。mode=forward 表示 VPS 回连平台（平台需公网可达）；mode=reverse 表示平台主动出站连 VPS（适合平台在 NAT/手机热点）。返回植入端回调地址与内容。",
		map[string]any{
			"mode":                 map[string]any{"type": "string", "enum": []any{"forward", "reverse"}, "description": "forward=常规（VPS 回连平台）；reverse=反向（平台主动连 VPS，适合平台无公网入站）。默认 forward"},
			"session_id":           map[string]any{"type": "integer", "description": "到 VPS 的立足点会话 id"},
			"c2_listener_id":       map[string]any{"type": "integer", "description": "已有 C2 listener id（必须 bind 127.0.0.1）；0=自动创建"},
			"c2_listener_kind":     map[string]any{"type": "string", "enum": []any{"http", "tcp"}, "description": "自动创建时的监听器类型（默认 http）"},
			"vps_host":             map[string]any{"type": "string", "description": "VPS 公网 host（目标回连地址，也是 reverse 模式的控制地址）"},
			"vps_port":             map[string]any{"type": "integer", "description": "VPS 回调端口（目标回连端口，如 39444）"},
			"platform_host":        map[string]any{"type": "string", "description": "forward：平台 chisel 公网 host（默认 ARTEX_CALLBACK_ADDR）；reverse 忽略，使用 vps_host 作为控制地址"},
			"platform_chisel_port": map[string]any{"type": "integer", "description": "forward：平台 chisel 端口；reverse：VPS 控制端口（默认 39443）"},
			"implant_lang":         map[string]any{"type": "string", "enum": []any{"powershell", "python", "bash"}, "description": "HTTP 植入端语言（默认 powershell）；TCP 固定 python"},
			"node_label":           map[string]any{"type": "string", "description": "VPS 节点标签（多节点归属/展示，可选）"},
		}, []string{"session_id", "vps_host", "vps_port"}, false,
		func(ctx context.Context, in json.RawMessage) (actool.Result, error) {
			var a struct {
				Mode               string `json:"mode"`
				SessionID          int64  `json:"session_id"`
				C2ListenerID       int64  `json:"c2_listener_id"`
				C2ListenerKind     string `json:"c2_listener_kind"`
				VPSHost            string `json:"vps_host"`
				VPSPort            int    `json:"vps_port"`
				PlatformHost       string `json:"platform_host"`
				PlatformChiselPort int    `json:"platform_chisel_port"`
				ImplantLang        string `json:"implant_lang"`
				NodeLabel          string `json:"node_label"`
			}
			_ = json.Unmarshal(in, &a)
			if h == nil || h.mgr == nil || h.srv == nil {
				return actool.Errorf("隧道运行时不可用"), nil
			}
			out, err := h.deployC2Ingress(ctx, c2IngressRequest{
				Mode: a.Mode, SessionID: a.SessionID, C2ListenerID: a.C2ListenerID,
				C2ListenerKind: a.C2ListenerKind, VPSHost: a.VPSHost, VPSPort: a.VPSPort,
				PlatformHost: a.PlatformHost, PlatformChiselPort: a.PlatformChiselPort,
				ImplantLang: a.ImplantLang, NodeLabel: a.NodeLabel,
			})
			if err != nil {
				return actool.Errorf(err.Error()), nil
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
