package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// 验收测试发现：非法 JSON 曾被忽略，*bool 被部分解码成 false，
// 导致畸形请求静默关闭 C2/自动同步。这里锁定「畸形请求必须 400」。
func TestC2EnableMalformedBody(t *testing.T) {
	s := &Server{}
	rec := httptest.NewRecorder()
	s.c2Enable(rec, httptest.NewRequest(http.MethodPost, "/api/c2/enable", strings.NewReader("{\"enabled\":\"not-bool\"}")))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestCveAutosyncMalformedBody(t *testing.T) {
	s := &Server{}
	rec := httptest.NewRecorder()
	s.cveAutosync(rec, httptest.NewRequest(http.MethodPost, "/api/cve/autosync", strings.NewReader("{\"enabled\":1.5}")))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
	}
}

// 清理接口允许空 body（沿用默认值），但畸形 JSON 必须 400，避免误触发真实清理。
func TestRetentionCleanupMalformedBody(t *testing.T) {
	s := &Server{}
	rec := httptest.NewRecorder()
	s.retentionCleanup(rec, httptest.NewRequest(http.MethodPost, "/api/retention/cleanup", strings.NewReader("{\"dry_run\":")))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
	}
}
