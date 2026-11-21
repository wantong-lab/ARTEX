package kb

import "testing"

func TestNormalizeVulnClass(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		// 已是标准值
		{"rce", "rce"},
		{"info-leak", "info-leak"},
		{"idor", "idor"},
		// 下划线 / 大小写变体
		{"path_traversal", "path-traversal"},
		{"sql_injection", "sqli"},
		{"SQL Injection", "sqli"},
		{"privIlege-escalation", "privesc"},
		{"auth_bypass", "auth-bypass"},
		{"authentication-bypass", "auth-bypass"},
		// 中文
		{"SQL注入", "sqli"},
		{"任意文件读取", "path-traversal"},
		{"弱口令", "weak-password"},
		{"信息泄露", "info-leak"},
		{"越权", "idor"},
		{"未授权访问", "unauthorized"},
		{"命令执行", "rce"},
		// 复合值（斜杠分隔）—— 具体类在先
		{"webshell / rce", "webshell"},
		{"unauthorized-access / info-leak", "unauthorized"},
		{"weak-password / auth-bypass", "auth-bypass"},
		{"IDOR / 越权访问（跨租户数据读取）", "idor"},
		{"存储型XSS / 未授权对象存储写入 (cloud storage write)", "xss"},
		// 长描述式（模型把标题当类别）
		{"任意文件写入 → RCE（data-URI 路径穿越 + 无后缀白名单）", "file-upload"},
		{"MySQL UDF privilege escalation to SYSTEM RCE", "privesc"},
		// 别名
		{"information-disclosure", "info-leak"},
		{"sensitive-data-exposure", "info-leak"},
		{"credential-exposure", "credential-leak"},
		{"hardcoded-credentials", "credential-leak"},
		{"nfs-misconfig", "protocol"},
		{"broken-access-control", "idor"},
		{"backdoor", "webshell"},
		{"SSTI", "rce"},
		{"business-logic", "business-logic"},
		// 空 / 无法识别 → 兜底
		{"", "info-leak"},
		{"   ", "info-leak"},
		{"完全无法识别的东西 xyzzy", "info-leak"},
	}

	for _, c := range cases {
		got := NormalizeVulnClass(c.in, "info-leak")
		if got != c.want {
			t.Errorf("NormalizeVulnClass(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestIsVulnClass(t *testing.T) {
	for _, ok := range []string{"rce", "sqli", "idor", "info-leak", "unauthorized", "xss"} {
		if !IsVulnClass(ok) {
			t.Errorf("IsVulnClass(%q) = false, want true", ok)
		}
	}
	for _, bad := range []string{"", "随便一个", "SQL Injection", "path_traversal"} {
		if IsVulnClass(bad) {
			t.Errorf("IsVulnClass(%q) = true, want false", bad)
		}
	}
}

func TestVulnClassesAllNormalized(t *testing.T) {
	// 标准清单里每个值归一后必须等于自己（防止规则表把标准值改坏）
	for _, v := range VulnClasses {
		if got := NormalizeVulnClass(v, "info-leak"); got != v {
			t.Errorf("标准类别 %q 归一后变成 %q（规则表有冲突）", v, got)
		}
	}
}
