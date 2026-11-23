package server

// C2 入站中继健康巡检（P2-C）：周期检查 tunnel 状态、C2 listener 状态、
// VPS 回调端口可达性，把结果写回 c2_ingress 台账。

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"time"

	"github.com/Autumn-27/artex/db"
)

func (s *Server) c2IngressHealthLoop(ctx context.Context) {
	if s == nil || s.m == nil || s.m.pg == nil {
		return
	}
	// 启动后稍等，避免和 C2 listener 恢复/隧道重建抢时序。
	select {
	case <-ctx.Done():
		return
	case <-time.After(8 * time.Second):
	}
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		s.c2IngressHealthOnce(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *Server) c2IngressHealthOnce(ctx context.Context) {
	if s == nil || s.m == nil || s.m.pg == nil {
		return
	}
	rt := s.c2rt()
	if rt == nil {
		return
	}
	ingStore := db.NewC2IngressStore(s.m.pg)
	tunnelStore := db.NewTunnelStore(s.m.pg)
	recs, err := ingStore.ListActive(ctx)
	if err != nil {
		return
	}
	seen := map[int64]bool{}
	for _, ing := range recs {
		seen[ing.TunnelID] = true
		s.probeC2Ingress(ctx, rt, tunnelStore, ingStore, ing)
	}
	// 兼容升级前只存在 tunnels 台账的 ingress：补建记录。
	tunnels, _ := tunnelStore.ListIngress(ctx)
	for _, tr := range tunnels {
		if seen[tr.ID] {
			continue
		}
		var plan struct {
			C2ListenerID   int64  "json:\"c2_listener_id\""
			C2CallbackHost string "json:\"c2_callback_host\""
			ListenPort     int    "json:\"listen_port\""
			NodeLabel      string "json:\"node_label\""
			TaskID         int64  "json:\"task_id\""
		}
		_ = jsonUnmarshal(tr.DeployParams, &plan)
		_, _ = ingStore.Upsert(ctx, &db.C2IngressRecord{
			TunnelID: tr.ID, ListenerID: plan.C2ListenerID, TaskID: plan.TaskID,
			VPSHost: plan.C2CallbackHost, VPSPort: plan.ListenPort, NodeLabel: plan.NodeLabel,
			Status: "unknown",
		})
	}
}

func (s *Server) probeC2Ingress(ctx context.Context, rt *c2Runtime, tunnelStore *db.TunnelStore, ingStore *db.C2IngressStore, ing *db.C2IngressRecord) {
	state := ""
	if tr, _ := tunnelStore.Get(ctx, ing.TunnelID); tr != nil {
		state = tr.State
	}
	listenerStatus := ""
	if l, _ := rt.store.GetListener(ctx, ing.ListenerID); l != nil {
		listenerStatus = l.Status
	}
	probeErr := error(nil)
	addr := net.JoinHostPort(ing.VPSHost, strconv.Itoa(ing.VPSPort))
	conn, err := net.DialTimeout("tcp", addr, 3*time.Second)
	if err != nil {
		probeErr = err
	} else {
		_ = conn.Close()
	}
	healthy := state == db.TunnelAlive && listenerStatus == db.C2ListenerRunning && probeErr == nil
	status := "healthy"
	errMsg := ""
	if !healthy {
		status = "degraded"
		if state != "" && state != db.TunnelAlive {
			errMsg = "tunnel=" + state
		}
		if listenerStatus != "" && listenerStatus != db.C2ListenerRunning {
			errMsg = appendErr(errMsg, "listener="+listenerStatus)
		}
		if probeErr != nil {
			errMsg = appendErr(errMsg, "callback: "+probeErr.Error())
		}
		if errMsg == "" {
			errMsg = fmt.Sprintf("probe failed for %s", addr)
		}
	}
	_ = ingStore.UpdateHealth(ctx, ing.TunnelID, healthy, status, errMsg)
}

func appendErr(base, add string) string {
	if base == "" {
		return add
	}
	return base + "; " + add
}
