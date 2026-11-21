package proxypool

// mihomo / Clash 配置适配器（P1-9）：从订阅或本地 config.yaml 的 proxies 列表解析
// HTTP / SOCKS5 代理并导入代理池。ss/vmess/trojan 等加密协议需要 mihomo 内核转发，
// 当前适配器不支持，会如实进入 skipped 列表。

import (
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
)

type MihomoProxy struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Server   string `json:"server"`
	Port     int    `json:"port"`
	Username string `json:"username,omitempty"`
	Password string `json:"-"`
}

type MihomoSkip struct {
	Name   string `json:"name"`
	Type   string `json:"type"`
	Reason string `json:"reason"`
}

type MihomoImport struct {
	Proxies []MihomoProxy `json:"proxies"`
	Skipped []MihomoSkip  `json:"skipped"`
}

// URL 生成代理池可识别的 URL；不支持的类型返回错误。
func (p MihomoProxy) URL() (string, error) {
	var scheme string
	switch strings.ToLower(strings.TrimSpace(p.Type)) {
	case "http", "https":
		scheme = "http"
	case "socks5", "socks5h":
		scheme = "socks5"
	default:
		return "", fmt.Errorf("不支持的代理类型: %s", p.Type)
	}
	server := strings.TrimSpace(p.Server)
	if server == "" || p.Port <= 0 || p.Port > 65535 {
		return "", fmt.Errorf("server/port 非法")
	}
	u := &url.URL{Scheme: scheme, Host: net.JoinHostPort(server, strconv.Itoa(p.Port))}
	if p.Username != "" || p.Password != "" {
		u.User = url.UserPassword(p.Username, p.Password)
	}
	return u.String(), nil
}

func trimYAMLValue(v string) string {
	v = strings.TrimSpace(v)
	if len(v) >= 2 && ((v[0] == '"' && v[len(v)-1] == '"') || (v[0] == '\'' && v[len(v)-1] == '\'')) {
		if v[0] == '"' {
			if unq, err := strconv.Unquote(v); err == nil {
				return unq
			}
		}
		return v[1 : len(v)-1]
	}
	return v
}

func fillMihomoField(p *MihomoProxy, key, val string) {
	val = trimYAMLValue(val)
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "name":
		p.Name = val
	case "type":
		p.Type = val
	case "server":
		p.Server = val
	case "port":
		if n, err := strconv.Atoi(val); err == nil {
			p.Port = n
		}
	case "username", "user":
		p.Username = val
	case "password", "pass":
		p.Password = val
	}
}

func parseMihomoInline(p *MihomoProxy, s string) {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "{")
	s = strings.TrimSuffix(s, "}")
	for _, part := range strings.Split(s, ",") {
		k, v, ok := strings.Cut(part, ":")
		if !ok {
			continue
		}
		fillMihomoField(p, k, v)
	}
}

// ParseMihomoConfig 解析 Clash/mihomo YAML 的 proxies 段（只识别列表字段，不依赖 YAML 库）。
func ParseMihomoConfig(data string) (*MihomoImport, error) {
	imp := &MihomoImport{}
	inProxies := false
	var cur *MihomoProxy
	flush := func() {
		if cur == nil {
			return
		}
		typ := strings.ToLower(strings.TrimSpace(cur.Type))
		switch typ {
		case "http", "https", "socks5", "socks5h":
			if _, err := cur.URL(); err == nil {
				imp.Proxies = append(imp.Proxies, *cur)
			} else {
				imp.Skipped = append(imp.Skipped, MihomoSkip{Name: cur.Name, Type: cur.Type, Reason: err.Error()})
			}
		default:
			imp.Skipped = append(imp.Skipped, MihomoSkip{Name: cur.Name, Type: cur.Type,
				Reason: "需要 mihomo 内核转发（适配器仅导入 http/socks5）"})
		}
		cur = nil
	}
	for _, raw := range strings.Split(data, "\n") {
		line := strings.TrimRight(raw, "\r")
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		indent := len(line) - len(strings.TrimLeft(line, " \t"))
		if indent == 0 {
			flush()
			inProxies = trimmed == "proxies:"
			continue
		}
		if !inProxies {
			continue
		}
		if strings.HasPrefix(trimmed, "-") {
			flush()
			cur = &MihomoProxy{}
			rest := strings.TrimSpace(strings.TrimPrefix(trimmed, "-"))
			if rest != "" {
				parseMihomoInline(cur, rest)
			}
			continue
		}
		if cur == nil {
			continue
		}
		if strings.HasPrefix(trimmed, "{") {
			parseMihomoInline(cur, trimmed)
			continue
		}
		k, v, ok := strings.Cut(trimmed, ":")
		if ok {
			fillMihomoField(cur, k, v)
		}
	}
	flush()
	if len(imp.Proxies) == 0 && len(imp.Skipped) == 0 {
		return nil, fmt.Errorf("未在配置中找到 proxies 列表")
	}
	return imp, nil
}
