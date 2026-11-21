package server

import (
	"context"
	"testing"

	"github.com/Autumn-27/artex/agent"
	"github.com/Autumn-27/artex/db"
)

// fakeFindingRecorder 模拟底层漏洞存储，返回固定 finding id。
type fakeFindingRecorder struct {
	id int64
}

func (f *fakeFindingRecorder) Record(context.Context, db.RecordFindingInput, []db.TrafficRef) (*db.RecordedFinding, error) {
	return &db.RecordedFinding{FindingID: f.id, NodeID: f.id + 1}, nil
}

var _ agent.FindingRecorder = (*fakeFindingRecorder)(nil)

func TestCuratingRecorderIdempotentAndAnonymized(t *testing.T) {
	s := &Server{m: &Manager{dir: t.TempDir()}}
	c := s.kbClient()
	if c == nil {
		t.Fatal("kb client")
	}
	t.Cleanup(func() { _ = c.Close() })
	rec := &curatingRecorder{srv: s, inner: &fakeFindingRecorder{id: 77}}
	in := db.RecordFindingInput{
		VulnClass: "sqli", Name: "SQL注入 报错注入", Severity: "high",
		Summary:  "在 10.9.9.9 的登录接口确认报错注入",
		Evidence: "sqlmap 输出 10.9.9.9 back-end DBMS",
	}
	for i := 0; i < 2; i++ {
		out, err := rec.Record(context.Background(), in, nil)
		if err != nil || out == nil || out.FindingID != 77 {
			t.Fatalf("record #%d: %v %v", i, out, err)
		}
	}
	res, err := c.Search("", 100)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if res.Total != 1 {
		t.Fatalf("curation must be idempotent, total=%d", res.Total)
	}
	if res.Entries[0].ID != "finding-77" {
		t.Fatalf("entry id=%q", res.Entries[0].ID)
	}
	if res.Entries[0].Category != "tactic" {
		t.Fatalf("category=%q", res.Entries[0].Category)
	}
	if want := "<ip>"; !containsStr(res.Entries[0].Content, want) {
		t.Fatalf("content should be anonymized, got %q", res.Entries[0].Content)
	}
	if containsStr(res.Entries[0].Content, "10.9.9.9") {
		t.Fatal("raw IP leaked into kb entry")
	}
}

func containsStr(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
