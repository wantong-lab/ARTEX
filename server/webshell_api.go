package server

// WebShell 生成与管理 API（M4-02）：生成马体、登记会话、执行、读取、清理。
// 执行/读取复用会话中枢；清理会写 host_ledger 供复核。

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Autumn-27/artex/session"
)

func (s *Server) registerWebshellRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/webshell/shells", s.webshellList)
	mux.HandleFunc("POST /api/webshell/generate", s.webshellGenerate)
	mux.HandleFunc("POST /api/webshell/register", s.webshellRegister)
	mux.HandleFunc("POST /api/webshell/exec", s.webshellExec)
	mux.HandleFunc("POST /api/webshell/read", s.webshellRead)
	mux.HandleFunc("DELETE /api/webshell/shells/{id}", s.webshellCleanup)
}

func (s *Server) webshellList(w http.ResponseWriter, r *http.Request) {
	h := s.sessions()
	shells, err := h.listShells(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	out := make([]map[string]any, 0, len(shells))
	for _, rec := range shells {
		out = append(out, map[string]any{
			"id": rec.ID, "kind": rec.Kind, "lang": rec.Lang, "status": rec.Status,
			"host": shellHost(rec.URL), "url": rec.URL, "host_asset_id": rec.HostAssetID,
			"created_by_task": rec.CreatedByTask, "last_beat": rec.LastBeat, "created_at": rec.CreatedAt,
			"artifact_path": rec.ArtifactPath,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"shells": out})
}

func (s *Server) webshellGenerate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Lang      string `json:"lang"`
		Obfuscate bool   `json:"obfuscate"`
		Note      string `json:"note"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "请求体解析失败"})
		return
	}
	gen, err := session.GenerateShell(body.Lang, body.Obfuscate, body.Note)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, gen)
}

func (s *Server) webshellRegister(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Kind        string `json:"kind"`
		URL         string `json:"url"`
		Password    string `json:"password"`
		Lang        string `json:"lang"`
		Secret      string `json:"secret"`
		AssetID     int64  `json:"asset_id"`
		Path        string `json:"path"`
		Host        string `json:"host"`
		Port        int    `json:"port"`
		User        string `json:"user"`
		Key         string `json:"key"`
		Proxy       string `json:"proxy"`
		Hash        string `json:"hash"`
		Proxychains bool   `json:"proxychains"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "请求体解析失败"})
		return
	}
	var (
		res map[string]any
		err error
	)
	switch strings.ToLower(strings.TrimSpace(body.Kind)) {
	case "ssh":
		res, err = s.sessions().registerSSHCore(r.Context(), session.SSHSecret{
			Host: body.Host, Port: body.Port, User: body.User,
			Password: body.Password, Key: body.Key, Proxy: body.Proxy,
		}, body.AssetID)
	case "smb", "smbexec":
		res, err = s.sessions().registerSMBCore(r.Context(), session.SMBSecret{
			Host: body.Host, User: body.User, Pass: body.Password, Hash: body.Hash, ProxyC: body.Proxychains,
		}, body.AssetID)
	default:
		res, err = s.sessions().registerCore(r.Context(), body.URL, body.Password, body.Lang, body.Secret, body.AssetID, body.Path)
	}
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) webshellExec(w http.ResponseWriter, r *http.Request) {
	var body struct {
		SessionID int64  `json:"session_id"`
		Command   string `json:"command"`
		Timeout   int    `json:"timeout_seconds"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "请求体解析失败"})
		return
	}
	res, err := s.sessions().execCore(r.Context(), body.SessionID, body.Command, time.Duration(body.Timeout)*time.Second)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) webshellRead(w http.ResponseWriter, r *http.Request) {
	var body struct {
		SessionID int64  `json:"session_id"`
		Path      string `json:"path"`
		MaxBytes  int    `json:"max_bytes"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "请求体解析失败"})
		return
	}
	res, err := s.sessions().readCore(r.Context(), body.SessionID, body.Path, body.MaxBytes)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) webshellCleanup(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(strings.TrimSpace(r.PathValue("id")), 10, 64)
	if err != nil || id <= 0 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "id 非法"})
		return
	}
	res, err := s.sessions().cleanupShell(r.Context(), id)
	if err != nil {
		status := http.StatusConflict
		if errors.Is(err, errShellNotFound) {
			status = http.StatusNotFound
		}
		writeJSON(w, status, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok": true, "cleanup": res["cleanup"], "physical": res["physical"],
		"verified": res["verified"], "artifact_path": res["artifact_path"],
	})
}

var _ = fmt.Sprintf
