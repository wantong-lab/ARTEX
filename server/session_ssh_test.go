package server

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"fmt"
	"net"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Autumn-27/artex/session"
	"golang.org/x/crypto/ssh"
)

// startTestSSHServer 起一个真实 SSH 协议服务（密码认证 + exec 通道），用于真机验证
// 平台侧 SSH 会话驱动，不依赖外部 sshd。
func startTestSSHServer(t *testing.T, user, pass string) (string, int) {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatalf("signer: %v", err)
	}
	cfg := &ssh.ServerConfig{PasswordCallback: func(c ssh.ConnMetadata, password []byte) (*ssh.Permissions, error) {
		if c.User() == user && string(password) == pass {
			return nil, nil
		}
		return nil, fmt.Errorf("密码错误")
	}}
	cfg.AddHostKey(signer)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			nc, err := ln.Accept()
			if err != nil {
				return
			}
			go serveTestSSHConn(nc, cfg)
		}
	}()
	host, portStr, _ := net.SplitHostPort(ln.Addr().String())
	port, _ := strconv.Atoi(portStr)
	return host, port
}

func serveTestSSHConn(nc net.Conn, cfg *ssh.ServerConfig) {
	sc, chans, reqs, err := ssh.NewServerConn(nc, cfg)
	if err != nil {
		_ = nc.Close()
		return
	}
	defer sc.Close()
	go ssh.DiscardRequests(reqs)
	for nch := range chans {
		if nch.ChannelType() != "session" {
			_ = nch.Reject(ssh.UnknownChannelType, "only session")
			continue
		}
		ch, requests, err := nch.Accept()
		if err != nil {
			continue
		}
		go func() {
			defer ch.Close()
			for req := range requests {
				if req.Type != "exec" {
					_ = req.Reply(false, nil)
					continue
				}
				cmd := string(req.Payload[4:])
				_ = req.Reply(true, nil)
				out, runErr := exec.Command("sh", "-c", cmd).CombinedOutput()
				_, _ = ch.Write(out)
				status := uint32(0)
				if runErr != nil {
					status = 1
				}
				_, _ = ch.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{status}))
				return
			}
		}()
	}
}

func TestSSHSessionRegisterExecReadWrite(t *testing.T) {
	host, port := startTestSSHServer(t, "root", "ssh-pass-1")
	store := newFakeSessionStore()
	ledger := &fakeLedger{}
	hub := newSessionHub(store, ledger, nil)
	ctx := context.Background()
	res, err := hub.registerSSHCore(ctx, session.SSHSecret{Host: host, Port: port, User: "root", Password: "ssh-pass-1"}, 0)
	if err != nil {
		t.Fatalf("register ssh: %v", err)
	}
	sid, _ := res["session_id"].(int64)
	if sid <= 0 || res["kind"] != "ssh" {
		t.Fatalf("register result=%v", res)
	}
	out, err := hub.execCore(ctx, sid, "echo ssh-e2e-ok", 10*time.Second)
	if err != nil {
		t.Fatalf("ssh exec: %v", err)
	}
	if !strings.Contains(fmt.Sprint(out["stdout"]), "ssh-e2e-ok") {
		t.Fatalf("exec output=%v", out)
	}
	// 强制走「从库恢复」路径（模拟进程重启）。
	restored := newSessionHub(store, ledger, nil)
	sh, err := restored.load(ctx, sid)
	if err != nil {
		t.Fatalf("restore ssh: %v", err)
	}
	path := t.TempDir() + "/ssh-file.txt"
	if err := sh.WriteFile(ctx, path, []byte("ssh-write-ok")); err != nil {
		t.Fatalf("write: %v", err)
	}
	data, err := sh.ReadFile(ctx, path)
	if err != nil || string(data) != "ssh-write-ok" {
		t.Fatalf("read=%q err=%v", string(data), err)
	}
	found := false
	for _, op := range ledger.ops {
		if op.Channel == "session" && strings.Contains(op.Cmd, "ssh-e2e-ok") {
			found = true
		}
	}
	if !found {
		t.Fatal("ssh exec should write host_ledger")
	}
}

func TestSSHSessionRejectsBadPasswordAndGate(t *testing.T) {
	host, port := startTestSSHServer(t, "root", "right-pass")
	hub := newSessionHub(newFakeSessionStore(), &fakeLedger{}, nil)
	if _, err := hub.registerSSHCore(context.Background(), session.SSHSecret{Host: host, Port: port, User: "root", Password: "wrong-pass"}, 0); err == nil {
		t.Fatal("bad password must fail")
	}
	gated := newSessionHub(newFakeSessionStore(), &fakeLedger{}, func(context.Context, string) error {
		return fmt.Errorf("目标未通过测试范围校验：命中阻断规则")
	})
	if _, err := gated.registerSSHCore(context.Background(), session.SSHSecret{Host: host, Port: port, User: "root", Password: "right-pass"}, 0); err == nil || !strings.Contains(err.Error(), "阻断规则") {
		t.Fatalf("gate should block, err=%v", err)
	}
}

func TestSMBSessionRegisterAndLoad(t *testing.T) {
	hub := newSessionHub(newFakeSessionStore(), &fakeLedger{}, nil)
	ctx := context.Background()
	res, err := hub.registerSMBCore(ctx, session.SMBSecret{Host: "10.0.0.9", User: "CORP\\admin", Hash: "0123456789abcdef0123456789abcdef"}, 0)
	if err != nil {
		t.Fatalf("register smb: %v", err)
	}
	sid, _ := res["session_id"].(int64)
	if sid <= 0 || res["kind"] != "smbexec" || res["hash_used"] != true {
		t.Fatalf("register result=%v", res)
	}
	restored := newSessionHub(hub.store, &fakeLedger{}, nil)
	sh, err := restored.load(ctx, sid)
	if err != nil {
		t.Fatalf("restore smb: %v", err)
	}
	if sh.Kind() != "smbexec" {
		t.Fatalf("kind=%s", sh.Kind())
	}
	// 本机没有 wmiexec.py：必须返回明确错误而不是 panic/伪造成功。
	if _, _, err := sh.Exec(ctx, "whoami", 3*time.Second); err == nil {
		t.Fatal("smb exec without wmiexec should return explicit error")
	}
	if _, err := restored.registerSMBCore(ctx, session.SMBSecret{Host: "h", User: "u"}, 0); err == nil {
		t.Fatal("smb without password/hash must fail")
	}
}
