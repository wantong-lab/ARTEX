package server

// C2 运行时与 API（M4-01）。默认关闭：settings c2_enabled=on 且配置了监听器才会 bind
// 端口；关闭状态下工具与监听全部不可用。监听器 token 加密落库，植入端用它握手。

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Autumn-27/artex/c2"
	"github.com/Autumn-27/artex/db"
)

type c2Runtime struct {
	srv *Server

	mu      sync.Mutex
	store   *db.C2Store
	mgr     *c2.Manager
	dbToMgr map[int64]int64 // DB listener id → manager listener id
	mgrToDB map[int64]int64 // manager listener id → DB listener id

	// HTTP beacon 监听器（P1-6）：独立 http.Server，按 DB listener id 管理。
	httpMu      sync.Mutex
	httpServers map[int64]*http.Server

	// execMu 串行化任务下发（同一植入端并发下发会串台输出）。
	execMu sync.Mutex
}

// c2rt 惰性初始化 C2 运行时（token 从 settings 读，缺失时生成并持久化）。
func (s *Server) c2rt() *c2Runtime {
	s.c2Mu.Lock()
	defer s.c2Mu.Unlock()
	if s.c2RT != nil {
		return s.c2RT
	}
	if s.m == nil || s.m.pg == nil {
		return nil
	}
	rt := &c2Runtime{srv: s, dbToMgr: map[int64]int64{}, mgrToDB: map[int64]int64{}, httpServers: map[int64]*http.Server{}}
	rt.store = db.NewC2Store(s.m.pg, s.jwtKey)
	token := ""
	if v, ok, err := s.m.pg.GetSetting("c2_token"); err == nil && ok {
		token = strings.TrimSpace(v)
	}
	if token == "" {
		buf := make([]byte, 24)
		if _, err := rand.Read(buf); err != nil {
			log.Printf("[c2] 生成 token 失败: %v", err)
			return nil
		}
		token = base64.RawURLEncoding.EncodeToString(buf)
		_ = s.m.pg.SetSetting("c2_token", token)
	}
	rt.mgr = c2.NewManager(token)
	rt.mgr.SetEventHandler(rt.onManagerEvent)
	s.c2RT = rt
	return rt
}

// Token 返回植入端握手 token（仅进程内可用；API 只暴露「已配置」）。
func (r *c2Runtime) Token() string { return r.mgr.Token() }

func (r *c2Runtime) onManagerEvent(kind string, info c2.SessionInfo) {
	r.mu.Lock()
	dbListener := r.mgrToDB[info.Listener]
	r.mu.Unlock()
	if dbListener == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	switch kind {
	case "session_new":
		id, err := r.store.UpsertSession(ctx, &db.C2Session{
			ListenerID: dbListener, ImplantUUID: info.ID, RemoteAddr: info.Remote,
			OS: info.OS, Arch: info.Arch, Username: info.User, Status: db.C2SessionActive,
		})
		if err != nil {
			log.Printf("[c2] 会话落库失败: %v", err)
			return
		}
		r.store.AddEvent(ctx, "session", fmt.Sprintf("植入端上线 %s（%s@%s）", info.ID, info.User, info.Remote), "info", dbListener, id, 0)
	case "session_dead":
		sessions, err := r.store.ListSessions(ctx, false)
		if err != nil {
			return
		}
		for _, sess := range sessions {
			if sess.ImplantUUID == info.ID && sess.ListenerID == dbListener && sess.Status == db.C2SessionActive {
				_ = r.store.SetSessionStatus(ctx, sess.ID, db.C2SessionDead)
			}
		}
	}
}

func (r *c2Runtime) enabled() bool {
	if r == nil || r.srv.m == nil || r.srv.m.pg == nil {
		return false
	}
	v, ok, err := r.srv.m.pg.GetSetting("c2_enabled")
	return err == nil && ok && strings.EqualFold(strings.TrimSpace(v), "on")
}

// Start 按台账启动所有监听器（幂等：已启动的跳过）。
func (r *c2Runtime) Start(ctx context.Context) error {
	if r == nil || !r.enabled() {
		return fmt.Errorf("C2 未启用：需显式设置 c2_enabled=on")
	}
	listeners, err := r.store.ListListeners(ctx)
	if err != nil {
		return err
	}
	for _, l := range listeners {
		if l.Kind == db.C2ListenerKindHTTP {
			if err := r.startHTTPOne(ctx, l); err != nil {
				_ = r.store.SetListenerStatus(ctx, l.ID, db.C2ListenerError, err.Error())
				log.Printf("[c2] HTTP 监听器 %d 启动失败（%s:%d）：%v", l.ID, l.BindHost, l.BindPort, err)
			}
			continue
		}
		r.mu.Lock()
		_, running := r.dbToMgr[l.ID]
		r.mu.Unlock()
		if running {
			continue
		}
		addr := net.JoinHostPort(l.BindHost, strconv.Itoa(l.BindPort))
		mgrID, boundAddr, err := r.mgr.Start(ctx, addr)
		if err != nil {
			_ = r.store.SetListenerStatus(ctx, l.ID, db.C2ListenerError, err.Error())
			log.Printf("[c2] 监听器 %d 启动失败（%s）: %v", l.ID, addr, err)
			continue
		}
		r.mu.Lock()
		r.dbToMgr[l.ID] = mgrID
		r.mgrToDB[mgrID] = l.ID
		r.mu.Unlock()
		_ = r.store.SetListenerStatus(ctx, l.ID, db.C2ListenerRunning, "")
		log.Printf("[c2] 监听器 %d 已启动 %s", l.ID, boundAddr)
	}
	return nil
}

func (r *c2Runtime) stopAll() {
	if r == nil {
		return
	}
	r.mu.Lock()
	ids := make([]int64, 0, len(r.dbToMgr))
	for dbID := range r.dbToMgr {
		ids = append(ids, dbID)
	}
	r.dbToMgr = map[int64]int64{}
	r.mgrToDB = map[int64]int64{}
	r.mu.Unlock()
	r.stopAllHTTP()
	r.mgr.StopAll()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for _, id := range ids {
		_ = r.store.SetListenerStatus(ctx, id, db.C2ListenerStopped, "")
	}
}

func (r *c2Runtime) startOne(ctx context.Context, dbID int64) error {
	l, err := r.store.GetListener(ctx, dbID)
	if err != nil || l == nil {
		return fmt.Errorf("监听器 %d 不存在", dbID)
	}
	if l.Kind == db.C2ListenerKindHTTP {
		return r.startHTTPOne(ctx, l)
	}
	addr := net.JoinHostPort(l.BindHost, strconv.Itoa(l.BindPort))
	mgrID, _, err := r.mgr.Start(ctx, addr)
	if err != nil {
		_ = r.store.SetListenerStatus(ctx, l.ID, db.C2ListenerError, err.Error())
		return err
	}
	r.mu.Lock()
	r.dbToMgr[l.ID] = mgrID
	r.mgrToDB[mgrID] = l.ID
	r.mu.Unlock()
	return r.store.SetListenerStatus(ctx, l.ID, db.C2ListenerRunning, "")
}

func (r *c2Runtime) stopOne(ctx context.Context, dbID int64) error {
	r.stopHTTPOne(ctx, dbID)
	r.mu.Lock()
	mgrID, ok := r.dbToMgr[dbID]
	if ok {
		delete(r.dbToMgr, dbID)
		delete(r.mgrToDB, mgrID)
	}
	r.mu.Unlock()
	if ok {
		r.mgr.Stop(mgrID)
	}
	return r.store.SetListenerStatus(ctx, dbID, db.C2ListenerStopped, "")
}

func (s *Server) registerC2Routes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/c2/status", s.c2Status)
	mux.HandleFunc("POST /api/c2/enable", s.c2Enable)
	mux.HandleFunc("GET /api/c2/listeners", s.c2ListListeners)
	mux.HandleFunc("POST /api/c2/listeners", s.c2CreateListener)
	mux.HandleFunc("POST /api/c2/listeners/{id}/start", s.c2StartListener)
	mux.HandleFunc("POST /api/c2/listeners/{id}/stop", s.c2StopListener)
	mux.HandleFunc("GET /api/c2/sessions", s.c2ListSessions)
	mux.HandleFunc("GET /api/c2/tasks", s.c2ListTasks)
	mux.HandleFunc("DELETE /api/c2/listeners/{id}", s.c2DeleteListener)
	mux.HandleFunc("POST /api/c2/sessions/{id}/tasks", s.c2QueueTask)
	mux.HandleFunc("GET /api/c2/implant", s.c2GenerateImplant)
	mux.HandleFunc("GET /api/c2/ingress", s.c2IngressList)
	mux.HandleFunc("POST /api/c2/ingress", s.c2IngressCreate)
	mux.HandleFunc("POST /api/c2/ingress/{id}/teardown", s.c2IngressTeardown)
}

func (s *Server) c2Status(w http.ResponseWriter, r *http.Request) {
	rt := s.c2rt()
	if rt == nil {
		writeJSON(w, http.StatusOK, map[string]any{"enabled": false, "listeners": []any{}, "sessions": 0, "error": "C2 运行时不可用"})
		return
	}
	ctx := r.Context()
	listeners, _ := rt.store.ListListeners(ctx)
	sessions, _ := rt.store.ListSessions(ctx, true)
	plain := make([]map[string]any, 0, len(listeners))
	for _, l := range listeners {
		plain = append(plain, map[string]any{
			"id": l.ID, "name": l.Name, "kind": l.Kind, "bind_host": l.BindHost, "bind_port": l.BindPort,
			"status": l.Status, "last_error": l.LastError, "started_at": l.StartedAt,
			"has_token": l.Token != "",
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"enabled": rt.enabled(), "listeners": plain, "sessions": len(sessions),
	})
}

func (s *Server) c2Enable(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Enabled *bool `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "请求体解析失败"})
		return
	}
	if body.Enabled == nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "enabled 必填"})
		return
	}
	rt := s.c2rt()
	if rt == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "C2 运行时不可用"})
		return
	}
	val := "off"
	if *body.Enabled {
		val = "on"
	}
	if err := s.m.pg.SetSetting("c2_enabled", val); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	if *body.Enabled {
		if err := rt.Start(r.Context()); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		}
	} else {
		rt.stopAll()
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "enabled": val})
}

func (s *Server) c2ListListeners(w http.ResponseWriter, r *http.Request) {
	rt := s.c2rt()
	if rt == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "C2 运行时不可用"})
		return
	}
	listeners, err := rt.store.ListListeners(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	out := make([]map[string]any, 0, len(listeners))
	for _, l := range listeners {
		out = append(out, map[string]any{
			"id": l.ID, "name": l.Name, "kind": l.Kind, "bind_host": l.BindHost, "bind_port": l.BindPort,
			"status": l.Status, "last_error": l.LastError, "has_token": l.Token != "",
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"listeners": out})
}

func (s *Server) c2CreateListener(w http.ResponseWriter, r *http.Request) {
	rt := s.c2rt()
	if rt == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "C2 运行时不可用"})
		return
	}
	var body struct {
		Name     string `json:"name"`
		Kind     string `json:"kind"`
		BindHost string `json:"bind_host"`
		BindPort int    `json:"bind_port"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "请求体解析失败"})
		return
	}
	if body.BindPort < 1 || body.BindPort > 65535 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "bind_port 必须在 1-65535"})
		return
	}
	if strings.TrimSpace(body.BindHost) == "" {
		body.BindHost = "127.0.0.1"
	}
	// P2-13：默认只允许回环绑定；对外监听需显式开启 c2_allow_non_loopback。
	allowNonLoopback := false
	if s.m != nil && s.m.pg != nil {
		allowNonLoopback = s.m.pg.GetBool("c2_allow_non_loopback", false)
	}
	if err := c2.ValidateBindHost(body.BindHost, allowNonLoopback); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error(), "setting": "c2_allow_non_loopback"})
		return
	}
	body.Kind = strings.ToLower(strings.TrimSpace(body.Kind))
	if body.Kind == "" {
		body.Kind = db.C2ListenerKindTCP
	}
	if body.Kind != db.C2ListenerKindTCP && body.Kind != db.C2ListenerKindHTTP {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "kind 仅支持 tcp 或 http"})
		return
	}
	tokBuf := make([]byte, 24)
	if _, err := rand.Read(tokBuf); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	token := base64.RawURLEncoding.EncodeToString(tokBuf)
	id, err := rt.store.CreateListener(r.Context(), &db.C2Listener{
		Name: body.Name, Kind: body.Kind, BindHost: body.BindHost, BindPort: body.BindPort, Token: token,
	})
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	// token 只在创建时返回一次（不入列表接口）。
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "id": id, "token": token})
}

func (s *Server) c2StartListener(w http.ResponseWriter, r *http.Request) {
	rt := s.c2rt()
	if rt == nil || !rt.enabled() {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "C2 未启用（先 POST /api/c2/enable）"})
		return
	}
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	// 兜底：历史监听器/设置被关掉后再次启动时也要复核回环约束。
	if l, err := rt.store.GetListener(r.Context(), id); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	} else if l == nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "监听器不存在"})
		return
	} else {
		allowNonLoopback := false
		if s.m != nil && s.m.pg != nil {
			allowNonLoopback = s.m.pg.GetBool("c2_allow_non_loopback", false)
		}
		if err := c2.ValidateBindHost(l.BindHost, allowNonLoopback); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error(), "setting": "c2_allow_non_loopback"})
			return
		}
	}
	if err := rt.startOne(r.Context(), id); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) c2StopListener(w http.ResponseWriter, r *http.Request) {
	rt := s.c2rt()
	if rt == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "C2 运行时不可用"})
		return
	}
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err := rt.stopOne(r.Context(), id); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) c2ListSessions(w http.ResponseWriter, r *http.Request) {
	rt := s.c2rt()
	if rt == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "C2 运行时不可用"})
		return
	}
	sessions, err := rt.store.ListSessions(r.Context(), strings.EqualFold(r.URL.Query().Get("active"), "1"))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"sessions": sessions})
}

func (s *Server) c2ListTasks(w http.ResponseWriter, r *http.Request) {
	rt := s.c2rt()
	if rt == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "C2 运行时不可用"})
		return
	}
	sessionID, _ := strconv.ParseInt(strings.TrimSpace(r.URL.Query().Get("session_id")), 10, 64)
	limit, _ := strconv.Atoi(strings.TrimSpace(r.URL.Query().Get("limit")))
	tasks, err := rt.store.ListTasks(r.Context(), sessionID, limit)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"tasks": tasks})
}

// startC2IfEnabled 在服务启动时按设置尝试拉起监听器（失败不影响主服务）。
func (s *Server) startC2IfEnabled(ctx context.Context) {
	rt := s.c2rt()
	if rt == nil || !rt.enabled() {
		return
	}
	if err := rt.Start(ctx); err != nil {
		log.Printf("[c2] 启动时拉起监听器失败: %v", err)
	}
}
