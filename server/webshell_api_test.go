package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Autumn-27/artex/db"
	"github.com/Autumn-27/artex/session"
)

// fakeSessionStore 的列表/删除能力（WebShell 管理 API 需要）。
func (f *fakeSessionStore) List(_ context.Context, status string) ([]*db.SessionRecord, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []*db.SessionRecord
	for _, r := range f.rows {
		if status == "" || r.Status == status {
			cp := *r
			out = append(out, &cp)
		}
	}
	return out, nil
}

func (f *fakeSessionStore) Delete(_ context.Context, id int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.rows[id]; !ok {
		return fmt.Errorf("session %d not found", id)
	}
	delete(f.rows, id)
	return nil
}

// newWebshellTestServer 组装：假 PHP 一句话马（真 HTTP）+ 内存会话台账 + 内存 host_ledger。
func newWebshellTestServer(t *testing.T) (*Server, *fakeSessionStore, *fakeLedger, string) {
	t.Helper()
	shell := fakeWebshell(t)
	t.Cleanup(shell.Close)
	store := newFakeSessionStore()
	ledger := &fakeLedger{}
	h := newSessionHub(store, ledger, nil)
	h.dial = func(_ context.Context, rawURL, _, _, _ string) (session.Session, error) {
		return session.NewHTTPShell(rawURL, "cmd", "phpcmd")
	}
	s := &Server{m: &Manager{dir: t.TempDir()}}
	s.sessHub = h
	return s, store, ledger, shell.URL
}

func postJSON(t *testing.T, h http.HandlerFunc, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	raw, _ := json.Marshal(body)
	rec := httptest.NewRecorder()
	h(rec, httptest.NewRequest(http.MethodPost, path, strings.NewReader(string(raw))))
	return rec
}

// TestWebshellChainRegisterExecReadCleanup 覆盖验收链路：登记→执行→读取→清理（清理走 host_ledger）。
func TestWebshellChainRegisterExecReadCleanup(t *testing.T) {
	s, _, ledger, url := newWebshellTestServer(t)

	// 1) 登记
	reg := postJSON(t, s.webshellRegister, "/api/webshell/register", map[string]any{"url": url, "lang": "phpcmd"})
	if reg.Code != http.StatusOK {
		t.Fatalf("register code=%d body=%s", reg.Code, reg.Body.String())
	}
	var regOut struct {
		SessionID int64  `json:"session_id"`
		Kind      string `json:"kind"`
	}
	if err := json.Unmarshal(reg.Body.Bytes(), &regOut); err != nil || regOut.SessionID <= 0 {
		t.Fatalf("register out=%s err=%v", reg.Body.String(), err)
	}

	// 2) 执行
	exec := postJSON(t, s.webshellExec, "/api/webshell/exec", map[string]any{"session_id": regOut.SessionID, "command": "echo chain-ok"})
	if exec.Code != http.StatusOK {
		t.Fatalf("exec code=%d body=%s", exec.Code, exec.Body.String())
	}
	var execOut struct {
		Stdout string `json:"stdout"`
	}
	_ = json.Unmarshal(exec.Body.Bytes(), &execOut)
	if !strings.Contains(execOut.Stdout, "chain-ok") {
		t.Fatalf("exec stdout=%q", execOut.Stdout)
	}

	// 3) 读取（假马在本机执行，读本机临时文件）
	dir := t.TempDir()
	path := filepath.Join(dir, "evidence.txt")
	if err := os.WriteFile(path, []byte("file-content-42"), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	read := postJSON(t, s.webshellRead, "/api/webshell/read", map[string]any{"session_id": regOut.SessionID, "path": path})
	if read.Code != http.StatusOK {
		t.Fatalf("read code=%d body=%s", read.Code, read.Body.String())
	}
	if !strings.Contains(read.Body.String(), "file-content-42") {
		t.Fatalf("read body=%s", read.Body.String())
	}

	// 4) 列表可见
	list := httptest.NewRecorder()
	s.webshellList(list, httptest.NewRequest(http.MethodGet, "/api/webshell/shells", nil))
	if !strings.Contains(list.Body.String(), `"id":`) {
		t.Fatalf("shells list=%s", list.Body.String())
	}
	if strings.Contains(list.Body.String(), "secret") {
		t.Fatal("shell list must not expose secrets")
	}

	// 5) 清理：写 host_ledger + 删除会话
	cleanupRec := httptest.NewRecorder()
	cleanupReq := httptest.NewRequest(http.MethodDelete, fmt.Sprintf("/api/webshell/shells/%d", regOut.SessionID), nil)
	cleanupReq.SetPathValue("id", fmt.Sprintf("%d", regOut.SessionID))
	s.webshellCleanup(cleanupRec, cleanupReq)
	if cleanupRec.Code != http.StatusOK {
		t.Fatalf("cleanup code=%d body=%s", cleanupRec.Code, cleanupRec.Body.String())
	}
	if !strings.Contains(cleanupRec.Body.String(), "WebShell") {
		t.Fatalf("cleanup body=%s", cleanupRec.Body.String())
	}
	if len(ledger.ops) == 0 {
		t.Fatal("cleanup must record a host_ledger entry")
	}
	last := ledger.ops[len(ledger.ops)-1]
	if !strings.Contains(last.Cleanup, "WebShell") || last.Channel != "webshell" || !last.Artifact {
		t.Fatalf("ledger entry=%+v", last)
	}

	// 清理后再执行必须失败（会话已删）
	exec2 := postJSON(t, s.webshellExec, "/api/webshell/exec", map[string]any{"session_id": regOut.SessionID, "command": "id"})
	if exec2.Code == http.StatusOK {
		t.Fatalf("exec after cleanup should fail: %s", exec2.Body.String())
	}
}

func TestWebshellGenerateAndGate(t *testing.T) {
	s, _, _, url := newWebshellTestServer(t)
	gen := postJSON(t, s.webshellGenerate, "/api/webshell/generate", map[string]any{"lang": "php"})
	if gen.Code != http.StatusOK {
		t.Fatalf("generate code=%d body=%s", gen.Code, gen.Body.String())
	}
	var g map[string]any
	_ = json.Unmarshal(gen.Body.Bytes(), &g)
	if g["key_b64"] == nil || g["content"] == nil {
		t.Fatalf("generate out=%v", g)
	}
	bad := postJSON(t, s.webshellGenerate, "/api/webshell/generate", map[string]any{"lang": "ruby"})
	if bad.Code != http.StatusBadRequest {
		t.Fatalf("unsupported lang should be 400: %d", bad.Code)
	}
	// gate 拒绝：目标未通过范围校验时不得登记
	s.sessHub.gate = func(context.Context, string) error { return fmt.Errorf("目标未通过测试范围校验") }
	blocked := postJSON(t, s.webshellRegister, "/api/webshell/register", map[string]any{"url": url, "lang": "phpcmd"})
	if blocked.Code != http.StatusBadRequest {
		t.Fatalf("gated register code=%d body=%s", blocked.Code, blocked.Body.String())
	}
}
