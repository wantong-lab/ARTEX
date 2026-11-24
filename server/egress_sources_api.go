package server

import (
	"encoding/json"
	"net/http"
	"strings"
)

func (s *Server) proxyPoolNodesImport(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Links string `json:"links"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<20)).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "请求体解析失败"})
		return
	}
	if strings.TrimSpace(body.Links) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "links 不能为空"})
		return
	}
	st, err := s.startLinksProxySubscription(r.Context(), body.Links)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "status": st})
}

func (s *Server) proxyPoolGatewayStart(w http.ResponseWriter, r *http.Request) {
	var body struct {
		URL   string `json:"url"`
		Count int    `json:"count"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "请求体解析失败"})
		return
	}
	if strings.TrimSpace(body.URL) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "url 不能为空"})
		return
	}
	st, err := s.startGatewayProxySubscription(r.Context(), body.URL, body.Count)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "status": st})
}

func (s *Server) proxyPoolGatewayRotate(w http.ResponseWriter, r *http.Request) {
	src, err := s.egressSourceStore().GetSubscription(r.Context())
	if err != nil || src == nil || src.Kind != "gateway" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "当前来源不是网关"})
		return
	}
	st, err := s.startGatewayProxySubscription(r.Context(), src.ConfigURL, 5)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "status": st})
}
