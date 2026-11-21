package kb

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCVEOpenReadOnlyMount 验收「只读挂载可检索」：目录不可写时 WAL 打开失败，
// OpenCVE 应回退 immutable=1 并且照常检索。
func TestCVEOpenReadOnlyMount(t *testing.T) {
	dir := newCVEFixtureDir(t)
	// 先正常打开建表+种子，再关掉。
	c, err := OpenCVE(dir)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := c.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	// 清掉 WAL 旁路文件，模拟只读镜像（只带 cve.db 本体）。
	_ = os.Remove(filepath.Join(dir, "cve.db-wal"))
	_ = os.Remove(filepath.Join(dir, "cve.db-shm"))
	defer func() { _ = os.Chmod(dir, 0o755) }()
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	c2, err := OpenCVE(dir)
	if err != nil {
		t.Fatalf("open read-only mount: %v", err)
	}
	defer c2.Close()
	n, err := c2.Count()
	if err != nil || n < 1 {
		t.Fatalf("read-only count: n=%d err=%v", n, err)
	}
	recs, err := c2.Search("Log4j", 5)
	if err != nil {
		t.Fatalf("read-only search: %v", err)
	}
	found := false
	for _, r := range recs {
		if strings.Contains(r.ID, "CVE-") {
			found = true
		}
	}
	if !found {
		t.Fatalf("read-only search should hit fixture, got %d", len(recs))
	}
}
