package server

// 知识库 API（M3-02）：status / search / entry / backfill / kb-insights。
// 检索走 kb.Client.Search（FTS5 + 三档配比）；空查询直接返回空结果，宁缺毋滥。

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/Autumn-27/artex/kb"
)

// kbClient 惰性打开知识库（data/kb/knowledge.db）；失败返回 nil，工具/API 静默降级。
func (s *Server) kbClient() *kb.Client {
	s.kbMu.Lock()
	defer s.kbMu.Unlock()
	if s.kbOnce {
		return s.kbDB
	}
	s.kbOnce = true
	dir := strings.TrimSpace(s.m.dir + "/kb")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		log.Printf("[kb] 创建知识库目录失败: %v", err)
		return nil
	}
	c, err := kb.Open(dir)
	if err != nil {
		log.Printf("[kb] 打开知识库失败（检索/入库将降级为不可用）: %v", err)
		return nil
	}
	s.kbDB = c
	return c
}

func (s *Server) registerKBRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/kb/status", s.kbStatus)
	mux.HandleFunc("GET /api/kb/search", s.kbSearch)
	mux.HandleFunc("GET /api/kb/entry", s.kbEntry)
	mux.HandleFunc("GET /api/kb/entries", s.kbEntries)
	mux.HandleFunc("PUT /api/kb/entry", s.kbUpdateEntry)
	mux.HandleFunc("DELETE /api/kb/entry", s.kbDeleteEntry)
	mux.HandleFunc("POST /api/kb/backfill", s.kbBackfill)
	mux.HandleFunc("POST /api/kb/add", s.kbAdd)
	mux.HandleFunc("GET /api/v2/kb-insights", s.kbInsights)
}

func (s *Server) kbStatus(w http.ResponseWriter, r *http.Request) {
	c := s.kbClient()
	if c == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"ok": false, "error": "知识库不可用"})
		return
	}
	res, err := c.Search("", 1)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "dir": c.Dir(), "entries": res.Total})
}

func (s *Server) kbSearch(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		// 严格过滤：空查询不返回全量（避免调用方把整库当结果）。
		writeJSON(w, http.StatusOK, map[string]any{"total": 0, "entries": []any{}, "hint": "query 不能为空"})
		return
	}
	c := s.kbClient()
	if c == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "知识库不可用"})
		return
	}
	limit := 20
	if v, err := strconv.Atoi(strings.TrimSpace(r.URL.Query().Get("limit"))); err == nil && v > 0 && v <= 100 {
		limit = v
	}
	res, err := c.Search(q, limit)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) kbEntry(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.URL.Query().Get("id"))
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "id 必填"})
		return
	}
	c := s.kbClient()
	if c == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "知识库不可用"})
		return
	}
	e, err := c.Get(id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	if e == nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "条目不存在"})
		return
	}
	writeJSON(w, http.StatusOK, e)
}

func (s *Server) kbBackfill(w http.ResponseWriter, r *http.Request) {
	c := s.kbClient()
	if c == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "知识库不可用"})
		return
	}
	n, err := c.Reindex()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "indexed": n})
}

// kbAdd 手工入库（情报页「入库」区块）：幂等键可选（id）。
func (s *Server) kbAdd(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ID        string `json:"id"`
		Title     string `json:"title"`
		Category  string `json:"category"`
		VulnClass string `json:"vuln_class"`
		Product   string `json:"product"`
		CVE       string `json:"cve"`
		Content   string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "请求体解析失败"})
		return
	}
	if strings.TrimSpace(body.Title) == "" || strings.TrimSpace(body.Content) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "title 与 content 为必填"})
		return
	}
	c := s.kbClient()
	if c == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "知识库不可用"})
		return
	}
	id, err := c.Add(&kb.Entry{
		ID: strings.TrimSpace(body.ID), Title: strings.TrimSpace(body.Title),
		Category: strings.TrimSpace(body.Category), VulnClass: strings.TrimSpace(body.VulnClass),
		Product: strings.TrimSpace(body.Product), CVE: strings.TrimSpace(body.CVE),
		Content: body.Content, Verified: true,
	})
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "id": id})
}

// kbInsights 汇总：SQL GROUP BY 聚合（P1-8 起不再取前 500 条内存聚合）。
func (s *Server) kbInsights(w http.ResponseWriter, r *http.Request) {
	c := s.kbClient()
	if c == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "知识库不可用"})
		return
	}
	ins, err := c.Insights()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, ins)
}

// kbEntries 服务端分页列举（offset/limit/category）。
func (s *Server) kbEntries(w http.ResponseWriter, r *http.Request) {
	c := s.kbClient()
	if c == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "知识库不可用"})
		return
	}
	q := r.URL.Query()
	offset, _ := strconv.Atoi(strings.TrimSpace(q.Get("offset")))
	limit, _ := strconv.Atoi(strings.TrimSpace(q.Get("limit")))
	page, err := c.ListPage(offset, limit, strings.TrimSpace(q.Get("category")))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, page)
}

// kbUpdateEntry 编辑条目（title/content/category/vuln_class/product/cve 等）。
func (s *Server) kbUpdateEntry(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ID          string `json:"id"`
		Title       string `json:"title"`
		Category    string `json:"category"`
		VulnClass   string `json:"vuln_class"`
		Product     string `json:"product"`
		CVE         string `json:"cve"`
		Description string `json:"description"`
		Content     string `json:"content"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "请求体解析失败"})
		return
	}
	if strings.TrimSpace(body.ID) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "id 必填"})
		return
	}
	c := s.kbClient()
	if c == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "知识库不可用"})
		return
	}
	if err := c.Update(&kb.Entry{
		ID: body.ID, Title: strings.TrimSpace(body.Title), Category: strings.TrimSpace(body.Category),
		VulnClass: strings.TrimSpace(body.VulnClass), Product: strings.TrimSpace(body.Product),
		CVE: strings.TrimSpace(body.CVE), Description: body.Description, Content: body.Content,
	}); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "id": body.ID})
}

// kbDeleteEntry 删除条目（含 FTS 与相对路径正文文件）。
func (s *Server) kbDeleteEntry(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.URL.Query().Get("id"))
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "id 必填"})
		return
	}
	c := s.kbClient()
	if c == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "知识库不可用"})
		return
	}
	if err := c.Delete(id); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "id": id})
}

// 确保 sync 被使用（Server 字段类型在 server.go，这里仅防未用导入）。
var _ = sync.Mutex{}

// jsonUnused 占位，避免 json 在后续重构中被误删。
var _ = json.Marshal
