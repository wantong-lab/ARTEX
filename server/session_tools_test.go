package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Autumn-27/artex/agent"
	"github.com/Autumn-27/artex/db"
	"github.com/Autumn-27/artex/session"
)

// fakeSessionStore 是 sessions 表的内存实现，便于不依赖 PG 测工具逻辑。
type fakeSessionStore struct {
	mu   sync.Mutex
	next int64
	rows map[int64]*db.SessionRecord
}

func newFakeSessionStore() *fakeSessionStore {
	return &fakeSessionStore{rows: map[int64]*db.SessionRecord{}}
}

func (f *fakeSessionStore) Create(_ context.Context, rec *db.SessionRecord) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.next++
	cp := *rec
	cp.ID = f.next
	if cp.LastBeat.IsZero() {
		cp.LastBeat = time.Now()
	}
	f.rows[cp.ID] = &cp
	return cp.ID, nil
}

func (f *fakeSessionStore) Get(_ context.Context, id int64) (*db.SessionRecord, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	r, ok := f.rows[id]
	if !ok {
		return nil, fmt.Errorf("session %d not found", id)
	}
	cp := *r
	return &cp, nil
}

func (f *fakeSessionStore) UpdateStatus(_ context.Context, id int64, status string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if r, ok := f.rows[id]; ok {
		r.Status = status
	}
	return nil
}

func (f *fakeSessionStore) Touch(_ context.Context, id int64) {}

type fakeLedger struct {
	mu  sync.Mutex
	ops []*db.HostOp
}

func (f *fakeLedger) AddHostOp(o *db.HostOp) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	cp := *o
	f.ops = append(f.ops, &cp)
	return nil
}

// fakeWebshell 是真 HTTP 的 phpcmd 一句话马：POST 字段值直接交给 sh 执行并回显。
func fakeWebshell(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		cmd := r.FormValue("cmd")
		out, _ := exec.Command("sh", "-c", cmd).CombinedOutput()
		_, _ = w.Write(out)
	}))
}

func TestSessionToolsExecReadWrite(t *testing.T) {
	shell := fakeWebshell(t)
	defer shell.Close()
	store := newFakeSessionStore()
	ledger := &fakeLedger{}
	hub := newSessionHub(store, ledger, nil)
	hub.dial = func(_ context.Context, rawURL, _, _, _ string) (session.Session, error) {
		return session.NewHTTPShell(rawURL, "cmd", "phpcmd")
	}
	tools := hub.tools()
	ctx := context.Background()
	regIn, _ := json.Marshal(map[string]any{"url": shell.URL, "lang": "phpcmd"})
	regRes, err := tools[0].Call(ctx, regIn, nil)
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	var regOut struct {
		SessionID int64  `json:"session_id"`
		Kind      string `json:"kind"`
	}
	if err := json.Unmarshal([]byte(regRes.Flatten()), &regOut); err != nil {
		t.Fatalf("register result: %v (%s)", err, regRes.Flatten())
	}
	if regOut.SessionID <= 0 || regOut.Kind != "http_phpcmd" {
		t.Fatalf("register out=%+v", regOut)
	}

	execIn, _ := json.Marshal(map[string]any{"session_id": regOut.SessionID, "command": "echo hello-artex"})
	execRes, err := tools[1].Call(ctx, execIn, nil)
	if err != nil {
		t.Fatalf("exec: %v", err)
	}
	if !strings.Contains(execRes.Flatten(), "hello-artex") {
		t.Fatalf("exec output=%s", execRes.Flatten())
	}

	dir := t.TempDir()
	path := dir + "/note.txt"
	writeIn, _ := json.Marshal(map[string]any{"session_id": regOut.SessionID, "path": path, "content": "session-write-ok"})
	if _, err := tools[3].Call(ctx, writeIn, nil); err != nil {
		t.Fatalf("write: %v", err)
	}
	readIn, _ := json.Marshal(map[string]any{"session_id": regOut.SessionID, "path": path})
	readRes, err := tools[2].Call(ctx, readIn, nil)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(readRes.Flatten(), "session-write-ok") {
		t.Fatalf("read output=%s", readRes.Flatten())
	}

	if len(ledger.ops) < 3 {
		t.Fatalf("host_ledger ops=%d, want >=3", len(ledger.ops))
	}
	artifact := false
	for _, op := range ledger.ops {
		if op.Artifact {
			artifact = true
		}
	}
	if !artifact {
		t.Fatal("write should be recorded as artifact")
	}
}

func TestSessionExecTruncatesOutput(t *testing.T) {
	shell := fakeWebshell(t)
	defer shell.Close()
	hub := newSessionHub(newFakeSessionStore(), &fakeLedger{}, nil)
	hub.dial = func(_ context.Context, rawURL, _, _, _ string) (session.Session, error) {
		return session.NewHTTPShell(rawURL, "cmd", "phpcmd")
	}
	tools := hub.tools()
	regIn, _ := json.Marshal(map[string]any{"url": shell.URL, "lang": "phpcmd"})
	regRes, _ := tools[0].Call(context.Background(), regIn, nil)
	var reg struct {
		SessionID int64 `json:"session_id"`
	}
	_ = json.Unmarshal([]byte(regRes.Flatten()), &reg)
	execIn, _ := json.Marshal(map[string]any{
		"session_id": reg.SessionID,
		"command":    "head -c 20000 /dev/zero | tr \"\\0\" a",
	})
	res, err := tools[1].Call(context.Background(), execIn, nil)
	if err != nil {
		t.Fatalf("exec: %v", err)
	}
	if !strings.Contains(res.Flatten(), "输出截断") {
		t.Fatalf("expected truncation marker, got %d bytes", len(res.Flatten()))
	}
}

func TestSessionRegisterBlockedByGate(t *testing.T) {
	shell := fakeWebshell(t)
	defer shell.Close()
	hub := newSessionHub(newFakeSessionStore(), &fakeLedger{}, func(context.Context, string) error {
		return fmt.Errorf("目标未通过测试范围校验：命中阻断规则")
	})
	hub.dial = func(_ context.Context, rawURL, _, _, _ string) (session.Session, error) {
		return session.NewHTTPShell(rawURL, "cmd", "phpcmd")
	}
	regIn, _ := json.Marshal(map[string]any{"url": shell.URL, "lang": "phpcmd"})
	res, err := hub.tools()[0].Call(context.Background(), regIn, nil)
	if err != nil {
		t.Fatalf("register should return tool error text, got err=%v", err)
	}
	if !strings.Contains(res.Flatten(), "未通过测试范围校验") {
		t.Fatalf("expected gate rejection, got %s", res.Flatten())
	}
}

func TestStaleSessionMarkedDead(t *testing.T) {
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "gone", http.StatusInternalServerError)
	}))
	defer bad.Close()
	store := newFakeSessionStore()
	sec, _ := json.Marshal(session.Secret{URL: bad.URL, Password: "cmd", Lang: "phpcmd"})
	id, err := store.Create(context.Background(), &db.SessionRecord{
		Kind: "http_phpcmd", URL: bad.URL, Secret: sec, Lang: "phpcmd",
		Status: db.SessionAlive, LastBeat: time.Now().Add(-time.Hour),
	})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	hub := newSessionHub(store, &fakeLedger{}, nil)
	if _, err := hub.load(context.Background(), id); err == nil {
		t.Fatal("stale dead session must fail load")
	}
	got, _ := store.Get(context.Background(), id)
	if got.Status != db.SessionDead {
		t.Fatalf("status=%s, want dead", got.Status)
	}
}

// TestSessionToolSchemaMatchesCatalog 保证运行时 handler 的参数面与目录 seed 不漂移：
// 名字、必填项、属性名必须一致（描述允许不同）。
func TestSessionToolSchemaMatchesCatalog(t *testing.T) {
	hub := newSessionHub(newFakeSessionStore(), &fakeLedger{}, nil)
	for _, tl := range hub.tools() {
		seed, ok := agent.SessionToolMeta(tl.Name())
		if !ok {
			t.Fatalf("%s not registered in catalog", tl.Name())
		}
		rt := tl.InputSchema()
		if !sameStringSet(requiredNames(rt), requiredNames(seed.Schema)) {
			t.Fatalf("%s required mismatch: runtime=%v seed=%v", tl.Name(), requiredNames(rt), requiredNames(seed.Schema))
		}
		if !sameStringSet(propertyNames(rt), propertyNames(seed.Schema)) {
			t.Fatalf("%s properties mismatch", tl.Name())
		}
	}
}

func requiredNames(schema map[string]any) []string {
	var out []string
	if req, ok := schema["required"].([]any); ok {
		for _, v := range req {
			if s, ok := v.(string); ok {
				out = append(out, s)
			}
		}
	}
	return out
}

func propertyNames(schema map[string]any) []string {
	props, _ := schema["properties"].(map[string]any)
	out := make([]string, 0, len(props))
	for k := range props {
		out = append(out, k)
	}
	return out
}

func sameStringSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	m := map[string]int{}
	for _, v := range a {
		m[v]++
	}
	for _, v := range b {
		m[v]--
	}
	for _, n := range m {
		if n != 0 {
			return false
		}
	}
	return true
}
