package guard

// RoE 范围匹配（M2-04）：任务授权范围三态判定。
//   - InScope     目标落在任务 scope 内
//   - OutOfScope  任务有 scope 配置，但目标不在其中
//   - Unknown     任务没有 scope 配置（或目标/矩阵不可用）——按「未配置即不拦」处理，
//                 避免历史任务被一刀切拦死；strict 模式只对确定的 OutOfScope 生效。
// loopback/localhost 永远 InScope：平台自身回调、本地代理、测试桩不该被 RoE 拦。

import (
	"net"
	"strings"
)

type ScopeState int

const (
	Unknown ScopeState = iota
	InScope
	OutOfScope
)

func (s ScopeState) String() string {
	switch s {
	case InScope:
		return "in_scope"
	case OutOfScope:
		return "out_of_scope"
	default:
		return "unknown"
	}
}

// ScopeRule 是一条授权范围（kind: domain|ip|cidr|url）。
type ScopeRule struct {
	Kind  string
	Value string
}

type scopeNet struct {
	net *net.IPNet
}

// ScopeMatcher 是任务 scope 的匹配器（构建一次、热更新时重建；只读安全）。
type ScopeMatcher struct {
	domains []string
	ips     []string
	cidrs   []scopeNet
}

func NewScopeMatcher(rules []ScopeRule) *ScopeMatcher {
	m := &ScopeMatcher{}
	for _, r := range rules {
		v := strings.TrimSpace(r.Value)
		if v == "" {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(r.Kind)) {
		case "domain", "root_domain", "subdomain":
			if u, err := urlHost(v); err == nil {
				v = u
			}
			m.domains = append(m.domains, strings.ToLower(strings.TrimSuffix(v, ".")))
		case "cidr":
			if _, n, err := net.ParseCIDR(v); err == nil {
				m.cidrs = append(m.cidrs, scopeNet{net: n})
			}
		case "ip":
			if ip := net.ParseIP(v); ip != nil {
				m.ips = append(m.ips, ip.String())
			}
		default:
			// url 或未知类型：按 host 字面量参与域名匹配。
			if u, err := urlHost(v); err == nil {
				m.domains = append(m.domains, strings.ToLower(strings.TrimSuffix(u, ".")))
			}
		}
	}
	return m
}

func urlHost(v string) (string, error) {
	if strings.Contains(v, "://") {
		u, err := parseURL(v)
		if err != nil {
			return "", err
		}
		return u, nil
	}
	return v, nil
}

func parseURL(v string) (string, error) {
	if i := strings.Index(v, "://"); i >= 0 {
		rest := v[i+3:]
		if j := strings.IndexAny(rest, "/?#"); j >= 0 {
			rest = rest[:j]
		}
		if h, _, err := net.SplitHostPort(rest); err == nil {
			return h, nil
		}
		return rest, nil
	}
	return v, nil
}

func isLoopback(host string) bool {
	switch strings.ToLower(host) {
	case "localhost", "127.0.0.1", "::1", "0.0.0.0":
		return true
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsLoopback()
	}
	return false
}

// Match 判定单个主机/IP 的范围状态。
func (m *ScopeMatcher) Match(host string) ScopeState {
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "" {
		return Unknown
	}
	if isLoopback(host) {
		return InScope
	}
	if m == nil || (len(m.domains) == 0 && len(m.ips) == 0 && len(m.cidrs) == 0) {
		return Unknown
	}
	if ip := net.ParseIP(host); ip != nil {
		for _, want := range m.ips {
			if ip.String() == want {
				return InScope
			}
		}
		for _, c := range m.cidrs {
			if c.net != nil && c.net.Contains(ip) {
				return InScope
			}
		}
		return OutOfScope
	}
	for _, d := range m.domains {
		if host == d || strings.HasSuffix(host, "."+d) {
			return InScope
		}
	}
	return OutOfScope
}

// MatchTargets 汇总一组目标：任一 OutOfScope 即 OutOfScope（返回该目标）；
// 全 InScope 则 InScope；其余 Unknown。
func (m *ScopeMatcher) MatchTargets(ts []Target) (ScopeState, Target) {
	if len(ts) == 0 {
		return Unknown, Target{}
	}
	allIn := true
	for _, t := range ts {
		switch m.Match(t.Host) {
		case OutOfScope:
			return OutOfScope, t
		case Unknown:
			allIn = false
		}
	}
	if allIn {
		return InScope, Target{}
	}
	return Unknown, Target{}
}
