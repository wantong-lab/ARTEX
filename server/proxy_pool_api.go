package server

// 代理池运行时与 API（M4-03）：录入/探测/切换/回退；出站请求统一走带兜底的 client。

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Autumn-27/artex/db"
	"github.com/Autumn-27/artex/kb"
	"github.com/Autumn-27/artex/proxypool"
)

// proxyStoreAdapter 把 db.ProxyStore 适配成 proxypool.Store。
type proxyStoreAdapter struct{ store *db.ProxyStore }

func (a proxyStoreAdapter) List(ctx context.Context) ([]proxypool.Entry, error) {
	rows, err := a.store.List(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]proxypool.Entry, 0, len(rows))
	for _, e := range rows {
		out = append(out, proxypool.Entry{ID: e.ID, URL: e.URL, Status: e.Status})
	}
	return out, nil
}

func (a proxyStoreAdapter) Get(ctx context.Context, id int64) (proxypool.Entry, bool, error) {
	e, err := a.store.Get(ctx, id)
	if err != nil || e == nil {
		return proxypool.Entry{}, false, err
	}
	return proxypool.Entry{ID: e.ID, URL: e.URL, Status: e.Status}, true, nil
}

func (a proxyStoreAdapter) RecordFailure(ctx context.Context, id int64, msg string) error {
	return a.store.RecordFailure(ctx, id, msg)
}

func (a proxyStoreAdapter) UpdateProbe(ctx context.Context, id int64, exitIP string, latencyMs int, errMsg string) error {
	return a.store.UpdateProbe(ctx, id, exitIP, latencyMs, errMsg)
}

type proxyPoolRuntime struct {
	srv     *Server
	dbStore *db.ProxyStore
	mgr     *proxypool.Manager
}

// proxyPool 惰性初始化代理池运行时，并把出站 client 注入 Sploitus 检索。
func (s *Server) proxyPool() *proxyPoolRuntime {
	s.proxyMu.Lock()
	defer s.proxyMu.Unlock()
	if s.proxyRT != nil {
		return s.proxyRT
	}
	if s.m == nil || s.m.pg == nil {
		return nil
	}
	store := db.NewProxyStore(s.m.pg, s.jwtKey)
	mgr := proxypool.NewManager(proxyStoreAdapter{store: store})
	rt := &proxyPoolRuntime{srv: s, dbStore: store, mgr: mgr}
	if v, ok, err := s.m.pg.GetSetting("proxy_global_url"); err == nil && ok {
		if err := mgr.SetGlobal(strings.TrimSpace(v)); err != nil {
			log.Printf("[proxypool] 全局代理配置非法（已忽略）: %v", err)
		}
	}
	if v, ok, err := s.m.pg.GetSetting("proxy_probe_url"); err == nil && ok && strings.TrimSpace(v) != "" {
		mgr.SetProbeURL(strings.TrimSpace(v))
	}
	if v, ok, err := s.m.pg.GetSetting("proxy_active_id"); err == nil && ok {
		if id, _ := strconv.ParseInt(strings.TrimSpace(v), 10, 64); id > 0 {
			mgr.SetActive(id)
		}
	}
	kb.SetSploitusHTTPClient(mgr.Client())
	kb.SetCVEHTTPClient(mgr.Client()) // 统一出站：Sploitus + CVE 同步共用代理池回退链
	s.proxyRT = rt
	if s.m != nil {
		s.m.SetPoolEgress(rt.mgr.ActiveURL(context.Background()), rt.mgr.Global())
	}
	return rt
}

// syncPoolEgress 把代理池活动/兜底代理同步给 Manager；pool 模式下这是实际出口。
// rebuild=true 时重建 Agent，使 capture-off 的 bash/WebFetch 立刻换代理。
func (s *Server) syncPoolEgress(ctx context.Context, rebuild bool) {
	rt := s.proxyPool()
	if rt == nil || s.m == nil {
		return
	}
	active := rt.mgr.ActiveURL(ctx)
	if s.m != nil {
		s.m.SetPoolEgress(active, rt.mgr.Global())
	}
	if rebuild && s.m.EgressMode() == egressPool {
		s.rebuildAgentsForEgress()
	}
}

func (s *Server) rebuildAgentsForEgress() {
	s.cfgMu.Lock()
	cfg, on := s.llmCfg, s.llmOn
	s.cfgMu.Unlock()
	if on {
		if err := s.applyLLM(cfg); err != nil {
			log.Printf("[egress] 重建 Agent 失败: %v", err)
		}
	}
}

func (s *Server) registerProxyPoolRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/proxy-pool", s.proxyPoolList)
	mux.HandleFunc("POST /api/proxy-pool/entries", s.proxyPoolAdd)
	mux.HandleFunc("POST /api/proxy-pool/{id}/probe", s.proxyPoolProbe)
	mux.HandleFunc("POST /api/proxy-pool/{id}/switch", s.proxyPoolSwitch)
	mux.HandleFunc("POST /api/proxy-pool/fallback", s.proxyPoolFallback)
	mux.HandleFunc("PUT /api/proxy-pool/global", s.proxyPoolGlobal)
	mux.HandleFunc("POST /api/proxy-pool/egress", s.proxyPoolEgress)
	mux.HandleFunc("POST /api/proxy-pool/import-mihomo", s.proxyPoolImportMihomo)
	mux.HandleFunc("DELETE /api/proxy-pool/{id}", s.proxyPoolDelete)
	mux.HandleFunc("GET /api/proxy-pool/subscription", s.proxyPoolSubscriptionStatus)
	mux.HandleFunc("POST /api/proxy-pool/subscription", s.proxyPoolSubscriptionStart)
	mux.HandleFunc("DELETE /api/proxy-pool/subscription", s.proxyPoolSubscriptionStop)
	mux.HandleFunc("POST /api/proxy-pool/subscription/refresh", s.proxyPoolSubscriptionRefresh)
	mux.HandleFunc("GET /api/proxy-pool/subscription/nodes", s.proxyPoolSubscriptionNodes)
	mux.HandleFunc("POST /api/proxy-pool/subscription/select", s.proxyPoolSubscriptionSelect)
	mux.HandleFunc("POST /api/proxy-pool/subscription/probe", s.proxyPoolSubscriptionProbe)
	mux.HandleFunc("POST /api/proxy-pool/subscription/auto-rotate", s.proxyPoolSubscriptionAutoRotate)
	mux.HandleFunc("POST /api/proxy-pool/nodes/import", s.proxyPoolNodesImport)
	mux.HandleFunc("POST /api/proxy-pool/gateway", s.proxyPoolGatewayStart)
	mux.HandleFunc("POST /api/proxy-pool/gateway/rotate", s.proxyPoolGatewayRotate)
}

func (s *Server) proxyPoolList(w http.ResponseWriter, r *http.Request) {
	rt := s.proxyPool()
	if rt == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "代理池不可用（数据库未就绪）"})
		return
	}
	rows, err := rt.dbStore.List(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	out := make([]map[string]any, 0, len(rows))
	healthy, failed := 0, 0
	for _, e := range rows {
		auth := strings.Contains(e.URL, "@")
		checked := !e.LastChecked.IsZero()
		if e.Status == db.ProxyActive && checked {
			healthy++
		}
		if e.Status == db.ProxyFailed {
			failed++
		}
		row := map[string]any{
			"id": e.ID, "name": e.Name, "masked": e.Masked(), "scheme": e.Scheme, "auth": auth,
			"status": e.Status, "exit_ip": e.ExitIP, "latency_ms": e.LatencyMs,
			"fail_count": e.FailCount, "last_error": e.LastError,
		}
		if checked {
			row["last_checked"] = e.LastChecked
		}
		out = append(out, row)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"entries": out, "active_id": rt.mgr.Active(), "global_set": rt.mgr.Global() != "",
		"probe_url": rt.mgr.ProbeURL(), "healthy": healthy, "failed": failed,
		"unified_outbound": []string{"sploitus", "cve_sync"},
	})
}

func (s *Server) proxyPoolAdd(w http.ResponseWriter, r *http.Request) {
	rt := s.proxyPool()
	if rt == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "代理池不可用"})
		return
	}
	var body struct {
		URL     string              `json:"url"`
		Name    string              `json:"name"`
		Entries []map[string]string `json:"entries"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "请求体解析失败"})
		return
	}
	type item struct{ name, url string }
	var items []item
	for _, e := range body.Entries {
		items = append(items, item{name: e["name"], url: e["url"]})
	}
	if strings.TrimSpace(body.URL) != "" {
		items = append(items, item{name: body.Name, url: body.URL})
	}
	if len(items) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "url 或 entries 必填"})
		return
	}
	type result struct {
		ID   int64  `json:"id"`
		Dup  bool   `json:"duplicate"`
		Err  string `json:"error,omitempty"`
		Name string `json:"name,omitempty"`
	}
	results := make([]result, 0, len(items))
	for _, it := range items {
		if _, err := proxypool.ParseProxyURL(it.url); err != nil {
			results = append(results, result{Err: err.Error(), Name: it.name})
			continue
		}
		id, dup, err := rt.dbStore.Create(r.Context(), &db.ProxyEntry{Name: it.name, URL: it.url})
		if err != nil {
			results = append(results, result{Err: err.Error(), Name: it.name})
			continue
		}
		results = append(results, result{ID: id, Dup: dup, Name: it.name})
	}
	writeJSON(w, http.StatusOK, map[string]any{"results": results})
}

func proxyID(r *http.Request) int64 {
	id, _ := strconv.ParseInt(strings.TrimSpace(r.PathValue("id")), 10, 64)
	return id
}

func (s *Server) proxyPoolProbe(w http.ResponseWriter, r *http.Request) {
	rt := s.proxyPool()
	if rt == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "代理池不可用"})
		return
	}
	id := proxyID(r)
	if id <= 0 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "id 非法"})
		return
	}
	res, err := rt.mgr.Probe(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) proxyPoolSwitch(w http.ResponseWriter, r *http.Request) {
	rt := s.proxyPool()
	if rt == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "代理池不可用"})
		return
	}
	id := proxyID(r)
	e, err := rt.dbStore.Get(r.Context(), id)
	if err != nil || e == nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "代理不存在"})
		return
	}
	if e.Status == db.ProxyFailed {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "该代理最近探测失败，请先探测或修复"})
		return
	}
	rt.mgr.SetActive(id)
	if err := s.m.pg.SetSetting("proxy_active_id", strconv.FormatInt(id, 10)); err != nil {
		log.Printf("[proxypool] 保存活动代理失败: %v", err)
	}
	s.syncPoolEgress(r.Context(), true)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "active_id": id})
}

func (s *Server) proxyPoolFallback(w http.ResponseWriter, r *http.Request) {
	rt := s.proxyPool()
	if rt == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "代理池不可用"})
		return
	}
	rt.mgr.SetActive(0)
	_ = s.m.pg.SetSetting("proxy_active_id", "0")
	s.syncPoolEgress(r.Context(), true)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "active_id": 0, "global_set": rt.mgr.Global() != ""})
}

func (s *Server) proxyPoolGlobal(w http.ResponseWriter, r *http.Request) {
	rt := s.proxyPool()
	if rt == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "代理池不可用"})
		return
	}
	var body struct {
		URL string `json:"url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "请求体解析失败"})
		return
	}
	if err := rt.mgr.SetGlobal(strings.TrimSpace(body.URL)); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	if err := s.m.pg.SetSetting("proxy_global_url", strings.TrimSpace(body.URL)); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	s.syncPoolEgress(r.Context(), true)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "global_set": rt.mgr.Global() != ""})
}

// proxyPoolEgress 用当前活动代理（失败自动回退）探一次出口，证明切换生效。
func (s *Server) proxyPoolEgress(w http.ResponseWriter, r *http.Request) {
	rt := s.proxyPool()
	if rt == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "代理池不可用"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	exitIP, _, err := rt.mgr.Egress(ctx)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"ok": false, "error": err.Error(), "active_id": rt.mgr.Active()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "exit_ip": exitIP, "active_id": rt.mgr.Active(), "global_set": rt.mgr.Global() != ""})
}

func (s *Server) proxyPoolDelete(w http.ResponseWriter, r *http.Request) {
	rt := s.proxyPool()
	if rt == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "代理池不可用"})
		return
	}
	id := proxyID(r)
	if id <= 0 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "id 非法"})
		return
	}
	if rt.mgr.Active() == id {
		rt.mgr.SetActive(0)
		_ = s.m.pg.SetSetting("proxy_active_id", "0")
	}
	if err := rt.dbStore.Delete(r.Context(), id); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	s.syncPoolEgress(r.Context(), true)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

var _ sync.Mutex

// proxyPoolImportMihomo 从 mihomo/Clash YAML（或订阅 URL）导入 http/socks5 代理。
// validateSubscriptionURL 只允许 http/https 订阅地址，避免 file/其他 scheme。
func validateSubscriptionURL(raw string) error {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" {
		return fmt.Errorf("订阅地址非法")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("订阅地址仅支持 http/https")
	}
	return nil
}

func (s *Server) proxyPoolImportMihomo(w http.ResponseWriter, r *http.Request) {
	rt := s.proxyPool()
	if rt == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "代理池不可用"})
		return
	}
	var body struct {
		YAML       string `json:"yaml"`
		URL        string `json:"url"`
		NamePrefix string `json:"name_prefix"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<20)).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "请求体解析失败"})
		return
	}
	raw := body.YAML
	if strings.TrimSpace(raw) == "" {
		subURL := strings.TrimSpace(body.URL)
		if subURL == "" {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "yaml 或 url 必填"})
			return
		}
		if err := validateSubscriptionURL(subURL); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		}
		req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, subURL, nil)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		}
		req.Header.Set("User-Agent", "ARTEX-proxypool/1.0")
		req.Header.Set("Accept", "application/yaml,text/yaml,text/plain,*/*")
		resp, err := rt.mgr.Client().Do(req)
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]any{"error": "订阅拉取失败：" + err.Error()})
			return
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			writeJSON(w, http.StatusBadGateway, map[string]any{"error": fmt.Sprintf("订阅返回 HTTP %d", resp.StatusCode)})
			return
		}
		data, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error()})
			return
		}
		raw = string(data)
	}
	imp, err := proxypool.ParseMihomoConfig(raw)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	prefix := strings.TrimSpace(body.NamePrefix)
	imported := make([]map[string]any, 0, len(imp.Proxies))
	duplicates := 0
	errorsOut := make([]map[string]any, 0)
	for _, p := range imp.Proxies {
		u, err := p.URL()
		if err != nil {
			errorsOut = append(errorsOut, map[string]any{"name": p.Name, "error": err.Error()})
			continue
		}
		name := prefix + p.Name
		if strings.TrimSpace(name) == "" {
			name = strings.ToLower(p.Type) + "-" + p.Server
		}
		id, dup, err := rt.dbStore.Create(r.Context(), &db.ProxyEntry{Name: name, URL: u})
		if err != nil {
			errorsOut = append(errorsOut, map[string]any{"name": name, "error": err.Error()})
			continue
		}
		if dup {
			duplicates++
			continue
		}
		imported = append(imported, map[string]any{"id": id, "name": name})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok": true, "imported": imported, "imported_count": len(imported),
		"duplicates": duplicates, "skipped": imp.Skipped, "errors": errorsOut,
	})
}
