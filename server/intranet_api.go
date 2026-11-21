package server

// 内网中枢只读 API + 范围审批 + 立足点移交（M2-05）。
// 页面只读展示会话/隧道/凭据/主机台账/范围审批队列；秘密一律脱敏返回。

import (
	"context"
	"encoding/json"
	"log"
	"net"
	"net/http"
	"strconv"
	"strings"

	pgdb "github.com/Autumn-27/artex/db"
)

func (s *Server) registerIntranetRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/intranet/sessions", s.intranetSessions)
	mux.HandleFunc("GET /api/intranet/tunnels", s.intranetTunnels)
	mux.HandleFunc("GET /api/intranet/credentials", s.intranetCredentials)
	mux.HandleFunc("POST /api/intranet/credentials", s.intranetCreateCredential)
	mux.HandleFunc("GET /api/intranet/credentials/{id}/secret", s.intranetRevealCredential)
	mux.HandleFunc("DELETE /api/intranet/credentials/{id}", s.intranetDeleteCredential)
	mux.HandleFunc("GET /api/intranet/host-ledger", s.intranetHostLedger)
	mux.HandleFunc("GET /api/intranet/pending-scope", s.intranetPendingScope)
	mux.HandleFunc("POST /api/intranet/pending-scope/{id}/approve", s.intranetApproveScope)
	mux.HandleFunc("POST /api/intranet/pending-scope/{id}/reject", s.intranetRejectScope)
	mux.HandleFunc("POST /api/intranet/handoff", s.intranetHandoff)
}

func intranetTaskID(r *http.Request) int64 {
	v := strings.TrimSpace(r.URL.Query().Get("task_id"))
	if v == "" {
		return 0
	}
	id, _ := strconv.ParseInt(v, 10, 64)
	return id
}

func (s *Server) intranetSessions(w http.ResponseWriter, r *http.Request) {
	store := pgdb.NewSessionStore(s.m.pg, s.jwtKey)
	ctx := r.Context()
	taskID := intranetTaskID(r)
	var (
		recs []*pgdb.SessionRecord
		err  error
	)
	if taskID > 0 {
		recs, err = store.ListByTask(ctx, taskID)
	} else {
		recs, err = store.List(ctx, strings.TrimSpace(r.URL.Query().Get("status")))
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	out := make([]map[string]any, 0, len(recs))
	for _, rec := range recs {
		out = append(out, map[string]any{
			"id": rec.ID, "kind": rec.Kind, "url": rec.URL, "lang": rec.Lang,
			"status": rec.Status, "host_asset_id": rec.HostAssetID,
			"created_by_task": rec.CreatedByTask, "created_by_intent": rec.CreatedByIntent,
			"last_beat": rec.LastBeat, "created_at": rec.CreatedAt,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"sessions": out})
}

func (s *Server) intranetTunnels(w http.ResponseWriter, r *http.Request) {
	store := pgdb.NewTunnelStore(s.m.pg)
	ctx := r.Context()
	taskID := intranetTaskID(r)
	var (
		recs []*pgdb.TunnelRecord
		err  error
	)
	if taskID > 0 {
		recs, err = store.ListByTask(ctx, taskID)
	} else {
		recs, err = store.List(ctx)
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	out := make([]map[string]any, 0, len(recs))
	for _, rec := range recs {
		out = append(out, map[string]any{
			"id": rec.ID, "task_id": rec.TaskID, "kind": rec.Kind, "adapter": rec.Adapter,
			"state": rec.State, "listen_host": rec.ListenHost, "listen_port": rec.ListenPort,
			"target_host": rec.TargetHost, "target_port": rec.TargetPort,
			"via_session_id": rec.ViaSessionID, "error": rec.Error,
			"last_check": rec.LastCheck, "created_at": rec.CreatedAt,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"tunnels": out})
}

func (s *Server) intranetCredentials(w http.ResponseWriter, r *http.Request) {
	store := pgdb.NewCredentialStore(s.m.pg, s.jwtKey)
	ctx := r.Context()
	taskID := intranetTaskID(r)
	var (
		recs []*pgdb.CredentialRecord
		err  error
	)
	if taskID > 0 {
		recs, err = store.ListByTask(ctx, taskID)
	} else {
		recs, err = store.ListAll(ctx)
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	out := make([]map[string]any, 0, len(recs))
	for _, rec := range recs {
		row := map[string]any{
			"id": rec.ID, "task_id": rec.TaskID, "username": rec.Username,
			"cred_type": rec.CredType, "domain": rec.Domain, "source": rec.Source,
			"verified": rec.Verified, "secret_ok": rec.SecretOK, "created_at": rec.CreatedAt,
		}
		if rec.SecretOK {
			row["secret_masked"] = pgdb.MaskSecret(rec.Secret)
		} else {
			row["secret_error"] = rec.SecretError
		}
		out = append(out, row)
	}
	writeJSON(w, http.StatusOK, map[string]any{"credentials": out})
}

func (s *Server) intranetHostLedger(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	sessionID, _ := strconv.ParseInt(strings.TrimSpace(q.Get("session_id")), 10, 64)
	host := strings.TrimSpace(q.Get("host"))
	artifacts := q.Get("artifacts") == "1" || q.Get("artifacts") == "true"
	limit := 200
	if v, err := strconv.Atoi(strings.TrimSpace(q.Get("limit"))); err == nil && v > 0 {
		limit = v
	}
	ops, err := s.m.pg.ListHostOps(sessionID, host, artifacts, limit)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ops": ops})
}

func (s *Server) intranetPendingScope(w http.ResponseWriter, r *http.Request) {
	store := pgdb.NewPendingScopeStore(s.m.pg)
	status := strings.TrimSpace(r.URL.Query().Get("status"))
	if status == "" {
		status = "pending"
	}
	rows, err := store.List(r.Context(), intranetTaskID(r), status)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"requests": rows})
}

func (s *Server) intranetApproveScope(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if id <= 0 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "id 无效"})
		return
	}
	reviewer := "web"
	var body struct {
		Reviewer string `json:"reviewer"`
	}
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&body)
	}
	if strings.TrimSpace(body.Reviewer) != "" {
		reviewer = strings.TrimSpace(body.Reviewer)
	}
	store := pgdb.NewPendingScopeStore(s.m.pg)
	if err := store.Approve(r.Context(), id, reviewer, s.m.Assets()); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) intranetRejectScope(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if id <= 0 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "id 无效"})
		return
	}
	store := pgdb.NewPendingScopeStore(s.m.pg)
	if err := store.Reject(r.Context(), id, "web"); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// intranetHandoff 以会话为起点建子任务：初始 scope 取会话目标的单主机（/32 或域名），
// 与父任务通过 SourceTaskIDs 建立血缘。
func (s *Server) intranetHandoff(w http.ResponseWriter, r *http.Request) {
	var body struct {
		TaskID      int64  `json:"task_id"`
		SessionID   int64  `json:"session_id"`
		Goal        string `json:"goal"`
		Description string `json:"description"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "请求体解析失败"})
		return
	}
	if body.SessionID <= 0 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "session_id 为必填"})
		return
	}
	ctx := r.Context()
	rec, err := pgdb.NewSessionStore(s.m.pg, s.jwtKey).Get(ctx, body.SessionID)
	if err != nil || rec == nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "会话不存在"})
		return
	}
	parentID := body.TaskID
	if parentID <= 0 {
		parentID = rec.CreatedByTask
	}
	goal := strings.TrimSpace(body.Goal)
	if goal == "" {
		goal = "以立足点会话为起点继续内网渗透"
	}
	desc := strings.TrimSpace(body.Description)
	if desc == "" {
		desc = "由会话 #" + strconv.FormatInt(body.SessionID, 10) + " 移交的子任务"
	}
	opts := pgdb.TaskCreateOptions{}
	if parentID > 0 {
		opts.SourceTaskIDs = []int64{parentID}
	}
	t, err := s.m.CreateTaskWithOptions(desc, goal, opts)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	// 初始范围：会话目标单主机（IP → /32，域名 → subdomain）。
	var scopeErr error
	if host := hostOfURL(rec.URL); host != "" {
		kind := "subdomain"
		if net.ParseIP(host) != nil {
			kind = "ip"
		}
		if as := s.m.Assets(); as != nil {
			_, scopeErr = as.AddAgentScope(taskIDOf(t), kind, host, "立足点移交初始范围", "agent")
		}
	}
	resp := map[string]any{"task_id": t.ID, "session_id": body.SessionID}
	if scopeErr != nil {
		resp["scope_error"] = scopeErr.Error()
	}
	writeJSON(w, http.StatusOK, resp)
}

func hostOfURL(raw string) string {
	s := strings.TrimSpace(raw)
	if i := strings.Index(s, "://"); i >= 0 {
		s = s[i+3:]
	}
	if j := strings.IndexAny(s, "/?#"); j >= 0 {
		s = s[:j]
	}
	if h, _, err := net.SplitHostPort(s); err == nil {
		s = h
	}
	return strings.ToLower(strings.TrimSpace(s))
}

func taskIDOf(t *Task) int64 {
	if t == nil {
		return 0
	}
	id, _ := strconv.ParseInt(t.ID, 10, 64)
	return id
}

var _ = context.Background

// intranetCreateCredential 手动登记凭据（agent 之外的人工录入/复用入口）。
func (s *Server) intranetCreateCredential(w http.ResponseWriter, r *http.Request) {
	var body struct {
		TaskID      int64  `json:"task_id"`
		HostAssetID int64  `json:"host_asset_id"`
		Username    string `json:"username"`
		CredType    string `json:"cred_type"`
		Secret      string `json:"secret"`
		Domain      string `json:"domain"`
		Source      string `json:"source"`
		Verified    bool   `json:"verified"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "请求体解析失败"})
		return
	}
	body.Username = strings.TrimSpace(body.Username)
	body.CredType = strings.TrimSpace(body.CredType)
	body.Secret = strings.TrimSpace(body.Secret)
	if body.Username == "" || body.Secret == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "username 与 secret 必填"})
		return
	}
	if body.TaskID <= 0 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "task_id 必填（凭据按任务归属）"})
		return
	}
	if !pgdb.ValidCredType(body.CredType) {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "cred_type 非法"})
		return
	}
	source := strings.TrimSpace(body.Source)
	if source == "" {
		source = "web"
	}
	store := pgdb.NewCredentialStore(s.m.pg, s.jwtKey)
	id, err := store.Create(r.Context(), &pgdb.CredentialRecord{
		TaskID: body.TaskID, HostAssetID: body.HostAssetID, Username: body.Username,
		CredType: body.CredType, Secret: body.Secret, Domain: strings.TrimSpace(body.Domain),
		Source: source, Verified: body.Verified,
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"ok": true, "id": id})
}

// intranetRevealCredential 按需返回明文凭据（用于复用）；每次读取写服务端日志。
func (s *Server) intranetRevealCredential(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(strings.TrimSpace(r.PathValue("id")), 10, 64)
	if err != nil || id <= 0 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "id 非法"})
		return
	}
	store := pgdb.NewCredentialStore(s.m.pg, s.jwtKey)
	rec, err := store.Get(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "凭据解密失败：" + err.Error()})
		return
	}
	if rec == nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "凭据不存在"})
		return
	}
	log.Printf("[credential] 明文读取 id=%d username=%s source=%s", rec.ID, rec.Username, rec.Source)
	writeJSON(w, http.StatusOK, map[string]any{
		"id": rec.ID, "username": rec.Username, "cred_type": rec.CredType,
		"secret": rec.Secret, "domain": rec.Domain, "verified": rec.Verified,
	})
}

func (s *Server) intranetDeleteCredential(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(strings.TrimSpace(r.PathValue("id")), 10, 64)
	if err != nil || id <= 0 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "id 非法"})
		return
	}
	store := pgdb.NewCredentialStore(s.m.pg, s.jwtKey)
	if err := store.Delete(r.Context(), id); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
