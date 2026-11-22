package session

import (
	"errors"
	"strings"
	"testing"
)

func TestSMBExecArgsWMIAndATExec(t *testing.T) {
	sh, err := DialSMB(SMBSecret{Host: "10.0.0.5", User: "admin", Pass: "p@ss"})
	if err != nil {
		t.Fatal(err)
	}
	bin, args := sh.execArgs(smbBackendWMI, "cmd /c whoami")
	if bin != "wmiexec.py" {
		t.Fatalf("wmiexec bin=%q", bin)
	}
	if strings.Join(args, " ") != "-dc-ip 10.0.0.5 admin:p@ss@10.0.0.5 cmd /c whoami" {
		t.Fatalf("wmiexec args=%v", args)
	}
	bin, args = sh.execArgs(smbBackendATExec, "cmd /c whoami")
	if bin != "atexec.py" {
		t.Fatalf("atexec bin=%q", bin)
	}
	if strings.Join(args, " ") != "-dc-ip 10.0.0.5 admin:p@ss@10.0.0.5 cmd /c whoami" {
		t.Fatalf("atexec args=%v", args)
	}

	hashSh, _ := DialSMB(SMBSecret{Host: "10.0.0.5", User: "Administrator", Hash: "deadbeef"})
	bin, args = hashSh.execArgs(smbBackendATExec, "cmd /c whoami")
	if bin != "atexec.py" || strings.Join(args, " ") != "-dc-ip 10.0.0.5 -hashes :deadbeef Administrator@10.0.0.5 cmd /c whoami" {
		t.Fatalf("hash args bin=%q args=%v", bin, args)
	}

	proxySh, _ := DialSMB(SMBSecret{Host: "10.0.0.5", User: "u", Pass: "p", ProxyC: true})
	bin, args = proxySh.execArgs(smbBackendWMI, "cmd /c whoami")
	if bin != "proxychains" || len(args) < 3 || args[0] != "-q" || args[1] != "wmiexec.py" {
		t.Fatalf("proxychains bin=%q args=%v", bin, args)
	}
}

func TestSMBFallbackEligible(t *testing.T) {
	cases := []struct {
		err  error
		want bool
	}{
		{errors.New("wmiexec 失败: exit status 1: [Errno 54] Connection reset by peer"), true},
		{errors.New("wmiexec 失败: dial tcp 10.0.0.5:135: i/o timeout"), true},
		{errors.New("exec: \"wmiexec.py\": executable file not found in $PATH"), true},
		{errors.New("wmiexec 失败: STATUS_LOGON_FAILURE"), false},
		{errors.New("atexec 失败: STATUS_ACCESS_DENIED"), false},
		{nil, false},
	}
	for _, c := range cases {
		if got := fallbackEligible(c.err); got != c.want {
			t.Fatalf("fallbackEligible(%v)=%v want %v", c.err, got, c.want)
		}
	}
}

func TestSMBSanitizeOutput(t *testing.T) {
	in := "banner\r\nnt authority\\system\x00\r\n"
	out := sanitizeSMBOutput(in)
	if strings.Contains(out, "\x00") || strings.Contains(out, "\r") {
		t.Fatalf("still dirty: %q", out)
	}
	if !strings.Contains(out, "nt authority\\system") {
		t.Fatalf("lost output: %q", out)
	}
}

func TestSMBBackendPinnedByEnv(t *testing.T) {
	t.Setenv("ARTEX_SMB_BACKEND", "atexec")
	sh, err := DialSMB(SMBSecret{Host: "h", User: "u", Pass: "p"})
	if err != nil {
		t.Fatal(err)
	}
	if sh.Backend() != smbBackendATExec || sh.canAutoFallback() {
		t.Fatalf("backend=%q fallback=%v", sh.Backend(), sh.canAutoFallback())
	}
	t.Setenv("ARTEX_SMB_BACKEND", "invalid")
	sh2, _ := DialSMB(SMBSecret{Host: "h", User: "u", Pass: "p"})
	if sh2.Backend() != smbBackendWMI || !sh2.canAutoFallback() {
		t.Fatalf("invalid env backend=%q fallback=%v", sh2.Backend(), sh2.canAutoFallback())
	}
}
