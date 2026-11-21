package memory

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/Autumn-27/artex/db"
)

// openTestStore 连接 ARTEX_PG_DSN；未配置时跳过（CI 有 PG，纯单测环境不报错）。
func openTestStore(t *testing.T, embedURL string) *Store {
	t.Helper()
	dsn := strings.TrimSpace(os.Getenv("ARTEX_PG_DSN"))
	if dsn == "" {
		t.Skip("ARTEX_PG_DSN not set")
	}
	t.Setenv("ARTEX_EMBED_URL", embedURL)
	d, err := db.Open(dsn)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	s := New(d)
	if !s.Enabled() {
		t.Fatal("store should be enabled when DB is reachable")
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestKeywordRecallWhenEmbedderDown(t *testing.T) {
	s := openTestStore(t, "http://127.0.0.1:1")
	ctx := context.Background()
	ref := "m3-01-keyword-test"
	_, _ = s.d.Exec(`DELETE FROM mem_entries WHERE kind=$1 AND ref_id=$2`, "technique", ref)
	t.Cleanup(func() { _, _ = s.d.Exec(`DELETE FROM mem_entries WHERE kind=$1 AND ref_id=$2`, "technique", ref) })
	if err := s.AddSync(ctx, "technique", ref, 0, "SQL注入 登录绕过 打法：先测报错注入再打盲注"); err != nil {
		t.Fatalf("AddSync: %v", err)
	}
	ents, err := s.Recall(ctx, "SQL注入", "technique", 5)
	if err != nil {
		t.Fatalf("Recall: %v", err)
	}
	found := false
	for _, e := range ents {
		if e.RefID == ref {
			found = true
		}
	}
	if !found {
		t.Fatalf("keyword fallback should find the entry, got %d rows", len(ents))
	}
	if s.Count() < 1 {
		t.Fatal("Count should see the entry")
	}
}

func TestRecallBlockEmptyWhenNoMatch(t *testing.T) {
	s := openTestStore(t, "http://127.0.0.1:1")
	if block := s.RecallBlock(context.Background(), "zzz-no-such-token-xyz", 0); block != "" {
		t.Fatalf("unexpected recall block: %q", block)
	}
}

func TestVecLit(t *testing.T) {
	if got := vecLit([]float32{0.5, -1, 2}); got != "[0.5,-1,2]" {
		t.Fatalf("vecLit=%q", got)
	}
}

func TestEnvDefaults(t *testing.T) {
	t.Setenv("ARTEX_EMBED_URL", "")
	if got := envOr("ARTEX_EMBED_URL", defaultEmbedURL); got != defaultEmbedURL {
		t.Fatalf("envOr=%q", got)
	}
}
