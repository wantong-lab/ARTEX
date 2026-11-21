package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Autumn-27/artex/kb"
)

// newKBTestServer 构造只带数据目录的最小 Server（kb 相关 handler 只需要 m.dir）。
func newKBTestServer(t *testing.T) *Server {
	t.Helper()
	s := &Server{m: &Manager{dir: t.TempDir()}}
	c := s.kbClient()
	if c == nil {
		t.Fatal("kb client should open in temp dir")
	}
	t.Cleanup(func() { _ = c.Close() })
	return s
}

func doGET(t *testing.T, h http.HandlerFunc, path string) map[string]any {
	t.Helper()
	rec := httptest.NewRecorder()
	h(rec, httptest.NewRequest(http.MethodGet, path, nil))
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("bad json %s: %v", rec.Body.String(), err)
	}
	return out
}

func TestKBSearchStrictAndStatus(t *testing.T) {
	s := newKBTestServer(t)
	if _, err := s.kbClient().Add(&kb.Entry{ID: "e1", Title: "SQL注入 报错注入打法", Category: "tactic", VulnClass: "sqli", Content: "先测报错注入"}); err != nil {
		t.Fatalf("add: %v", err)
	}
	status := doGET(t, s.kbStatus, "/api/kb/status")
	if status["entries"].(float64) < 1 {
		t.Fatalf("status=%v", status)
	}
	// 空 query：严格过滤，不返回全量。
	empty := doGET(t, s.kbSearch, "/api/kb/search")
	if empty["total"].(float64) != 0 {
		t.Fatalf("empty query must not dump the library: %v", empty)
	}
	// 完全无关的查询：宁可空结果。
	none := doGET(t, s.kbSearch, "/api/kb/search?q=zzzz-no-such-token")
	if none["total"].(float64) != 0 {
		t.Fatalf("irrelevant query must return empty: %v", none)
	}
	// 命中查询。
	hit := doGET(t, s.kbSearch, "/api/kb/search?q="+"SQL")
	if hit["total"].(float64) < 1 {
		t.Fatalf("expected hit: %v", hit)
	}
}

func TestKBEntryAndInsights(t *testing.T) {
	s := newKBTestServer(t)
	if _, err := s.kbClient().Add(&kb.Entry{ID: "e2", Title: "SSTI 打法", Category: "tactic", VulnClass: "ssti", Content: "模板注入验证"}); err != nil {
		t.Fatalf("add: %v", err)
	}
	entry := doGET(t, s.kbEntry, "/api/kb/entry?id=e2")
	if entry["id"] != "e2" {
		t.Fatalf("entry=%v", entry)
	}
	missing := doGET(t, s.kbEntry, "/api/kb/entry?id=nope")
	if _, ok := missing["error"]; !ok {
		t.Fatalf("missing entry should report error: %v", missing)
	}
	ins := doGET(t, s.kbInsights, "/api/v2/kb-insights")
	if ins["total"].(float64) < 1 {
		t.Fatalf("insights=%v", ins)
	}
	if _, ok := ins["by_category"].(map[string]any); !ok {
		t.Fatalf("insights missing by_category: %v", ins)
	}
}

func TestKBBackfill(t *testing.T) {
	s := newKBTestServer(t)
	rec := httptest.NewRecorder()
	s.kbBackfill(rec, httptest.NewRequest(http.MethodPost, "/api/kb/backfill", nil))
	if !strings.Contains(rec.Body.String(), "ok") {
		t.Fatalf("backfill body=%s", rec.Body.String())
	}
}
