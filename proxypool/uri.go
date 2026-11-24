package proxypool

// 分享链接解析：vmess / ss / trojan / vless -> mihomo proxy map。
// 不依赖 mihomo，进程内纯解析。

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"
)

type ParseIssue struct {
	Line   int    `json:"line"`
	Reason string `json:"reason"`
}

func ParseShareLinks(raw string) ([]map[string]any, []ParseIssue, error) {
	text := strings.TrimSpace(raw)
	if text == "" {
		return nil, nil, fmt.Errorf("分享链接为空")
	}
	if !strings.Contains(text, "://") {
		if b, err := uriB64Decode(text); err == nil && strings.Contains(string(b), "://") {
			text = string(b)
		}
	}
	var nodes []map[string]any
	var issues []ParseIssue
	for i, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		n, err := parseShareURI(line)
		if err != nil {
			issues = append(issues, ParseIssue{Line: i + 1, Reason: err.Error()})
			continue
		}
		if name, _ := n["name"].(string); isInfoNode(name) {
			continue
		}
		nodes = append(nodes, n)
	}
	if len(nodes) == 0 {
		if len(issues) > 0 {
			return nil, issues, fmt.Errorf("没有可用节点：%s", issues[0].Reason)
		}
		return nil, issues, fmt.Errorf("没有解析到节点")
	}
	return nodes, issues, nil
}

func isInfoNode(name string) bool {
	for _, p := range []string{"剩余流量", "套餐到期", "到期时间", "距离下次重置", "官网", "客服", "过期"} {
		if strings.Contains(name, p) {
			return true
		}
	}
	return false
}

func parseShareURI(uri string) (map[string]any, error) {
	switch {
	case strings.HasPrefix(uri, "vmess://"):
		return parseVmessURI(uri)
	case strings.HasPrefix(uri, "ss://"):
		return parseSSURI(uri)
	case strings.HasPrefix(uri, "trojan://"):
		return parseTrojanURI(uri)
	case strings.HasPrefix(uri, "vless://"):
		return parseVlessURI(uri)
	default:
		if len(uri) > 16 {
			uri = uri[:16]
		}
		return nil, fmt.Errorf("不支持的协议: %s", uri)
	}
}

func splitFragment(s string) (string, string) {
	if i := strings.Index(s, "#"); i >= 0 {
		frag, err := url.PathUnescape(s[i+1:])
		if err != nil {
			frag = s[i+1:]
		}
		return s[:i], frag
	}
	return s, ""
}

func splitQuery(s string) (string, url.Values) {
	if i := strings.Index(s, "?"); i >= 0 {
		q, err := url.ParseQuery(s[i+1:])
		if err != nil {
			return s[:i], url.Values{}
		}
		return s[:i], q
	}
	return s, url.Values{}
}

func uriHostPort(s string) (string, int, bool) {
	host, portStr, err := net.SplitHostPort(s)
	if err != nil {
		if i := strings.LastIndex(s, ":"); i >= 0 {
			host, portStr = s[:i], s[i+1:]
		} else {
			return "", 0, false
		}
	}
	host = strings.Trim(host, "[]")
	p, err := strconv.Atoi(portStr)
	if err != nil || p <= 0 || p > 65535 {
		return "", 0, false
	}
	return host, p, true
}

func uriB64Decode(s string) ([]byte, error) {
	s = strings.TrimSpace(s)
	encs := []*base64.Encoding{base64.RawURLEncoding, base64.URLEncoding, base64.RawStdEncoding, base64.StdEncoding}
	for _, e := range encs {
		if b, err := e.DecodeString(s); err == nil {
			return b, nil
		}
	}
	switch len(s) % 4 {
	case 2:
		s += "=="
	case 3:
		s += "="
	}
	for _, e := range encs {
		if b, err := e.DecodeString(s); err == nil {
			return b, nil
		}
	}
	return nil, fmt.Errorf("base64 解码失败")
}

func uriNewNode(name, typ, server string, port int) map[string]any {
	name = strings.TrimSpace(name)
	if name == "" {
		name = fmt.Sprintf("%s:%d", server, port)
	}
	if utf8.RuneCountInString(name) > 60 {
		name = string([]rune(name)[:60])
	}
	return map[string]any{"name": name, "type": typ, "server": server, "port": port}
}

func uriQ(q url.Values, keys ...string) string {
	for _, k := range keys {
		if v := strings.TrimSpace(q.Get(k)); v != "" {
			return v
		}
	}
	return ""
}

func uriJStr(j map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := j[k]; ok {
			switch t := v.(type) {
			case string:
				if s := strings.TrimSpace(t); s != "" {
					return s
				}
			case float64:
				return strconv.FormatFloat(t, 'f', -1, 64)
			}
		}
	}
	return ""
}

func uriJInt(j map[string]any, keys ...string) int {
	for _, k := range keys {
		if v, ok := j[k]; ok {
			switch t := v.(type) {
			case float64:
				return int(t)
			case string:
				if n, err := strconv.Atoi(strings.TrimSpace(t)); err == nil {
					return n
				}
			}
		}
	}
	return 0
}

func uriSplitComma(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func uriTransportOpts(node map[string]any, q url.Values) {
	network := uriQ(q, "type", "network")
	security := uriQ(q, "security")
	tls := uriQ(q, "tls")
	sni := uriQ(q, "sni", "peer", "host")
	reality := security == "reality"
	if security == "tls" || tls == "1" || tls == "true" || reality {
		node["tls"] = true
		node["skip-cert-verify"] = true
		if sni != "" {
			node["servername"] = sni
		}
		if alpn := uriQ(q, "alpn"); alpn != "" {
			node["alpn"] = uriSplitComma(alpn)
		}
		if fp := uriQ(q, "fp"); fp != "" {
			node["client-fingerprint"] = fp
		}
		if reality {
			ro := map[string]any{}
			if pbk := uriQ(q, "pbk"); pbk != "" {
				ro["public-key"] = pbk
			}
			if sid := uriQ(q, "sid"); sid != "" {
				ro["short-id"] = sid
			}
			node["reality-opts"] = ro
		}
	}
	switch network {
	case "ws", "websocket":
		wo := map[string]any{}
		if p := uriQ(q, "path", "wspath"); p != "" {
			wo["path"] = p
		}
		if h := uriQ(q, "host"); h != "" {
			wo["headers"] = map[string]any{"Host": h}
		}
		node["ws-opts"] = wo
	case "grpc":
		go_ := map[string]any{}
		if srv := uriQ(q, "serviceName", "grpc-service-name"); srv != "" {
			go_["grpc-service-name"] = srv
		}
		node["grpc-opts"] = go_
	case "obfs":
		po := map[string]any{}
		if m := uriQ(q, "obfsParam", "mode"); m != "" {
			po["mode"] = m
		}
		if h := uriQ(q, "host"); h != "" {
			po["host"] = h
		}
		node["plugin-opts"] = po
	}
	if network != "" {
		node["network"] = network
	}
}

func parseVmessURI(uri string) (map[string]any, error) {
	rest := strings.TrimPrefix(uri, "vmess://")
	body, name := splitFragment(rest)
	body, q := splitQuery(body)
	if !strings.Contains(body, "@") {
		raw, err := uriB64Decode(body)
		if err != nil {
			return nil, fmt.Errorf("vmess base64: %w", err)
		}
		var j map[string]any
		if err := json.Unmarshal(raw, &j); err != nil {
			return nil, fmt.Errorf("vmess json: %w", err)
		}
		host := uriJStr(j, "add")
		port := uriJInt(j, "port")
		if host == "" || port == 0 {
			return nil, fmt.Errorf("vmess 缺少 add/port")
		}
		node := uriNewNode(firstNonEmpty(name, uriJStr(j, "ps")), "vmess", host, port)
		node["uuid"] = uriJStr(j, "id")
		node["alterId"] = uriJInt(j, "aid")
		node["cipher"] = firstNonEmpty(uriJStr(j, "scy", "type"), "auto")
		if net_ := uriJStr(j, "net"); net_ != "" {
			node["network"] = net_
			switch net_ {
			case "ws":
				wo := map[string]any{}
				if p := uriJStr(j, "path"); p != "" {
					wo["path"] = p
				}
				if h := uriJStr(j, "host"); h != "" {
					wo["headers"] = map[string]any{"Host": h}
				}
				node["ws-opts"] = wo
			case "grpc":
				node["grpc-opts"] = map[string]any{"grpc-service-name": uriJStr(j, "path")}
			}
		}
		if uriJStr(j, "tls") == "tls" || uriJStr(j, "tls") == "1" {
			node["tls"] = true
			node["skip-cert-verify"] = true
			if s := uriJStr(j, "sni"); s != "" {
				node["servername"] = s
			}
		}
		return node, nil
	}
	at := strings.LastIndex(body, "@")
	uuid := body[:at]
	host, port, ok := uriHostPort(body[at+1:])
	if !ok {
		return nil, fmt.Errorf("vmess host:port 非法")
	}
	node := uriNewNode(name, "vmess", host, port)
	node["uuid"] = uuid
	node["cipher"] = "auto"
	uriTransportOpts(node, q)
	return node, nil
}

func parseSSURI(uri string) (map[string]any, error) {
	rest := strings.TrimPrefix(uri, "ss://")
	body, name := splitFragment(rest)
	body, q := splitQuery(body)
	var userInfo, hostport string
	if at := strings.LastIndex(body, "@"); at >= 0 {
		userInfo, hostport = body[:at], body[at+1:]
		if !strings.Contains(userInfo, ":") {
			if b, err := uriB64Decode(userInfo); err == nil {
				userInfo = string(b)
			}
		}
	} else {
		b, err := uriB64Decode(body)
		if err != nil {
			return nil, fmt.Errorf("ss base64: %w", err)
		}
		body = string(b)
		at := strings.LastIndex(body, "@")
		if at < 0 {
			return nil, fmt.Errorf("ss 缺少 @")
		}
		userInfo, hostport = body[:at], body[at+1:]
	}
	method, pass, _ := strings.Cut(userInfo, ":")
	if p, err := url.QueryUnescape(pass); err == nil {
		pass = p
	}
	host, port, ok := uriHostPort(hostport)
	if !ok {
		return nil, fmt.Errorf("ss host:port 非法")
	}
	node := uriNewNode(name, "ss", host, port)
	node["cipher"] = method
	node["password"] = pass
	if plugin := uriQ(q, "plugin"); plugin != "" {
		parts := strings.Split(plugin, ";")
		node["plugin"] = parts[0]
		po := map[string]any{}
		for _, kv := range parts[1:] {
			k, v, _ := strings.Cut(kv, "=")
			po[strings.TrimSpace(k)] = strings.TrimSpace(v)
		}
		node["plugin-opts"] = po
	}
	return node, nil
}

func parseTrojanURI(uri string) (map[string]any, error) {
	rest := strings.TrimPrefix(uri, "trojan://")
	body, name := splitFragment(rest)
	body, q := splitQuery(body)
	at := strings.LastIndex(body, "@")
	if at < 0 {
		return nil, fmt.Errorf("trojan 缺少 @")
	}
	pass, err := url.QueryUnescape(body[:at])
	if err != nil {
		pass = body[:at]
	}
	host, port, ok := uriHostPort(body[at+1:])
	if !ok {
		return nil, fmt.Errorf("trojan host:port 非法")
	}
	node := uriNewNode(name, "trojan", host, port)
	node["password"] = pass
	node["tls"] = true
	node["skip-cert-verify"] = true
	uriTransportOpts(node, q)
	if _, has := node["servername"]; !has {
		if sni := uriQ(q, "sni", "peer", "host"); sni != "" {
			node["servername"] = sni
		}
	}
	return node, nil
}

func parseVlessURI(uri string) (map[string]any, error) {
	rest := strings.TrimPrefix(uri, "vless://")
	body, name := splitFragment(rest)
	body, q := splitQuery(body)
	at := strings.LastIndex(body, "@")
	if at < 0 {
		return nil, fmt.Errorf("vless 缺少 @")
	}
	uuid := strings.TrimSpace(body[:at])
	host, port, ok := uriHostPort(body[at+1:])
	if !ok {
		return nil, fmt.Errorf("vless host:port 非法")
	}
	node := uriNewNode(name, "vless", host, port)
	node["uuid"] = uuid
	if flow := uriQ(q, "flow"); flow != "" {
		node["flow"] = flow
	}
	uriTransportOpts(node, q)
	return node, nil
}

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if strings.TrimSpace(s) != "" {
			return strings.TrimSpace(s)
		}
	}
	return ""
}
