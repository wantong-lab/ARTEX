package session

// 真机 SMB 测试：需要一台可用的 Windows 目标与测试账号（默认跳过，CI 不执行）。
//
//	ARTEX_SMB_TEST_HOST=192.168.112.133 \
//	ARTEX_SMB_TEST_USER=artexsmb \
//	ARTEX_SMB_TEST_PASS='...' \
//	go test ./session -run TestSMBRealWindows -v -timeout 240s
//
// 该测试同时验证 wmiexec 因 RPC/DCOM 不可达时自动回退到 atexec 的真实链路，
// 以及 atexec 下单条/复合 cmd 命令、文件写入/读取/删除。

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

func smbTestEnv(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

func TestSMBRealWindows(t *testing.T) {
	host := strings.TrimSpace(os.Getenv("ARTEX_SMB_TEST_HOST"))
	if host == "" {
		t.Skip("设置 ARTEX_SMB_TEST_HOST/USER/PASS 后运行真机 SMB 测试")
	}
	user := strings.TrimSpace(os.Getenv("ARTEX_SMB_TEST_USER"))
	pass := os.Getenv("ARTEX_SMB_TEST_PASS")
	expectUser := strings.ToLower(smbTestEnv("ARTEX_SMB_TEST_EXPECT", "system"))
	expectHost := strings.ToLower(smbTestEnv("ARTEX_SMB_TEST_HOSTNAME", "desktop"))

	ctx, cancel := context.WithTimeout(context.Background(), 220*time.Second)
	defer cancel()

	sh, err := DialSMB(SMBSecret{Host: host, User: user, Pass: pass})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	out, _, err := sh.Exec(ctx, "whoami", 90*time.Second)
	if err != nil {
		t.Fatalf("exec whoami: %v", err)
	}
	t.Logf("whoami backend=%s out=%q", sh.Backend(), out)
	if !strings.Contains(strings.ToLower(out), expectUser) {
		t.Fatalf("whoami output %q missing %q", out, expectUser)
	}
	if os.Getenv("ARTEX_SMB_BACKEND") == "" && sh.Backend() != smbBackendATExec {
		t.Fatalf("expected auto fallback to atexec, got %s", sh.Backend())
	}

	// 复合命令：验证 atexec 的整串引号包装不会只执行最后一段。
	both, _, err := sh.Exec(ctx, "whoami & hostname", 90*time.Second)
	if err != nil {
		t.Fatalf("exec compound: %v", err)
	}
	t.Logf("compound out=%q", both)
	if !strings.Contains(strings.ToLower(both), expectUser) || !strings.Contains(strings.ToLower(both), expectHost) {
		t.Fatalf("compound output %q missing whoami/hostname", both)
	}

	path := "C:\\Windows\\Temp\\artex_smb_e2e.txt"
	payload := []byte("ARTEX-SMB-E2E " + time.Now().Format(time.RFC3339) + "\n")
	if err := sh.WriteFile(ctx, path, payload); err != nil {
		t.Fatalf("write file: %v", err)
	}
	got, err := sh.ReadFile(ctx, path)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if string(got) != string(payload) {
		t.Fatalf("read back mismatch: got %q want %q", got, payload)
	}
	if _, _, err := sh.Exec(ctx, "del /f \""+path+"\"", 60*time.Second); err != nil {
		t.Fatalf("cleanup: %v", err)
	}
	t.Logf("smb real e2e ok: backend=%s bytes=%d", sh.Backend(), len(payload))
}
