package tunnel

// 真机隧道 e2e：真实 chisel 二进制 + 真实隧道 Manager + 本机回环假立足点会话。
// 目标机语义用「本机 sh 会话」模拟（交付/拉起/回连全部真实执行），
// 需要 ARTEX_CHISEL_PATH 与 ARTEX_PG_DSN，CI 无二进制时自动 skip。

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Autumn-27/artex/db"
	"github.com/Autumn-27/artex/session"
	"github.com/Autumn-27/artex/stage"
)

type loopbackSession struct {
	id int64
}

func (s *loopbackSession) ID() int64    { return s.id }
func (s *loopbackSession) Kind() string { return "http_phpcmd" }
func (s *loopbackSession) Test(ctx context.Context) error {
	_, _, err := s.Exec(ctx, "echo __loopback_test__", 3*time.Second)
	return err
}
func (s *loopbackSession) Exec(ctx context.Context, cmd string, timeout time.Duration) (string, string, error) {
	if strings.Contains(cmd, "setsid ") {
		cmd = strings.Replace(cmd, "setsid ", "", 1)
	}
	// 用真实文件承接 stdout/stderr：目标侧后台进程（chisel client）可能继承写端，
	// CombinedOutput 的管道拷贝会让 Wait 一直等不到 EOF（实测挂死）。
	f, ferr := os.CreateTemp("", "loopback-exec-*")
	if ferr != nil {
		return "", "", ferr
	}
	name := f.Name()
	defer os.Remove(name)
	c := exec.CommandContext(ctx, "sh", "-c", cmd)
	c.Stdout = f
	c.Stderr = f
	err := c.Run()
	_ = f.Close()
	out, _ := os.ReadFile(name)
	return string(out), "", err
}
func (s *loopbackSession) ReadFile(_ context.Context, path string) ([]byte, error) {
	return os.ReadFile(path)
}
func (s *loopbackSession) WriteFile(_ context.Context, path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o700)
}
func (s *loopbackSession) Close(context.Context) error { return nil }

func TestChiselRealLoopbackE2E(t *testing.T) {
	chisel := strings.TrimSpace(os.Getenv("ARTEX_CHISEL_PATH"))
	dsn := strings.TrimSpace(os.Getenv("ARTEX_PG_DSN"))
	if chisel == "" || dsn == "" {
		t.Skip("真机隧道 e2e 需要 ARTEX_CHISEL_PATH 与 ARTEX_PG_DSN")
	}
	ctx := context.Background()
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
	store := db.NewTunnelStore(d)
	sessions := session.NewRegistry()
	sess := &loopbackSession{id: 1}
	sessions.Add(sess)
	mgr, err := NewManager(Options{
		Store: store, Stage: stageMgr, Sessions: sessions,
		ChiselPath: chisel, DataDir: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("manager: %v", err)
	}
	plan, err := PlanSocks(&PlanOpts{
		TaskID: 0, ViaSessionID: 1, ListenHost: "127.0.0.1", CallbackHost: "127.0.0.1",
		PortMin: 26100, PortMax: 26600, DataDir: mgr.PlanDataDir(),
	})
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	dctx, dcancel := context.WithTimeout(ctx, 80*time.Second)
	defer dcancel()
	rec, err := mgr.Deploy(dctx, plan)
	if err != nil {
		serverLog, _ := os.ReadFile(plan.ServerLog)
		clientLog, _ := os.ReadFile(plan.RemoteLog)
		t.Fatalf("deploy: %v\nserver_log=%s\nclient_log=%s", err, clip(string(serverLog), 1500), clip(string(clientLog), 1500))
	}
	defer func() {
		if plan.RemotePID > 0 {
			_, _, _ = sess.Exec(ctx, "kill "+strconv.Itoa(plan.RemotePID)+" 2>/dev/null; rm -rf "+plan.RemoteDir, 5*time.Second)
		}
		mgr.killServer(rec.ID, plan)
		_ = os.Remove(plan.AuthFile)
	}()
	if rec.State != db.TunnelAlive {
		t.Fatalf("tunnel state=%s", rec.State)
	}
	// 经真实 socks5 隧道访问本机 HTTP 服务，证明双向承载。
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("chisel-real-ok"))
	}))
	defer target.Close()
	u, err := url.Parse(target.URL)
	if err != nil {
		t.Fatalf("target url: %v", err)
	}
	conn, err := dialSocksData(mgr.EntryAddr(plan), u.Host, 8*time.Second)
	if err != nil {
		t.Fatalf("socks dial %s: %v", mgr.EntryAddr(plan), err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	req := "GET / HTTP/1.1\r\nHost: " + u.Host + "\r\nConnection: close\r\n\r\n"
	if _, err := conn.Write([]byte(req)); err != nil {
		t.Fatalf("write: %v", err)
	}
	resp, err := io.ReadAll(conn)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(string(resp), "chisel-real-ok") {
		t.Fatalf("tunnel response missing marker: %q", clip(string(resp), 300))
	}
	got, err := store.Get(ctx, rec.ID)
	if err != nil || got == nil || got.State != db.TunnelAlive {
		t.Fatalf("record state=%v err=%v", got, err)
	}
	t.Logf("真实 chisel 隧道验证通过：server=%d client=%d socks=%s -> %s", plan.ServerPID, plan.RemotePID, mgr.EntryAddr(plan), u.Host)
}

func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// dialSocksData 经平台回环 socks5 入口建立数据连接（与 verify 的 socks5Dial 同一协议，
// 这里需要拿到 conn 做真实 HTTP 往返）。
func dialSocksData(entry, target string, timeout time.Duration) (net.Conn, error) {
	host, portStr, err := net.SplitHostPort(target)
	if err != nil {
		return nil, err
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return nil, err
	}
	conn, err := net.DialTimeout("tcp", entry, timeout)
	if err != nil {
		return nil, err
	}
	_ = conn.SetDeadline(time.Now().Add(timeout))
	if _, err := conn.Write(BuildSocks5Greeting()); err != nil {
		_ = conn.Close()
		return nil, err
	}
	head := make([]byte, 2)
	if _, err := io.ReadFull(conn, head); err != nil {
		_ = conn.Close()
		return nil, err
	}
	if head[0] != 0x05 || head[1] != 0x00 {
		_ = conn.Close()
		return nil, fmt.Errorf("socks 握手被拒: %v", head)
	}
	req, err := BuildSocks5Request(host, port)
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	if _, err := conn.Write(req); err != nil {
		_ = conn.Close()
		return nil, err
	}
	reply := make([]byte, 4)
	if _, err := io.ReadFull(conn, reply); err != nil {
		_ = conn.Close()
		return nil, err
	}
	if ok, code := ParseSocks5Reply(reply[:2]); !ok {
		_ = conn.Close()
		return nil, fmt.Errorf("socks CONNECT 失败 code=%d", code)
	}
	var skip int
	switch reply[3] {
	case 0x01:
		skip = 4 + 2
	case 0x04:
		skip = 16 + 2
	case 0x03:
		lb := make([]byte, 1)
		if _, err := io.ReadFull(conn, lb); err != nil {
			_ = conn.Close()
			return nil, err
		}
		skip = int(lb[0]) + 2
	default:
		_ = conn.Close()
		return nil, fmt.Errorf("socks 未知地址类型 %d", reply[3])
	}
	if skip > 0 {
		if _, err := io.ReadFull(conn, make([]byte, skip)); err != nil {
			_ = conn.Close()
			return nil, err
		}
	}
	_ = conn.SetDeadline(time.Now().Add(timeout))
	return conn, nil
}
