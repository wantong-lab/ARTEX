package proxypool

// 代理池最小闭环（M4-03）：录入/探测/切换/回退。
// - Probe：经代理请求出口探测地址，记录 exit IP 与延迟；
// - Manager：当前活动代理 + 全局兜底代理；请求失败自动回退并累计失败次数；
// - Client：带「活动→兜底→直连」三级回退的 http.Client（请求体可重放时才重试）。

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	xproxy "golang.org/x/net/proxy"
)

// Entry 是代理池条目的最小视图（由调用方适配存储层）。
type Entry struct {
	ID     int64
	URL    string
	Status string
}

// Store 是 Manager 需要的存储面；db.ProxyStore 由 server 层适配。
type Store interface {
	List(ctx context.Context) ([]Entry, error)
	Get(ctx context.Context, id int64) (Entry, bool, error)
	RecordFailure(ctx context.Context, id int64, msg string) error
	UpdateProbe(ctx context.Context, id int64, exitIP string, latencyMs int, errMsg string) error
}

const (
	DefaultProbeURL = "https://api.ipify.org?format=json"
	defaultTimeout  = 20 * time.Second
)

// ParseProxyURL 校验并规范化代理 URL（http/https/socks5/socks5h，必须带端口）。
func ParseProxyURL(raw string) (*url.URL, error) {
	raw = strings.TrimSpace(raw)
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return nil, fmt.Errorf("代理 URL 非法：%s", raw)
	}
	scheme := strings.ToLower(u.Scheme)
	switch scheme {
	case "http", "https", "socks5", "socks5h":
	default:
		return nil, fmt.Errorf("不支持的代理协议 %q（支持 http/https/socks5/socks5h）", scheme)
	}
	if u.Port() == "" {
		return nil, fmt.Errorf("代理 URL 必须带端口")
	}
	u.Scheme = scheme
	return u, nil
}

// TransportFor 为单个代理 URL 构造 http.Transport。
func TransportFor(raw string) (*http.Transport, error) {
	u, err := ParseProxyURL(raw)
	if err != nil {
		return nil, err
	}
	tr := &http.Transport{
		Proxy:               http.ProxyURL(u),
		DialContext:         (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		ForceAttemptHTTP2:   true,
		MaxIdleConns:        50,
		IdleConnTimeout:     60 * time.Second,
		TLSHandshakeTimeout: 10 * time.Second,
		TLSClientConfig:     &tls.Config{MinVersion: tls.VersionTLS12},
	}
	return tr, nil
}

// socks5Transport 为 socks5 代理构造 Transport（http/https 走 x/net/proxy 拨号）。
func socks5Transport(u *url.URL) (*http.Transport, error) {
	var auth *xproxy.Auth
	if u.User != nil {
		pass, _ := u.User.Password()
		auth = &xproxy.Auth{User: u.User.Username(), Password: pass}
	}
	dialer, err := xproxy.SOCKS5("tcp", u.Host, auth, &net.Dialer{Timeout: 10 * time.Second})
	if err != nil {
		return nil, err
	}
	return &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return dialer.Dial(network, addr)
		},
		TLSHandshakeTimeout: 10 * time.Second,
		TLSClientConfig:     &tls.Config{MinVersion: tls.VersionTLS12},
	}, nil
}

// transportForAny 按 scheme 选择传输实现。
func transportForAny(raw string) (*http.Transport, error) {
	u, err := ParseProxyURL(raw)
	if err != nil {
		return nil, err
	}
	if u.Scheme == "socks5" || u.Scheme == "socks5h" {
		return socks5Transport(u)
	}
	return TransportFor(raw)
}

// ProbeResult 是一次出口探测结果。
type ProbeResult struct {
	OK        bool   `json:"ok"`
	ExitIP    string `json:"exit_ip,omitempty"`
	LatencyMs int64  `json:"latency_ms"`
	Error     string `json:"error,omitempty"`
}

// Manager 管理活动代理与全局兜底，并在失败时自动回退。
type Manager struct {
	store    Store
	probeURL atomic.Value // string

	mu      sync.Mutex
	active  int64
	global  string
	timeout time.Duration
}

func NewManager(store Store) *Manager {
	m := &Manager{store: store, timeout: defaultTimeout}
	m.probeURL.Store(DefaultProbeURL)
	return m
}

func (m *Manager) SetProbeURL(u string) {
	if strings.TrimSpace(u) != "" {
		m.probeURL.Store(strings.TrimSpace(u))
	}
}

func (m *Manager) ProbeURL() string { return m.probeURL.Load().(string) }

// SetActive 设置活动代理 id（0 = 不使用活动代理）。
func (m *Manager) SetActive(id int64) {
	m.mu.Lock()
	m.active = id
	m.mu.Unlock()
}

func (m *Manager) Active() int64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.active
}

// SetGlobal 设置全局兜底代理；空串表示回退直连。
func (m *Manager) SetGlobal(raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		m.mu.Lock()
		m.global = ""
		m.mu.Unlock()
		return nil
	}
	if _, err := ParseProxyURL(raw); err != nil {
		return err
	}
	m.mu.Lock()
	m.global = raw
	m.mu.Unlock()
	return nil
}

func (m *Manager) Global() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.global
}

// activeURL 返回当前活动代理 URL；条目不可用/状态非 active 时返回空。
func (m *Manager) activeURL(ctx context.Context) (int64, string) {
	id := m.Active()
	if id <= 0 || m.store == nil {
		return 0, ""
	}
	e, ok, err := m.store.Get(ctx, id)
	if err != nil || !ok || e.Status != "active" || strings.TrimSpace(e.URL) == "" {
		return 0, ""
	}
	return id, e.URL
}

// Probe 探测指定条目的出口 IP 与延迟，并把结果写回存储。
func (m *Manager) Probe(ctx context.Context, id int64) (ProbeResult, error) {
	if m.store == nil {
		return ProbeResult{}, fmt.Errorf("代理存储不可用")
	}
	e, ok, err := m.store.Get(ctx, id)
	if err != nil {
		return ProbeResult{}, err
	}
	if !ok {
		return ProbeResult{}, fmt.Errorf("代理 %d 不存在", id)
	}
	tr, err := transportForAny(e.URL)
	if err != nil {
		res := ProbeResult{OK: false, Error: err.Error()}
		_ = m.store.UpdateProbe(ctx, id, "", 0, res.Error)
		return res, nil
	}
	cli := &http.Client{Transport: tr, Timeout: m.timeout}
	start := time.Now()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, m.ProbeURL(), nil)
	if err != nil {
		return ProbeResult{}, err
	}
	req.Header.Set("User-Agent", "ARTEX-proxypool/1.0")
	resp, err := cli.Do(req)
	lat := time.Since(start).Milliseconds()
	if err != nil {
		res := ProbeResult{OK: false, LatencyMs: lat, Error: err.Error()}
		_ = m.store.UpdateProbe(ctx, id, "", int(lat), res.Error)
		return res, nil
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if resp.StatusCode != http.StatusOK {
		res := ProbeResult{OK: false, LatencyMs: lat, Error: fmt.Sprintf("探测地址返回 HTTP %d", resp.StatusCode)}
		_ = m.store.UpdateProbe(ctx, id, "", int(lat), res.Error)
		return res, nil
	}
	exitIP := parseExitIP(body)
	if exitIP == "" {
		res := ProbeResult{OK: false, LatencyMs: lat, Error: "探测响应无法解析出口 IP"}
		_ = m.store.UpdateProbe(ctx, id, "", int(lat), res.Error)
		return res, nil
	}
	_ = m.store.UpdateProbe(ctx, id, exitIP, int(lat), "")
	return ProbeResult{OK: true, ExitIP: exitIP, LatencyMs: lat}, nil
}

// parseExitIP 解析出口探测响应：JSON {"ip":...} / {"origin":...} 或纯文本。
func parseExitIP(body []byte) string {
	s := strings.TrimSpace(string(body))
	var m map[string]any
	if json.Unmarshal([]byte(s), &m) == nil {
		for _, k := range []string{"ip", "origin", "query"} {
			if v, ok := m[k].(string); ok && strings.TrimSpace(v) != "" {
				return strings.Fields(strings.TrimSpace(v))[0]
			}
		}
	}
	if f := strings.Fields(s); len(f) > 0 && net.ParseIP(f[0]) != nil {
		return f[0]
	}
	return ""
}

// fallbackTransport 实现「活动→兜底→直连」三级回退，失败时累计条目失败次数。
type fallbackTransport struct{ m *Manager }

func (t *fallbackTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	m := t.m
	ctx := req.Context()
	activeID, activeURL := m.activeURL(ctx)
	global := m.Global()

	type attempt struct {
		url string
		via string
	}
	var attempts []attempt
	if activeURL != "" {
		attempts = append(attempts, attempt{activeURL, "active"})
	}
	if global != "" && global != activeURL {
		attempts = append(attempts, attempt{global, "global"})
	}
	attempts = append(attempts, attempt{"", "direct"})

	var lastErr error
	for i, a := range attempts {
		hreq := req.Clone(ctx)
		if req.Body != nil {
			if req.GetBody == nil {
				if i > 0 {
					break // 请求体不可重放：只尝试第一级
				}
			} else if b, err := req.GetBody(); err == nil {
				hreq.Body = b
			}
		}
		var rt http.RoundTripper = http.DefaultTransport
		if a.url != "" {
			tr, err := transportForAny(a.url)
			if err != nil {
				lastErr = err
				continue
			}
			rt = tr
		}
		resp, err := rt.RoundTrip(hreq)
		if err == nil {
			return resp, nil
		}
		lastErr = err
		if a.via == "active" && activeID > 0 && m.store != nil {
			_ = m.store.RecordFailure(ctx, activeID, err.Error())
		}
	}
	if lastErr == nil {
		lastErr = errors.New("无可用代理路径")
	}
	return nil, lastErr
}

// Client 返回带回退能力的 http.Client（供出站请求统一使用）。
func (m *Manager) Client() *http.Client {
	return &http.Client{Transport: &fallbackTransport{m: m}, Timeout: m.timeout}
}

// Egress 用当前活动代理（失败自动回退）请求出口探测地址，返回出口 IP 与实际路径。
func (m *Manager) Egress(ctx context.Context) (exitIP, via string, err error) {
	cli := m.Client()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, m.ProbeURL(), nil)
	if err != nil {
		return "", "", err
	}
	resp, err := cli.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("出口探测返回 HTTP %d", resp.StatusCode)
	}
	return parseExitIP(body), "", nil
}
