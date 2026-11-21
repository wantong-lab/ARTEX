package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestC2DefaultDisabled：无数据库/未启用时状态为关闭，运行时门禁拒绝。
func TestC2DefaultDisabled(t *testing.T) {
	s := &Server{m: &Manager{dir: t.TempDir()}}
	rec := httptest.NewRecorder()
	s.c2Status(rec, httptest.NewRequest(http.MethodGet, "/api/c2/status", nil))
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("bad json: %s", rec.Body.String())
	}
	if body["enabled"] != false {
		t.Fatalf("c2 must default to disabled: %v", body)
	}
	if _, err := s.c2Gate(); err == nil {
		t.Fatal("c2Gate must fail when disabled")
	}
}

// TestC2EnableRequiresRuntime：没有 pg 时 enable 返回 503，不会伪造成功。
func TestC2EnableRequiresRuntime(t *testing.T) {
	s := &Server{m: &Manager{dir: t.TempDir()}}
	rec := httptest.NewRecorder()
	s.c2Enable(rec, httptest.NewRequest(http.MethodPost, "/api/c2/enable", strings.NewReader(`{"enabled":true}`)))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
	}
}
