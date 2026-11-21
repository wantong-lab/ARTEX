package db

import (
	"fmt"
	"testing"
	"time"
)

func TestSetHoneypotScore(t *testing.T) {
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
	assets := d.Assets()
	url := fmt.Sprintf("https://honeypot-%d.example.test/", time.Now().UnixNano())
	id, err := assets.UpsertHTTPService(UpsertHTTPServiceReq{URL: url})
	if err != nil {
		t.Fatalf("upsert service: %v", err)
	}
	if err := assets.SetHoneypotScore(id, 0.9, []string{"cowrie:banner"}); err != nil {
		t.Fatalf("set score: %v", err)
	}
	score, evidence, err := assets.GetHoneypotScore(id)
	if err != nil {
		t.Fatalf("get score: %v", err)
	}
	if score != 0.9 || len(evidence) != 1 || evidence[0] != "cowrie:banner" {
		t.Fatalf("score=%v evidence=%v", score, evidence)
	}
	// 更低的分数不得覆盖已有高分与证据
	if err := assets.SetHoneypotScore(id, 0.3, []string{"lower"}); err != nil {
		t.Fatalf("set lower score: %v", err)
	}
	score, evidence, err = assets.GetHoneypotScore(id)
	if err != nil {
		t.Fatalf("get score after lower: %v", err)
	}
	if score != 0.9 || len(evidence) != 1 || evidence[0] != "cowrie:banner" {
		t.Fatalf("lower score must not overwrite: score=%v evidence=%v", score, evidence)
	}
}
