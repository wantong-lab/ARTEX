package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func newObsTestServer() *Server {
	return &Server{metrics: newHTTPMetrics(), startedAt: time.Now()}
}

func scrapeMetrics(t *testing.T, s *Server, remote string) (int, string) {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	req.RemoteAddr = remote
	s.metricsHandler().ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

func TestMetricsPrometheusText(t *testing.T) {
	s := newObsTestServer()
	code, body := scrapeMetrics(t, s, "127.0.0.1:5555")
	if code != 200 {
		t.Fatalf("code=%d body=%s", code, body)
	}
	for _, want := range []string{
		"# TYPE artex_up gauge", "artex_up 1",
		"artex_build_info{version=", "artex_http_in_flight_requests 0",
		"artex_go_goroutines", "artex_go_mem_alloc_bytes",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("metrics missing %q\n%s", want, body)
		}
	}
}

func TestMetricsRejectsNonLoopback(t *testing.T) {
	s := newObsTestServer()
	code, _ := scrapeMetrics(t, s, "10.1.2.3:5555")
	if code != http.StatusForbidden {
		t.Fatalf("remote scrape code=%d, want 403", code)
	}
}

func TestMetricsTokenAllowsRemote(t *testing.T) {
	t.Setenv("ARTEX_METRICS_TOKEN", "secret-metrics-token")
	s := newObsTestServer()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	req.RemoteAddr = "10.1.2.3:5555"
	req.Header.Set("X-Metrics-Token", "secret-metrics-token")
	s.metricsHandler().ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("token scrape code=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestMetricsMiddlewareCounts(t *testing.T) {
	s := newObsTestServer()
	h := s.metricsMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte("ok"))
	}))
	for i := 0; i < 2; i++ {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/x", nil))
	}
	_, body := scrapeMetrics(t, s, "127.0.0.1:5555")
	if !strings.Contains(body, "artex_http_requests_total{method=\"POST\",code=\"201\"} 2") {
		t.Fatalf("counter missing: %s", body)
	}
	if !strings.Contains(body, "artex_http_request_duration_seconds_count{method=\"POST\"} 2") {
		t.Fatalf("duration count missing: %s", body)
	}
}

func TestObsLineIsJSON(t *testing.T) {
	line := obsLine("info", "http_request", map[string]any{"method": "GET", "status": 200})
	var rec map[string]any
	if err := json.Unmarshal([]byte(line), &rec); err != nil {
		t.Fatalf("not json: %v %s", err, line)
	}
	if rec["level"] != "info" || rec["msg"] != "http_request" || rec["method"] != "GET" {
		t.Fatalf("unexpected record: %v", rec)
	}
	if _, ok := rec["ts"]; !ok {
		t.Fatalf("missing ts: %v", rec)
	}
}

func TestObservabilityStatus(t *testing.T) {
	s := newObsTestServer()
	rec := httptest.NewRecorder()
	s.observabilityStatus(rec, httptest.NewRequest(http.MethodGet, "/api/observability/status", nil))
	if rec.Code != 200 {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
	}
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("bad json: %v", err)
	}
	for _, k := range []string{"version", "uptime_secs", "log_format", "http"} {
		if _, ok := out[k]; !ok {
			t.Fatalf("status missing %q: %v", k, out)
		}
	}
}
