package server

// CVE 语料库 API（M3-03）：status/search/sploitus/sync/autosync。
// 语料库在 data/cve（只读挂载也能开：OpenCVE 自带 immutable 回退）。同步任务分页拉取、
// 断点续跑，可随时中断。

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Autumn-27/artex/kb"
)

// 可测试替换点：Sploitus 检索（线上走 kb.SearchSploitus）。
var sploitusSearch = kb.SearchSploitus

// cveClient 惰性打开 CVE 语料库；不存在/打不开返回 nil（API 503，工具报明确错误）。
func (s *Server) cveClient() *kb.CVEClient {
	s.cveMu.Lock()
	defer s.cveMu.Unlock()
	if s.cveOnce {
		return s.cveDB
	}
	s.cveOnce = true
	dir := filepath.Join(s.m.dir, "cve")
	_ = os.MkdirAll(dir, 0o755)
	c, err := kb.OpenCVE(dir)
	if err != nil {
		log.Printf("[cve] 打开 CVE 语料库失败（检索将降级为不可用）: %v", err)
		return nil
	}
	s.cveDB = c
	return c
}

func (s *Server) registerCVERoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/cve/status", s.cveStatus)
	mux.HandleFunc("GET /api/cve/search", s.cveSearch)
	mux.HandleFunc("GET /api/cve/sploitus", s.cveSploitus)
	mux.HandleFunc("POST /api/cve/sync", s.cveSyncStart)
	mux.HandleFunc("POST /api/cve/sync/stop", s.cveSyncStop)
	mux.HandleFunc("POST /api/cve/autosync", s.cveAutosync)
}

func (s *Server) cveStatus(w http.ResponseWriter, r *http.Request) {
	state := kb.LoadCVESyncState(filepath.Join(s.m.dir, "cve"))
	s.cveMu.Lock()
	running := s.cveRunning
	s.cveMu.Unlock()
	c := s.cveClient()
	resp := map[string]any{"running": running, "sync": state}
	if c == nil {
		resp["ok"] = false
		resp["error"] = "CVE 语料库不可用"
		writeJSON(w, http.StatusOK, resp)
		return
	}
	n, err := c.Count()
	if err != nil {
		resp["ok"] = false
		resp["error"] = err.Error()
		writeJSON(w, http.StatusOK, resp)
		return
	}
	minY, maxY, _ := c.Years()
	resp["ok"] = true
	resp["count"] = n
	resp["year_min"] = minY
	resp["year_max"] = maxY
	if s.m != nil && s.m.pg != nil {
		if v, ok, err := s.m.pg.GetSetting("cve_autosync"); err == nil && ok {
			resp["autosync"] = v
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) cveSearch(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		writeJSON(w, http.StatusOK, map[string]any{"total": 0, "records": []any{}, "hint": "query 不能为空"})
		return
	}
	c := s.cveClient()
	if c == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "CVE 语料库不可用"})
		return
	}
	limit := 20
	if v, err := strconv.Atoi(strings.TrimSpace(r.URL.Query().Get("limit"))); err == nil && v > 0 && v <= 100 {
		limit = v
	}
	recs, err := c.Search(q, limit)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"total": len(recs), "records": recs})
}

func (s *Server) cveSploitus(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "query 不能为空"})
		return
	}
	limit := 20
	if v, err := strconv.Atoi(strings.TrimSpace(r.URL.Query().Get("limit"))); err == nil && v > 0 && v <= 50 {
		limit = v
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	items, total, err := sploitusSearch(ctx, q, strings.TrimSpace(r.URL.Query().Get("type")), limit)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": "Sploitus 检索失败: " + err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"total": total, "items": items})
}

// cveSyncStart 启动后台分页同步（可中断）；重复启动返回当前状态。
func (s *Server) cveSyncStart(w http.ResponseWriter, r *http.Request) {
	s.cveMu.Lock()
	if s.cveRunning {
		st := kb.LoadCVESyncState(filepath.Join(s.m.dir, "cve"))
		s.cveMu.Unlock()
		writeJSON(w, http.StatusAccepted, map[string]any{"running": true, "sync": st})
		return
	}
	s.cveRunning = true
	dir := filepath.Join(s.m.dir, "cve")
	_ = os.MkdirAll(dir, 0o755)
	feedURL := strings.TrimSpace(r.URL.Query().Get("feed_url"))
	s.cveMu.Unlock()
	s.cveMu.Lock()
	s.cveSyncCancel = nil
	s.cveMu.Unlock()
	ctx, cancel := context.WithCancel(context.Background())
	s.cveMu.Lock()
	s.cveSyncCancel = cancel
	s.cveMu.Unlock()
	go func() {
		defer func() {
			s.cveMu.Lock()
			s.cveRunning = false
			s.cveSyncCancel = nil
			s.cveMu.Unlock()
			cancel()
		}()
		st, err := kb.SyncCVEPaged(ctx, dir, kb.CVESyncOptions{FeedURL: feedURL})
		if err != nil {
			log.Printf("[cve] 同步中断/失败（可从断点续跑）: state=%+v err=%v", st, err)
		} else {
			log.Printf("[cve] 同步完成: processed=%d total=%d", st.Processed, st.Total)
		}
	}()
	state := kb.LoadCVESyncState(dir)
	writeJSON(w, http.StatusAccepted, map[string]any{"running": true, "sync": state})
}

// cveSyncStop 中断正在跑的同步：取消 ctx，已落库的页与断点保留（下次可续跑）。
func (s *Server) cveSyncStop(w http.ResponseWriter, r *http.Request) {
	s.cveMu.Lock()
	cancel := s.cveSyncCancel
	running := s.cveRunning
	s.cveMu.Unlock()
	if !running || cancel == nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "running": false, "hint": "没有正在运行的同步"})
		return
	}
	cancel()
	// 等后台 goroutine 收尾（保存断点），最多 3s。
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		s.cveMu.Lock()
		r := s.cveRunning
		s.cveMu.Unlock()
		if !r {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	st := kb.LoadCVESyncState(filepath.Join(s.m.dir, "cve"))
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "running": false, "sync": st})
}

// cveAutosync 记录自动同步开关（真正的调度由 M5 定时任务接管；这里只持久化意图）。
func (s *Server) cveAutosync(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Enabled *bool `json:"enabled"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	if body.Enabled == nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "enabled 必填"})
		return
	}
	val := "off"
	if *body.Enabled {
		val = "on"
	}
	if s.m == nil || s.m.pg == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "settings 存储不可用"})
		return
	}
	if err := s.m.pg.SetSetting("cve_autosync", val); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "autosync": val})
}

// 防未用导入。
var _ = fmt.Sprintf
var _ sync.Mutex
