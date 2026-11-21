package guard

// RoE 目标提取（M2-04）：从一次工具调用里提取「要去哪」的网络目标，供范围匹配。
// 覆盖 Bash/shell 命令里的 URL、host:port、裸 IP/CIDR，以及 host 类工具的 JSON 字段。
// 提取是尽力而为的：宁可漏也不把明显不是目标的东西（文件名/扩展名）当成主机。

import (
	"encoding/json"
	"net"
	"net/url"
	"regexp"
	"strings"
)

// Target 是一个从工具调用里提取出的网络目标。
type Target struct {
	Raw  string // 原始出现形式（URL/命令片段）
	Host string // 归一化主机名或 IP（小写、去端口）
	IP   string // 字面 IP（Host 是 IP 时非空）
}

var (
	reURL     = regexp.MustCompile(`(?i)\bhttps?://[^\s\x27"<>()\[\]{}|;]+`)
	reCIDR    = regexp.MustCompile(`\b(?:\d{1,3}\.){3}\d{1,3}/\d{1,2}\b`)
	reIPv4    = regexp.MustCompile(`\b(?:\d{1,3}\.){3}\d{1,3}\b`)
	reHost    = regexp.MustCompile(`(?i)\b(?:[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]{2,24}\b`)
	reBracket = regexp.MustCompile(`\[[0-9a-fA-F:]+\](?::\d{1,5})?`)
)

// fileLikeExt 是「看起来像文件名而不是主机名」的 TLD/扩展名集合。
var fileLikeExt = map[string]bool{
	"php": true, "jsp": true, "aspx": true, "asp": true, "txt": true, "md": true,
	"json": true, "js": true, "html": true, "htm": true, "css": true, "png": true,
	"jpg": true, "jpeg": true, "gif": true, "log": true, "sh": true, "py": true,
	"go": true, "zip": true, "tar": true, "gz": true, "bak": true, "conf": true,
	"yml": true, "yaml": true, "xml": true, "pdf": true, "docx": true, "xlsx": true,
	"tmp": true, "so": true, "exe": true, "dll": true, "bin": true, "sql": true,
}

func looksLikeFileName(host string) bool {
	i := strings.LastIndexByte(host, '.')
	if i < 0 || i == len(host)-1 {
		return false
	}
	return fileLikeExt[strings.ToLower(host[i+1:])]
}

func normalizeHostPort(v string) (host, rawIP string) {
	v = strings.TrimSpace(v)
	if v == "" {
		return "", ""
	}
	if strings.HasPrefix(v, "[") {
		if h, _, err := net.SplitHostPort(v); err == nil {
			return strings.ToLower(h), h
		}
		return strings.ToLower(strings.Trim(v, "[]")), strings.Trim(v, "[]")
	}
	if h, _, err := net.SplitHostPort(v); err == nil {
		v = h
	}
	if net.ParseIP(v) != nil {
		return v, v
	}
	return strings.ToLower(strings.TrimSuffix(v, ".")), ""
}

// ExtractTargets 从工具输入里提取去重后的网络目标。tool 目前不参与判定（所有工具
// 都扫一遍），留参数是为将来按工具收敛。
func ExtractTargets(_ string, input json.RawMessage) []Target {
	text := string(input)
	seen := map[string]bool{}
	var out []Target
	add := func(raw, hostOrURL string) {
		host, ip := hostOrURL, ""
		if strings.Contains(hostOrURL, "://") {
			u, err := url.Parse(hostOrURL)
			if err != nil {
				return
			}
			raw = hostOrURL
			host = u.Hostname()
		} else {
			host, ip = normalizeHostPort(hostOrURL)
		}
		host = strings.ToLower(host)
		if host == "" || seen[host] {
			return
		}
		if ip == "" && looksLikeFileName(host) {
			return
		}
		seen[host] = true
		out = append(out, Target{Raw: raw, Host: host, IP: ip})
	}
	for _, m := range reURL.FindAllString(text, -1) {
		add(m, strings.TrimRight(m, ".,;)'\""))
	}
	for _, m := range reCIDR.FindAllString(text, -1) {
		add(m, m)
	}
	for _, m := range reBracket.FindAllString(text, -1) {
		add(m, strings.Split(m, "]")[0]+"]")
	}
	for _, m := range reIPv4.FindAllString(text, -1) {
		add(m, m)
	}
	for _, m := range reHost.FindAllString(text, -1) {
		add(m, m)
	}
	return out
}
