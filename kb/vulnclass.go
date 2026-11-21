package kb

// 漏洞类别的规范集合与归一化。
//
// 背景（2026-11-17 实测）：kb_curator 的 LLM 会把 category 当标题写，产出
// 「任意文件写入 → RCE（data-URI 路径穿越 + 无后缀白名单）」「存储型XSS / 未授权
// 对象存储写入 (cloud storage write)」这类值——全库攒出 299 种非标准类别、890 条。
// 后果是检索按类别过滤时全部漏掉，统计也被拆成碎片。
//
// 这里提供两件事：
//  1. VulnClasses：标准类别清单（供 kb_add 的 schema enum 约束使用，模型乱填会被拒）；
//  2. NormalizeVulnClass：把任意输入映射回标准类别的兜底（中文/复合值/下划线/别名都覆盖），
//     入库前调用一次，保证写进库的永远是规范值。

// VulnClasses 是入库允许的 category 全集。
// 前 19 个是原 0.4.2 设计就有的，后 7 个是实测存在且高频、原清单漏掉的。
var VulnClasses = []string{
	// —— 原设计 ——
	"rce", "sqli", "file-upload", "auth-bypass", "weak-password", "ssrf", "xxe",
	"path-traversal", "info-leak", "privesc", "tunnel", "recon", "waf", "protocol",
	"deserialization", "frontend-reverse", "app-audit", "env", "methodology",
	"tactic", "negative", "ammo",
	// —— 实测补充 ——
	"unauthorized", "idor", "xss", "webshell", "cloud", "credential-leak", "business-logic",
}

// vulnClassSet 用于 O(1) 判定。
var vulnClassSet = func() map[string]bool {
	m := make(map[string]bool, len(VulnClasses))
	for _, v := range VulnClasses {
		m[v] = true
	}
	return m
}()

// vulnClassRules 是「关键词 → 标准类别」的归一规则。
// 顺序敏感：具体类别在前，宽泛类别在后，避免 "webshell / rce" 被 rce 抢走。
var vulnClassRules = []struct {
	keywords []string
	target   string
}{
	// 越权 / 访问控制
	{[]string{"idor", "越权", "对象级授权", "broken-access", "broken_access",
		"access-control", "access_control", "批量赋值", "mass assignment"}, "idor"},
	// 认证绕过
	{[]string{"auth-bypass", "auth_bypass", "authentication-bypass", "authentication_bypass",
		"broken-authentication", "broken_authentication", "认证绕过", "身份认证绕过",
		"login-bypass", "missing-authentication"}, "auth-bypass"},
	// XSS：须先于 unauthorized 匹配。
	// 例：「存储型XSS / 未授权对象存储写入」主体是 XSS，不该被判成 unauthorized。
	{[]string{"xss", "跨站"}, "xss"},
	// 提权：须先于 rce 匹配。
	// 例：「privilege escalation to SYSTEM RCE」主体是提权，不该被判成 rce。
	{[]string{"privilege-escalation", "privilege_escalation", "privilege escalation",
		"privilege elevation", "提权", "container-escape", "persistence", "持久化"}, "privesc"},
	// 未授权
	{[]string{"unauthorized", "unauthenticated", "missing-authorization", "未授权"}, "unauthorized"},
	// 凭据 / 硬编码 / AK
	{[]string{"credential", "hardcoded", "硬编码", "password-leak", "ak泄露", "ak-leak",
		"key-compromise", "cryptographic-key", "凭据", "account-takeover", "账户接管",
		"账号接管", "后台接管"}, "credential-leak"},
	// 弱口令
	{[]string{"weak-password", "weak_password", "weakpass", "弱口令", "默认口令", "默认密码"}, "weak-password"},
	// 源码/配置/敏感信息泄露
	{[]string{"source-code", "source_code", "源码泄露", "源码暴露", "config-disclosure",
		"配置泄露", "information-disclosure", "information_disclosure", "info-disclosure",
		"sensitive-data", "sensitive_data", "sensitive-information", "sensitive_info",
		"sensitive-file", "exposed-service", "信息泄露", "敏感信息", "敏感数据", "资料泄露"},
		"info-leak"},
	// SQL 注入
	{[]string{"sqli", "sql-injection", "sql_injection", "sql injection", "sql注入"}, "sqli"},
	// SSRF / XXE
	{[]string{"ssrf"}, "ssrf"},
	{[]string{"xxe", "xml external"}, "xxe"},
	// SSTI 归 rce
	{[]string{"ssti", "template-injection", "模板注入"}, "rce"},
	// 文件上传 / 任意文件写
	{[]string{"file-upload", "file_upload", "unrestricted-upload", "文件上传", "任意上传",
		"arbitrary-file-write", "arbitrary_file_write", "任意文件写入"}, "file-upload"},
	// 路径穿越 / 任意文件读
	{[]string{"path-traversal", "path_traversal", "traversal", "路径穿越", "目录穿越",
		"arbitrary-file-read", "arbitrary_file_read", "file-read", "任意文件读取", "文件读取"},
		"path-traversal"},
	// WebShell / 后门（须先于 rce 匹配）
	{[]string{"webshell", "web-shell", "backdoor", "malicious-script", "后门"}, "webshell"},
	// RCE / 命令执行
	{[]string{"rce", "remote-code", "remote_code", "command-exec", "command-execution",
		"command injection", "命令执行", "代码执行", "命令注入", "code-exec"}, "rce"},
	// 云
	{[]string{"cloud", "云", "aliyun", "aws", "azure", "oss", "s3-bucket", "bucket",
		"对象存储"}, "cloud"},
	// 业务逻辑
	{[]string{"business-logic", "business_logic", "逻辑漏洞", "业务逻辑",
		"payment-callback", "price tampering", "signature-bypass"}, "business-logic"},
	// 反序列化
	{[]string{"deserial", "反序列化"}, "deserialization"},
	// 服务/协议配置错误
	{[]string{"nfs", "misconfig", "misconfiguration", "配置不当"}, "protocol"},
	// 隧道
	{[]string{"tunnel", "隧道", "pivot", "代理"}, "tunnel"},
	// 侦察 / SEO 劫持
	{[]string{"recon", "侦察", "测绘", "fingerprint", "指纹", "seo", "cloaking", "parasite"}, "recon"},
	// WAF
	{[]string{"waf", "绕过防护"}, "waf"},
	// 协议
	{[]string{"protocol", "协议", "smb", "ldap", "kerberos", "redis", "mysql", "mssql",
		"ftp", "rdp", "ssh", "jdbc", "jmx"}, "protocol"},
	// 方法论 / 打法 / 死路 / 弹药
	{[]string{"methodology", "方法论"}, "methodology"},
	{[]string{"tactic", "打法", "playbook"}, "tactic"},
	{[]string{"negative", "避坑", "死路", "死胡同"}, "negative"},
	{[]string{"ammo", "弹药"}, "ammo"},
	{[]string{"app-audit", "代码审计", "源码审计"}, "app-audit"},
	{[]string{"frontend-reverse", "前端逆向"}, "frontend-reverse"},
	{[]string{"env", "环境"}, "env"},
}

// NormalizeVulnClass 把任意输入映射成标准类别。无法识别时回落到 fallback
// （调用方传各自的兜底，入库默认 "info-leak"，检索侧可以不兜底直接返回空）。
func NormalizeVulnClass(raw, fallback string) string {
	s := trimSpaceLower(raw)
	if s == "" {
		return fallback
	}
	if vulnClassSet[s] {
		return s
	}
	for _, r := range vulnClassRules {
		for _, kw := range r.keywords {
			if containsLower(s, kw) {
				return r.target
			}
		}
	}
	return fallback
}

// IsVulnClass 判断是否已是标准类别。
func IsVulnClass(s string) bool { return vulnClassSet[trimSpaceLower(s)] }
