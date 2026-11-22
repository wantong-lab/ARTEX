package tunnel

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

type fakePlatformSession struct {
	kind     string
	uname    string
	windows  string
	unameErr error
}

func (f *fakePlatformSession) ID() int64                  { return 1 }
func (f *fakePlatformSession) Kind() string               { return f.kind }
func (f *fakePlatformSession) Test(context.Context) error { return nil }
func (f *fakePlatformSession) Exec(_ context.Context, cmd string, _ time.Duration) (string, string, error) {
	switch {
	case strings.Contains(cmd, "uname"):
		if f.unameErr != nil {
			return "", "", f.unameErr
		}
		return f.uname, "", nil
	case strings.Contains(cmd, "%OS%"):
		return f.windows, "", nil
	}
	return "", "", errors.New("unsupported command")
}
func (f *fakePlatformSession) ReadFile(context.Context, string) ([]byte, error) { return nil, nil }
func (f *fakePlatformSession) WriteFile(context.Context, string, []byte) error  { return nil }
func (f *fakePlatformSession) Close(context.Context) error                      { return nil }

func TestNormalizePlatformNames(t *testing.T) {
	cases := []struct{ in, want string }{
		{"Linux", "linux"}, {"darwin", "darwin"}, {"Windows_NT", "windows"}, {"MINGW64_NT-10.0", "windows"},
	}
	for _, c := range cases {
		if got := normalizeOSName(c.in); got != c.want {
			t.Fatalf("normalizeOSName(%q)=%q want %q", c.in, got, c.want)
		}
	}
	archs := []struct{ in, want string }{
		{"x86_64", "amd64"}, {"AMD64", "amd64"}, {"aarch64", "arm64"}, {"arm64", "arm64"}, {"i386", "386"},
	}
	for _, c := range archs {
		if got := normalizeArchName(c.in); got != c.want {
			t.Fatalf("normalizeArchName(%q)=%q want %q", c.in, got, c.want)
		}
	}
}

func TestDetectTargetPlatform(t *testing.T) {
	linux := &fakePlatformSession{kind: "ssh", uname: "Linux\nx86_64\n"}
	if osName, arch := detectTargetPlatform(context.Background(), linux); osName != "linux" || arch != "amd64" {
		t.Fatalf("linux detect=%q/%q", osName, arch)
	}
	win := &fakePlatformSession{kind: "ssh", unameErr: errors.New("not found"), windows: "Windows_NT\nAMD64\n"}
	if osName, arch := detectTargetPlatform(context.Background(), win); osName != "windows" || arch != "amd64" {
		t.Fatalf("windows detect=%q/%q", osName, arch)
	}
	smb := &fakePlatformSession{kind: "smbexec", unameErr: errors.New("x"), windows: ""}
	if osName, arch := detectTargetPlatform(context.Background(), smb); osName != "windows" || arch != "amd64" {
		t.Fatalf("smb fallback=%q/%q", osName, arch)
	}
}

func TestClientBinaryFor(t *testing.T) {
	dir := t.TempDir()
	winDir := filepath.Join(dir, "windows-amd64")
	if err := os.MkdirAll(winDir, 0o755); err != nil {
		t.Fatal(err)
	}
	winBin := filepath.Join(winDir, "chisel.exe")
	if err := os.WriteFile(winBin, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	m := &Manager{chiselPath: "/platform/chisel", chiselClientDir: dir}
	got, err := m.clientBinaryFor(&Plan{TargetOS: "windows", TargetArch: "amd64"})
	if err != nil || got != winBin {
		t.Fatalf("windows client=%q err=%v", got, err)
	}
	if _, err := m.clientBinaryFor(&Plan{TargetOS: "linux", TargetArch: "amd64"}); err == nil {
		t.Fatalf("linux should be missing when platform is %s/%s", runtime.GOOS, runtime.GOARCH)
	}
	same, err := m.clientBinaryFor(&Plan{TargetOS: runtime.GOOS, TargetArch: runtime.GOARCH})
	if err != nil || same != "/platform/chisel" {
		t.Fatalf("same-platform fallback=%q err=%v", same, err)
	}
}

func TestAdaptWindowsPlan(t *testing.T) {
	p := &Plan{RemoteDir: "/tmp/.artex-abc123", RemoteBin: "/tmp/.artex-abc123/ch", RemoteLog: "/tmp/.artex-abc123/run.log"}
	adaptWindowsPlan(p)
	if !strings.HasPrefix(p.RemoteDir, "C:\\Windows\\Temp\\") || !strings.HasSuffix(p.RemoteBin, ".exe") {
		t.Fatalf("adapted paths dir=%q bin=%q", p.RemoteDir, p.RemoteBin)
	}
	if p.RemoteLog != p.RemoteDir+"\\run.log" {
		t.Fatalf("adapted log=%q", p.RemoteLog)
	}
}
func TestWindowsDeployCommands(t *testing.T) {
	p := &Plan{
		RemoteDir: "C:\\Windows\\Temp\\.artex-test", RemoteBin: "C:\\Windows\\Temp\\.artex-test\\ch.exe",
		RemoteLog: "C:\\Windows\\Temp\\.artex-test\\run.log", AuthUser: "artex", AuthPass: "deadbeef",
		PlatformAddr: "10.0.0.5:26200",
		ClientArgs:   []string{"client", "--keepalive", "10s", "http://10.0.0.5:26200", "R:127.0.0.1:26201:socks"},
	}
	spawn := windowsSpawnCommand(p)
	for _, want := range []string{"[char]34", "Win32_Process", "--auth", "ARTEX_PID="} {
		if !strings.Contains(spawn, want) {
			t.Fatalf("spawn command missing %q: %s", want, spawn)
		}
	}
	mkdir := windowsMkdirCommand(p.RemoteDir)
	if strings.ContainsAny(mkdir, "|>") || !strings.Contains(mkdir, "ARTEX_MKDIR_OK") {
		t.Fatalf("mkdir command has unsafe redirect/pipe or missing marker: %s", mkdir)
	}
	dl := windowsDownloadCommand("http://10.0.0.5:1234/token/ch.exe", p.RemoteBin)
	if strings.ContainsAny(dl, "|>") || !strings.Contains(dl, "Invoke-WebRequest") || !strings.Contains(dl, "ARTEX_DL_OK") {
		t.Fatalf("download command unsafe/incomplete: %s", dl)
	}
	pre := windowsPrecheckCommand("10.0.0.5", 445)
	if !strings.Contains(pre, "ARTEX_PRE_OK") || strings.ContainsAny(pre, "|>") {
		t.Fatalf("precheck command: %s", pre)
	}
}
