package server

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Autumn-27/artex/agent"
	pgdb "github.com/Autumn-27/artex/db"
)

func TestAnonymizeContent(t *testing.T) {
	in := "在 http://10.1.2.3:8080/admin 用 ssrf 打 192.168.0.9，回连 api.corp.example.com"
	out := anonymizeContent(in)
	for _, leaked := range []string{"10.1.2.3", "192.168.0.9", "api.corp.example.com"} {
		if strings.Contains(out, leaked) {
			t.Fatalf("anonymize leaked %q: %q", leaked, out)
		}
	}
	for _, want := range []string{"<url>", "<ip>", "<domain>"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing placeholder %q in %q", want, out)
		}
	}
}

func TestIntentSummary(t *testing.T) {
	b, _ := json.Marshal(map[string]any{"summary": "验证 SQL 注入"})
	if got := intentSummary(&pgdb.Node{Payload: b}); got != "验证 SQL 注入" {
		t.Fatalf("summary=%q", got)
	}
	if got := intentSummary(nil); got != "" {
		t.Fatalf("nil node summary=%q", got)
	}
	if got := intentSummary(&pgdb.Node{Payload: []byte("not-json")}); got != "" {
		t.Fatalf("bad payload summary=%q", got)
	}
}

func TestReflectIntentNoopWithoutMemory(t *testing.T) {
	m := &Manager{}
	b, _ := json.Marshal(map[string]any{"summary": "验证 SQL 注入"})
	// 未初始化内存层时不得 panic（best-effort 语义）。
	var wrote agent.WriteCounts
	wrote.Facts = 1
	m.ReflectIntent(1, &pgdb.Node{Payload: b}, "done", wrote)
}
