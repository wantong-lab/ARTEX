package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Autumn-27/artex/db"
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

type nilSessionStore struct{}

func (nilSessionStore) Create(context.Context, *db.SessionRecord) (int64, error) { return 0, nil }
func (nilSessionStore) Get(context.Context, int64) (*db.SessionRecord, error)    { return nil, nil }
func (nilSessionStore) UpdateStatus(context.Context, int64, string) error        { return nil }
func (nilSessionStore) Touch(context.Context, int64)                             {}
func (nilSessionStore) List(context.Context, string) ([]*db.SessionRecord, error) {
	return nil, nil
}
func (nilSessionStore) Delete(context.Context, int64) error { return nil }

// 真实 PG store 对不存在的 session 返回 (nil,nil)；这里锁定执行/清理不得 panic。
func TestSessionLoadMissingSessionNoPanic(t *testing.T) {
	h := newSessionHub(nilSessionStore{}, nil, nil)
	if _, err := h.load(context.Background(), 424242); err == nil {
		t.Fatal("missing session must return error, not nil deref")
	}
}

func TestSessionCleanupMissingSessionNoPanic(t *testing.T) {
	h := newSessionHub(nilSessionStore{}, nil, nil)
	if _, err := h.cleanupShell(context.Background(), 424242); err == nil {
		t.Fatal("cleanup missing session must return error, not nil deref")
	}
}
