package db

import (
	"sync"
	"testing"
)

// TestHostLedgerHashChain 验证台账哈希链：正常链路 ok；篡改历史行后校验精确定位。
func TestHostLedgerHashChain(t *testing.T) {
	d := openC2TestDB(t)
	for i := 0; i < 3; i++ {
		err := d.AddHostOp(&HostOp{
			SessionID: 1, Host: "10.0.0.1", Channel: "ssh", Cmd: "id", Stdout: "uid=0", OK: true, Ms: 5,
			Artifact: i == 0, AtType: "file", Cleanup: "rm -f /tmp/x", Note: "audit-chain",
		})
		if err != nil {
			t.Fatalf("add op %d: %v", i, err)
		}
	}
	res, err := d.VerifyHostLedger()
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if !res.OK || res.Total < 3 || res.Hashed < 3 {
		t.Fatalf("verify result=%+v", res)
	}
	var firstID int64
	if err := d.QueryRow("SELECT id FROM host_ledger ORDER BY id LIMIT 1").Scan(&firstID); err != nil {
		t.Fatalf("first id: %v", err)
	}
	if _, err := d.Exec("UPDATE host_ledger SET cmd='tampered' WHERE id=$1", firstID); err != nil {
		t.Fatalf("tamper: %v", err)
	}
	res, err = d.VerifyHostLedger()
	if err != nil {
		t.Fatalf("verify tampered: %v", err)
	}
	if res.OK || res.BrokenAt != firstID {
		t.Fatalf("tamper must be detected at %d, got %+v", firstID, res)
	}
}

// TestHostLedgerHashChainConcurrent 验证并发写入下链仍然完整（写路径串行锁表）。
func TestHostLedgerHashChainConcurrent(t *testing.T) {
	d := openC2TestDB(t)
	var wg sync.WaitGroup
	errs := make(chan error, 12)
	for w := 0; w < 4; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < 3; i++ {
				if err := d.AddHostOp(&HostOp{SessionID: int64(w + 1), Host: "127.0.0.1", Channel: "session", Cmd: "echo", Stdout: "ok", OK: true, Note: "concurrent"}); err != nil {
					errs <- err
				}
			}
		}(w)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent add: %v", err)
	}
	res, err := d.VerifyHostLedger()
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if !res.OK || res.Total != 12 || res.Hashed != 12 {
		t.Fatalf("concurrent verify=%+v", res)
	}
}
