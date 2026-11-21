package db

import (
	"strings"
	"testing"
	"time"
)

func openC2TestDB(t *testing.T) *DB {
	t.Helper()
	raw := openMigrationTestDB(t)
	ctx := t.Context()
	conn, err := raw.Conn(ctx)
	if err != nil {
		t.Fatalf("conn: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	if _, err := applyMigrations(ctx, conn, func(time.Duration) {}); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
	return &DB{raw}
}

// TestC2StoreTokenSealedAndTaskLifecycle 验证：token 密文落库、可解回；任务状态机可推进。
func TestC2StoreTokenSealedAndTaskLifecycle(t *testing.T) {
	d := openC2TestDB(t)
	ctx := t.Context()
	store := NewC2Store(d, []byte("test-master-key"))

	id, err := store.CreateListener(ctx, &C2Listener{Name: "lab", BindHost: "127.0.0.1", BindPort: 44444, Token: "secret-token-1"})
	if err != nil {
		t.Fatalf("create listener: %v", err)
	}
	var enc string
	if err := d.QueryRow(`SELECT token_enc FROM c2_listeners WHERE id=$1`, id).Scan(&enc); err != nil {
		t.Fatalf("read token_enc: %v", err)
	}
	if strings.Contains(enc, "secret-token-1") {
		t.Fatal("token must not be stored in plaintext")
	}
	if !strings.HasPrefix(enc, "gcm1:") {
		t.Fatalf("token_enc format=%q", enc)
	}
	l, err := store.GetListener(ctx, id)
	if err != nil || l == nil {
		t.Fatalf("get listener: %v", err)
	}
	if l.Token != "secret-token-1" {
		t.Fatalf("decrypted token=%q", l.Token)
	}
	if err := store.SetListenerStatus(ctx, id, C2ListenerRunning, ""); err != nil {
		t.Fatalf("set status: %v", err)
	}

	sessID, err := store.UpsertSession(ctx, &C2Session{ListenerID: id, ImplantUUID: "u-1", RemoteAddr: "127.0.0.1:5555", OS: "linux", Arch: "amd64", Username: "root", Status: C2SessionActive})
	if err != nil {
		t.Fatalf("upsert session: %v", err)
	}
	if _, err := store.UpsertSession(ctx, &C2Session{ListenerID: id, ImplantUUID: "u-1", Status: C2SessionActive}); err != nil {
		t.Fatalf("upsert again: %v", err)
	}
	sessions, err := store.ListSessions(ctx, true)
	if err != nil || len(sessions) != 1 {
		t.Fatalf("sessions=%d err=%v", len(sessions), err)
	}

	taskID, err := store.CreateTask(ctx, sessID, "exec", "id", "ai")
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	task, err := store.GetTask(ctx, taskID)
	if err != nil || task == nil || task.Status != C2TaskQueued {
		t.Fatalf("task=%+v err=%v", task, err)
	}
	if err := store.FinishTask(ctx, taskID, C2TaskSuccess, "uid=0(root)", "", 12); err != nil {
		t.Fatalf("finish task: %v", err)
	}
	task, _ = store.GetTask(ctx, taskID)
	if task.Status != C2TaskSuccess || task.Output != "uid=0(root)" || task.DurationMs != 12 {
		t.Fatalf("finished task=%+v", task)
	}
	tasks, err := store.ListTasks(ctx, sessID, 10)
	if err != nil || len(tasks) != 1 {
		t.Fatalf("list tasks=%d err=%v", len(tasks), err)
	}
}

// TestC2StoreEmptyKeyFailsClosed 验证：没有主密钥时加密失败（不允许明文降级）。
func TestC2StoreEmptyKeyFailsClosed(t *testing.T) {
	d := openC2TestDB(t)
	store := NewC2Store(d, nil)
	if _, err := store.CreateListener(t.Context(), &C2Listener{BindHost: "127.0.0.1", BindPort: 44444, Token: "t"}); err == nil {
		t.Fatal("empty master key must fail closed")
	}
}
