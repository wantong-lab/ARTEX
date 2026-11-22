package tunnel

// 真机 Windows 跨 OS 隧道 e2e（默认 skip）：平台侧 chisel server 用平台二进制，
// 目标侧上传 Windows chisel.exe，经 SMB 会话交付/启动，最后从平台 SOCKS5 访问
// 本地 HTTP 服务验证隧道双向承载。
//
//	ARTEX_CHISEL_PATH=/tmp/artex-chisel/darwin-arm64/chisel \
//	ARTEX_CHISEL_CLIENT_DIR=/tmp/artex-chisel/clients \
//	ARTEX_WIN_SMB_HOST=192.168.112.133 ARTEX_WIN_SMB_USER=artexsmb ARTEX_WIN_SMB_PASS=... \
//	ARTEX_PG_DSN=postgres://artex:artex@127.0.0.1:55432/artex_tunnel?sslmode=disable \
//	go test ./tunnel -run TestChiselWindowsRealE2E -v -timeout 240s

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Autumn-27/artex/db"
	"github.com/Autumn-27/artex/session"
	"github.com/Autumn-27/artex/stage"
)

func TestChiselWindowsRealE2E(t *testing.T) {
	host := strings.TrimSpace(os.Getenv("ARTEX_WIN_SMB_HOST"))
	user := strings.TrimSpace(os.Getenv("ARTEX_WIN_SMB_USER"))
	pass := os.Getenv("ARTEX_WIN_SMB_PASS")
	chisel := strings.TrimSpace(os.Getenv("ARTEX_CHISEL_PATH"))
	clientDir := strings.TrimSpace(os.Getenv("ARTEX_CHISEL_CLIENT_DIR"))
	dsn := strings.TrimSpace(os.Getenv("ARTEX_PG_DSN"))
	callback := strings.TrimSpace(os.Getenv("ARTEX_TUNNEL_CALLBACK_HOST"))
	if host == "" || user == "" || pass == "" || chisel == "" || clientDir == "" || dsn == "" {
		t.Skip("需要 ARTEX_WIN_SMB_* / ARTEX_CHISEL_PATH / ARTEX_CHISEL_CLIENT_DIR / ARTEX_PG_DSN")
	}
	if callback == "" {
		callback = "192.168.112.55"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 220*time.Second)
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
	sess, err := session.DialSMB(session.SMBSecret{Host: host, User: user, Pass: pass})
	if err != nil {
		t.Fatalf("dial smb: %v", err)
	}
	defer sess.Close(context.Background())
	sess.SetID(1)
	reg := session.NewRegistry()
	reg.Add(sess)
	store := db.NewTunnelStore(d)
	mgr, err := NewManager(Options{
		Store: store, Stage: stageMgr, Sessions: reg,
		ChiselPath: chisel, ChiselClientDir: clientDir, DataDir: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("manager: %v", err)
	}
	plan, err := PlanSocks(&PlanOpts{
		TaskID: 0, ViaSessionID: 1, ListenHost: "0.0.0.0", CallbackHost: callback,
		PortMin: 26200, PortMax: 26600, CanListen: CanListen, DataDir: mgr.PlanDataDir(),
	})
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	rec, err := mgr.Deploy(ctx, plan)
	if err != nil {
		slog, _ := os.ReadFile(plan.ServerLog)
		clog := ""
		if b, rerr := sess.ReadFile(ctx, plan.RemoteLog); rerr == nil {
			clog = string(b)
		}
		t.Fatalf("deploy: %v\nserver_log=%s\nclient_log=%s", err, clip(string(slog), 1500), clip(clog, 1500))
	}
	t.Logf("windows tunnel alive id=%d state=%s target=%s/%s bin=%s remote_pid=%d", rec.ID, rec.State, plan.TargetOS, plan.TargetArch, plan.ClientBinary, plan.RemotePID)
	defer func() {
		mgr.killRemote(context.Background(), sess, plan)
		_, _, _ = sess.Exec(context.Background(), windowsRemoveDirCommand(plan.RemoteDir), 10*time.Second)
		mgr.killServer(rec.ID, plan)
		_ = os.Remove(plan.AuthFile)
		_ = store.Delete(context.Background(), rec.ID)
	}()
	if rec.State != db.TunnelAlive {
		t.Fatalf("tunnel state=%s", rec.State)
	}
	if plan.TargetOS != "windows" || !strings.Contains(plan.ClientBinary, "windows") {
		t.Fatalf("target selection wrong: os=%s bin=%s", plan.TargetOS, plan.ClientBinary)
	}

	// 目标 HTTP 服务必须监听 0.0.0.0：SOCKS 入口在 Windows 目标侧，
	// 拨测从 Windows 发起，不能访问平台 Mac 的 127.0.0.1。
	ln, err := net.Listen("tcp", "0.0.0.0:0")
	if err != nil {
		t.Fatalf("target listen: %v", err)
	}
	target := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("windows-tunnel-ok"))
	}))
	target.Listener = ln
	target.Start()
	defer target.Close()
	_, tport, err := net.SplitHostPort(ln.Addr().String())
	if err != nil {
		t.Fatalf("target addr: %v", err)
	}
	uHost := net.JoinHostPort(callback, tport)
	conn, err := dialSocksData(mgr.EntryAddr(plan), uHost, 10*time.Second)
	if err != nil {
		t.Fatalf("socks dial: %v", err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(8 * time.Second))
	req := "GET / HTTP/1.1\r\nHost: " + uHost + "\r\nConnection: close\r\n\r\n"
	if _, err := conn.Write([]byte(req)); err != nil {
		t.Fatalf("write: %v", err)
	}
	resp, err := io.ReadAll(conn)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(string(resp), "windows-tunnel-ok") {
		t.Fatalf("tunnel response missing marker: %s", clip(string(resp), 400))
	}
	t.Logf("windows chisel tunnel verified: socks=%s -> %s", mgr.EntryAddr(plan), uHost)
}
