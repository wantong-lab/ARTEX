package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Autumn-27/artex/kb"
)

func newCVETestServer(t *testing.T) *Server {
	t.Helper()
	s := &Server{m: &Manager{dir: t.TempDir()}}
	dir := filepath.Join(s.m.dir, "cve")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "cve.db"), nil, 0o600); err != nil {
		t.Fatalf("cve.db: %v", err)
	}
	if s.cveClient() == nil {
		t.Fatal("cve client should open with fixture")
	}
	return s
}

func TestCVEStatusAndSearch(t *testing.T) {
	s := newCVETestServer(t)
	status := doGET(t, s.cveStatus, "/api/cve/status")
	if status["ok"] != true {
		t.Fatalf("status=%v", status)
	}
	if status["count"].(float64) < 2 {
		t.Fatalf("fixture should seed baseline CVEs: %v", status)
	}
	empty := doGET(t, s.cveSearch, "/api/cve/search")
	if empty["total"].(float64) != 0 {
		t.Fatalf("empty query must not dump corpus: %v", empty)
	}
	hit := doGET(t, s.cveSearch, "/api/cve/search?q=Log4j")
	if hit["total"].(float64) < 1 {
		t.Fatalf("expected Log4j hit: %v", hit)
	}
}

func TestCVESploitusHandler(t *testing.T) {
	s := newCVETestServer(t)
	old := sploitusSearch
	t.Cleanup(func() { sploitusSearch = old })
	sploitusSearch = func(context.Context, string, string, int) ([]kb.SploitusItem, int, error) {
		return []kb.SploitusItem{{ID: "1", Title: "PoC", Type: "exploit"}}, 1, nil
	}
	rec := httptest.NewRecorder()
	s.cveSploitus(rec, httptest.NewRequest(http.MethodGet, "/api/cve/sploitus?q=log4j", nil))
	if !strings.Contains(rec.Body.String(), "PoC") {
		t.Fatalf("body=%s", rec.Body.String())
	}
}

func TestCVESyncStartRunsToCompletion(t *testing.T) {
	s := newCVETestServer(t)
	feed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"totalResults": 1,
			"vulnerabilities": []map[string]any{{"cve": map[string]any{
				"id": "CVE-2024-9999", "published": "2024-05-01T00:00:00.000",
				"descriptions": []map[string]any{{"lang": "en", "value": "sync fixture"}},
			}}},
		})
	}))
	defer feed.Close()
	rec := httptest.NewRecorder()
	s.cveSyncStart(rec, httptest.NewRequest(http.MethodPost, "/api/cve/sync?feed_url="+feed.URL, nil))
	if rec.Code != http.StatusAccepted {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		if time.Now().After(deadline) {
			t.Fatal("sync did not finish in time")
		}
		s.cveMu.Lock()
		running := s.cveRunning
		s.cveMu.Unlock()
		if !running {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	st := kb.LoadCVESyncState(filepath.Join(s.m.dir, "cve"))
	if !st.Done || st.Processed < 1 {
		t.Fatalf("sync state=%+v", st)
	}
}

func TestCVEAutosyncRequiresSettingsStore(t *testing.T) {
	s := newCVETestServer(t)
	rec := httptest.NewRecorder()
	s.cveAutosync(rec, httptest.NewRequest(http.MethodPost, "/api/cve/autosync", strings.NewReader(`{"enabled":true}`)))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
	}
}
