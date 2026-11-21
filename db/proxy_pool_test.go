package db

import (
	"strings"
	"testing"
)

// TestProxyStoreEncryptedAndProbe 验证：入口 URL 密文落库、同 host:port 去重、探测结果回写。
func TestProxyStoreEncryptedAndProbe(t *testing.T) {
	d := openC2TestDB(t) // 复用迁移后的隔离库（0008 已含 proxy_pool）
	ctx := t.Context()
	store := NewProxyStore(d, []byte("test-master-key"))

	id, dup, err := store.Create(ctx, &ProxyEntry{Name: "lab", URL: "http://user:pass@127.0.0.1:18080"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if dup {
		t.Fatal("first insert must not be duplicate")
	}
	var enc string
	if err := d.QueryRow(`SELECT url_enc FROM proxy_pool WHERE id=$1`, id).Scan(&enc); err != nil {
		t.Fatalf("read url_enc: %v", err)
	}
	if strings.Contains(enc, "user:pass") || !strings.HasPrefix(enc, "gcm1:") {
		t.Fatalf("proxy url must be sealed, got %q", enc)
	}
	e, err := store.Get(ctx, id)
	if err != nil || e == nil {
		t.Fatalf("get: %v", err)
	}
	if e.URL != "http://user:pass@127.0.0.1:18080" || e.Host != "127.0.0.1" || e.Port != 18080 {
		t.Fatalf("decrypted entry=%+v", e)
	}
	if e.Masked() != "http://127.0.0.1:18080" {
		t.Fatalf("masked=%q", e.Masked())
	}

	// 去重：同 scheme+host+port
	id2, dup2, err := store.Create(ctx, &ProxyEntry{URL: "http://other:other@127.0.0.1:18080"})
	if err != nil || !dup2 || id2 != id {
		t.Fatalf("duplicate insert: id=%d dup=%v err=%v", id2, dup2, err)
	}

	// 探测失败回写 failed + fail_count
	if err := store.UpdateProbe(ctx, id, "", 0, "dial timeout"); err != nil {
		t.Fatalf("update probe: %v", err)
	}
	e, _ = store.Get(ctx, id)
	if e.Status != ProxyFailed || e.FailCount != 1 || e.LastError == "" {
		t.Fatalf("after failed probe: %+v", e)
	}
	// 探测成功置 active 并清零失败数
	if err := store.UpdateProbe(ctx, id, "203.0.113.9", 321, ""); err != nil {
		t.Fatalf("update probe ok: %v", err)
	}
	e, _ = store.Get(ctx, id)
	if e.Status != ProxyActive || e.ExitIP != "203.0.113.9" || e.LatencyMs != 321 || e.FailCount != 0 {
		t.Fatalf("after ok probe: %+v", e)
	}
}
