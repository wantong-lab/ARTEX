package server

// 真实 VPS 上的 SSH 会话验证（本地开发/验收用，CI 无凭据自动 skip）。
// 环境变量：ARTEX_VPS_HOST、ARTEX_VPS_KEY_FILE（PEM 私钥路径）、ARTEX_VPS_USER（默认 root）。

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Autumn-27/artex/session"
)

func TestSSHRealVPSExecReadWrite(t *testing.T) {
	host := strings.TrimSpace(os.Getenv("ARTEX_VPS_HOST"))
	keyFile := strings.TrimSpace(os.Getenv("ARTEX_VPS_KEY_FILE"))
	if host == "" || keyFile == "" {
		t.Skip("需要 ARTEX_VPS_HOST 与 ARTEX_VPS_KEY_FILE")
	}
	user := strings.TrimSpace(os.Getenv("ARTEX_VPS_USER"))
	if user == "" {
		user = "root"
	}
	key, err := os.ReadFile(keyFile)
	if err != nil {
		t.Fatalf("read key: %v", err)
	}
	hub := newSessionHub(newFakeSessionStore(), &fakeLedger{}, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	res, err := hub.registerSSHCore(ctx, session.SSHSecret{Host: host, Port: 22, User: user, Key: string(key)}, 0)
	if err != nil {
		t.Fatalf("register vps ssh: %v", err)
	}
	sid, _ := res["session_id"].(int64)
	if sid <= 0 {
		t.Fatalf("session_id=%v", res["session_id"])
	}
	out, err := hub.execCore(ctx, sid, "hostname && uname -m", 20*time.Second)
	if err != nil {
		t.Fatalf("vps exec: %v", err)
	}
	stdout := strings.TrimSpace(strings.TrimSpace(asString(out["stdout"])))
	if stdout == "" || !strings.Contains(stdout, "x86_64") {
		t.Fatalf("unexpected uname output: %q", stdout)
	}
	remoteFile := "/tmp/artex-vps-verify-" + time.Now().Format("150405")
	sh, err := hub.load(ctx, sid)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if err := sh.WriteFile(ctx, remoteFile, []byte("vps-ssh-write-ok")); err != nil {
		t.Fatalf("vps write: %v", err)
	}
	data, err := sh.ReadFile(ctx, remoteFile)
	if err != nil || string(data) != "vps-ssh-write-ok" {
		t.Fatalf("vps read=%q err=%v", string(data), err)
	}
	if _, err := hub.execCore(ctx, sid, "rm -f -- "+remoteFile, 10*time.Second); err != nil {
		t.Fatalf("cleanup: %v", err)
	}
	t.Logf("真实 VPS SSH 验证通过：host=%s user=%s sid=%d uname=%s", host, user, sid, mirror(stdout))
}

func asString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func mirror(s string) string {
	s = strings.ReplaceAll(s, "\n", " | ")
	if len(s) > 120 {
		return s[:120]
	}
	return s
}
