package db

import (
	"context"
	"testing"
	"time"
)

// TestCredentialDecryptDegradesPerRow 验证「单行解密失败降级不拖垮列表」：
// 用密钥 A 写入后，用密钥 B 的 store 读取，行仍可见但 SecretOK=false。
func TestCredentialDecryptDegradesPerRow(t *testing.T) {
	sqlDB := openMigrationTestDB(t)
	ctx := t.Context()
	conn, err := sqlDB.Conn(ctx)
	if err != nil {
		t.Fatalf("conn: %v", err)
	}
	defer conn.Close()
	if _, err := applyMigrations(ctx, conn, func(time.Duration) {}); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
	d := &DB{sqlDB}
	task, err := d.CreateTask("cred degrade test", "x", nil, 0, 0)
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	writer := NewCredentialStore(d, []byte("key-A-0123456789abcdef0123456789"))
	id, err := writer.Create(ctx, &CredentialRecord{TaskID: task.ID, Username: "root", CredType: "password", Secret: "s3cret"})
	if err != nil {
		t.Fatalf("create credential: %v", err)
	}
	// 正确密钥：可读
	got, err := writer.Get(ctx, id)
	if err != nil || got == nil || !got.SecretOK || got.Secret != "s3cret" {
		t.Fatalf("correct key read: %+v err=%v", got, err)
	}
	// 错误密钥：整表可列出，单行降级
	reader := NewCredentialStore(d, []byte("key-B-0123456789abcdef0123456789"))
	rows, err := reader.ListByTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("list with wrong key must not fail: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("rows=%d, want 1", len(rows))
	}
	if rows[0].SecretOK {
		t.Fatal("wrong key must mark SecretOK=false")
	}
	if rows[0].SecretError == "" {
		t.Fatal("SecretError should explain the failure")
	}
	if rows[0].Username != "root" {
		t.Fatal("metadata must stay readable when secret cannot be decrypted")
	}

	_ = context.Background()
}
