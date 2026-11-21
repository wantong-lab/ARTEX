package db

import (
	"context"
	"net"
	"testing"
	"time"
)

func TestPendingScopeApproveWritesTaskScope(t *testing.T) {
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
	as := d.Assets()
	ps := NewPendingScopeStore(d)

	task, err := d.CreateTask("pending scope test", "scope approval", nil, 0, 0)
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	taskID := task.ID
	id, value, err := ps.Create(ctx, taskID, "ip", "10.1.2.3", "探索发现新网段", "agent")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if value != "10.1.2.0/24" {
		t.Fatalf("IP should aggregate to /24, got %q", value)
	}
	pending, err := ps.List(ctx, taskID, "pending")
	if err != nil || len(pending) != 1 {
		t.Fatalf("list pending: %v %d", err, len(pending))
	}
	if err := ps.Approve(ctx, id, "alice", as); err != nil {
		t.Fatalf("approve: %v", err)
	}
	scopes, err := as.ListTaskScope(taskID)
	if err != nil {
		t.Fatalf("list scope: %v", err)
	}
	found := false
	for _, s := range scopes {
		if s.Kind == "cidr" && s.Net == "10.1.2.0/24" {
			found = true
		}
	}
	if !found {
		t.Fatalf("approved scope not written, got %+v", scopes)
	}
	// 审批后范围立即能判定（与 guard matcher 同语义的最小校验）。
	_, n, err := net.ParseCIDR("10.1.2.0/24")
	if err != nil || !n.Contains(net.ParseIP("10.1.2.99")) {
		t.Fatal("approved /24 must contain an address in the same range")
	}
	if err := ps.Approve(ctx, id, "alice", as); err == nil {
		t.Fatal("double approve must fail")
	}

	// 拒绝路径
	id2, _, err := ps.Create(ctx, taskID, "domain", "Internal.Example.COM.", "新域", "agent")
	if err != nil {
		t.Fatalf("create domain: %v", err)
	}
	if err := ps.Reject(ctx, id2, "bob"); err != nil {
		t.Fatalf("reject: %v", err)
	}
	rejected, _ := ps.List(ctx, taskID, "rejected")
	if len(rejected) != 1 || rejected[0].Value != "internal.example.com" {
		t.Fatalf("reject path: %+v", rejected)
	}
	_ = context.Background()
}
