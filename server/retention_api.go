package server

// 数据保留与清理 API（M5-02）：策略查看/修改、到期预览、dry-run 与真实清理、目标侧清理清单。
// 真实清理写 retention_audit；dry-run 只回 counts，不改数据、不写审计。

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Autumn-27/artex/db"
)

func (s *Server) registerRetentionRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/retention/status", s.retentionStatus)
	mux.HandleFunc("PUT /api/retention/policy", s.retentionSavePolicy)
	mux.HandleFunc("POST /api/retention/cleanup", s.retentionCleanup)
	mux.HandleFunc("GET /api/retention/cleanup-list", s.retentionCleanupList)
}

func (s *Server) retentionStore() *db.DB {
	if s.m == nil {
		return nil
	}
	return s.m.pg
}

func (s *Server) retentionStatus(w http.ResponseWriter, r *http.Request) {
	d := s.retentionStore()
	if d == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "数据库未就绪"})
		return
	}
	policy := d.LoadRetentionPolicy()
	counts, err := d.RetentionPreview(r.Context(), policy)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	last, err := d.LastRetentionAudit(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"policy": policy, "preview": counts, "last_cleanup": last,
	})
}

func (s *Server) retentionSavePolicy(w http.ResponseWriter, r *http.Request) {
	d := s.retentionStore()
	if d == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "数据库未就绪"})
		return
	}
	var p db.RetentionPolicy
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "请求体解析失败"})
		return
	}
	p = p.Normalize()
	if err := d.SaveRetentionPolicy(p); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "policy": p})
}

func (s *Server) retentionCleanup(w http.ResponseWriter, r *http.Request) {
	var body struct {
		DryRun bool   `json:"dry_run"`
		Actor  string `json:"actor"`
	}
	// 空 body 默认 dry-run：避免误发的空 POST 触发真实删除；真实清理必须显式 dry_run:false。
	dryRun := true
	if err := json.NewDecoder(r.Body).Decode(&body); err == nil {
		dryRun = body.DryRun
	} else if !errors.Is(err, io.EOF) {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "请求体解析失败"})
		return
	}
	d := s.retentionStore()
	if d == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "数据库未就绪"})
		return
	}
	actor := strings.TrimSpace(body.Actor)
	if actor == "" {
		actor = "admin"
	}
	res, err := d.RunRetentionCleanup(r.Context(), d.LoadRetentionPolicy(), actor, dryRun)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// retentionCleanupList 按 host_ledger 的持久化痕迹生成目标侧清理清单（artifact=true）。
func (s *Server) retentionCleanupList(w http.ResponseWriter, r *http.Request) {
	d := s.retentionStore()
	if d == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "数据库未就绪"})
		return
	}
	ops, err := d.ListHostOps(0, "", true, 500)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	items := make([]map[string]any, 0, len(ops))
	byHost := map[string]int{}
	now := time.Now()
	for _, op := range ops {
		byHost[op.Host]++
		items = append(items, map[string]any{
			"id": op.ID, "host": op.Host, "session_id": op.SessionID,
			"channel": op.Channel, "artifact_type": op.AtType,
			"cleanup_cmd": op.Cleanup, "note": op.Note, "created_at": op.CreatedAt,
			"age_days": int(now.Sub(op.CreatedAt).Hours() / 24),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items": items, "total": len(items), "by_host": byHost,
		"hint": "按 host 聚合的人工/AI 复核清单；实际删除由 session_exec 或运维执行",
	})
}
