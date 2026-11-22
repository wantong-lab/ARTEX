package tunnel

// 公网 VPS 跨主机隧道真机 E2E（默认 skip）：平台 macOS 起 chisel server（0.0.0.0:39444，
// 公网映射 39.89.211.55:39444 -> 本机:39444），经 SSH 会话在 Linux VPS 上传/拉起
// linux-amd64 client，平台 SOCKS5 经 VPS 访问 VPS 本地 HTTP 服务，并尝试验证公网出口 IP。
//
//	ARTEX_VPS_SSH_HOST=113.20.3.124 ARTEX_VPS_SSH_USER=root ARTEX_VPS_SSH_KEY_PATH=~/.ssh/id_ed25519_113.20.3.124 \
//	ARTEX_CHISEL_PATH=/tmp/artex-chisel/darwin-arm64/chisel \
//	ARTEX_CHISEL_CLIENT_DIR=/tmp/artex-chisel/clients \
//	ARTEX_TUNNEL_CALLBACK_HOST=39.89.211.55 \
//	ARTEX_PG_DSN=... go test ./tunnel -run TestChiselVPSRealE2E -v -timeout 300s

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Autumn-27/artex/db"
	"github.com/Autumn-27/artex/session"
	"github.com/Autumn-27/artex/stage"
)

func TestChiselVPSRealE2E(t *testing.T) {
	host := strings.TrimSpace(os.Getenv("ARTEX_VPS_SSH_HOST"))
	user := strings.TrimSpace(os.Getenv("ARTEX_VPS_SSH_USER"))
	keyPath := strings.TrimSpace(os.Getenv("ARTEX_VPS_SSH_KEY_PATH"))
	chisel := strings.TrimSpace(os.Getenv("ARTEX_CHISEL_PATH"))
	clientDir := strings.TrimSpace(os.Getenv("ARTEX_CHISEL_CLIENT_DIR"))
	callback := strings.TrimSpace(os.Getenv("ARTEX_TUNNEL_CALLBACK_HOST"))
	dsn := strings.TrimSpace(os.Getenv("ARTEX_PG_DSN"))
	if host == "" || user == "" || keyPath == "" || chisel == "" || clientDir == "" || callback == "" || dsn == "" {
		t.Skip("需要 ARTEX_VPS_SSH_* / ARTEX_CHISEL_PATH / ARTEX_CHISEL_CLIENT_DIR / ARTEX_TUNNEL_CALLBACK_HOST / ARTEX_PG_DSN")
	}
	key, err := os.ReadFile(keyPath)
	if err != nil {
		t.Fatalf("read ssh key: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 280*time.Second)
	defer cancel()

	d, err := db.Open(dsn)
	if err != nil {
		t.Fatalf("db open: %v", err)
	}
	defer d.Close()
	stageMgr, err := stage.New(ctx, filepath.Join(t.TempDir(), "stage"))
	if err != nil {
		t.Fatalf("stage: %v", err)
	}
	defer stageMgr.Close()
	sess, err := session.DialSSH(session.SSHSecret{Host: host, Port: 22, User: user, Key: string(key)}, 15*time.Second)
	if err != nil {
		t.Fatalf("dial ssh: %v", err)
	}
	defer sess.Close(context.Background())
	sess.SetID(1)
	reg := session.NewRegistry()
	reg.Add(sess)
	store := db.NewTunnelStore(d)
	mgr, err := NewManager(Options{
		Store: store, Stage: stageMgr, Sessions: reg,
		ChiselPath: chisel, ChiselClientDir: clientDir, StagePort: 39443, DataDir: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("manager: %v", err)
	}
	plan, err := PlanSocks(&PlanOpts{
		TaskID: 0, ViaSessionID: 1, ListenHost: "0.0.0.0", CallbackHost: callback,
		PortMin: 39444, PortMax: 39445, CanListen: CanListen, DataDir: mgr.PlanDataDir(),
	})
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	rec, err := mgr.Deploy(ctx, plan)
	if err != nil {
		slog, _ := os.ReadFile(plan.ServerLog)
		t.Fatalf("deploy: %v\nserver_log=%s", err, clipTunnelText(string(slog), 1500))
	}
	t.Logf("vps tunnel alive id=%d state=%s listen=%d socks=%d target=%s/%s bin=%s pid=%d", rec.ID, rec.State, plan.ListenPort, plan.SocksPort, plan.TargetOS, plan.TargetArch, plan.ClientBinary, plan.RemotePID)
	httpPort := 18081
	defer func() {
		mgr.killRemote(context.Background(), sess, plan)
		_, _, _ = sess.Exec(context.Background(), "pkill -f 'http.server "+strconv.Itoa(httpPort)+"' 2>/dev/null; rm -rf "+plan.RemoteDir, 10*time.Second)
		mgr.killServer(rec.ID, plan)
		_ = os.Remove(plan.AuthFile)
		_ = store.Delete(context.Background(), rec.ID)
	}()
	if rec.State != db.TunnelAlive {
		t.Fatalf("state=%s", rec.State)
	}
	if plan.TargetOS != "linux" || !strings.Contains(plan.ClientBinary, "linux") {
		t.Fatalf("target selection: os=%s bin=%s", plan.TargetOS, plan.ClientBinary)
	}
	// VPS 本地 HTTP 服务（只监听回环；从平台经 SOCKS 访问，证明流量确实落在 VPS 上）
	if _, _, err := sess.Exec(ctx, fmt.Sprintf("nohup python3 -m http.server %d --bind 127.0.0.1 >/tmp/artex-vps-http.log 2>&1 &", httpPort), 10*time.Second); err != nil {
		t.Fatalf("start vps http: %v", err)
	}
	time.Sleep(1 * time.Second)
	conn, err := dialSocksData(mgr.EntryAddr(plan), "127.0.0.1:"+strconv.Itoa(httpPort), 12*time.Second)
	if err != nil {
		t.Fatalf("socks dial vps local: %v", err)
	}
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
	if _, err := io.WriteString(conn, "GET / HTTP/1.1\r\nHost: 127.0.0.1\r\nConnection: close\r\n\r\n"); err != nil {
		t.Fatalf("write: %v", err)
	}
	resp, err := io.ReadAll(conn)
	conn.Close()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(string(resp), "Directory listing") && !strings.Contains(string(resp), "200 OK") {
		t.Fatalf("vps local http response unexpected: %s", clipTunnelText(string(resp), 300))
	}
	t.Logf("vps local HTTP via tunnel OK: %s -> 127.0.0.1:%d", mgr.EntryAddr(plan), httpPort)

	// 公网出口 IP：经 VPS SOCKS 访问 ifconfig.me，期望返回 VPS 公网 IP（非平台出口）。
	conn2, err := dialSocksData(mgr.EntryAddr(plan), "ifconfig.me:80", 8*time.Second)
	if err == nil {
		_ = conn2.SetDeadline(time.Now().Add(8 * time.Second))
		_, _ = io.WriteString(conn2, "GET /ip HTTP/1.1\r\nHost: ifconfig.me\r\nConnection: close\r\n\r\n")
		body, rerr := io.ReadAll(conn2)
		conn2.Close()
		if rerr == nil {
			txt := string(body)
			if i := strings.LastIndex(txt, "\r\n\r\n"); i >= 0 {
				txt = txt[i+4:]
			}
			t.Logf("vps egress ip (via tunnel) = %s", strings.TrimSpace(clipTunnelText(txt, 100)))
		} else {
			t.Logf("vps egress ip check failed: %v", rerr)
		}
	} else {
		t.Logf("vps egress ip check dial failed: %v", err)
	}
}
