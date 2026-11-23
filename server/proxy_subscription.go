package server

// Clash 订阅代理：订阅 -> 本地 mihomo mixed 口 -> 代理池本地条目。

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/Autumn-27/artex/db"
)

type proxySubscriptionState struct {
	cmd       *exec.Cmd
	port      int
	entryID   int64
	bin       string
	dir       string
	logPath   string
	source    string
	startedAt time.Time
	lastErr   string
}

func (s *Server) mihomoBinary() (string, error) {
	if p := strings.TrimSpace(os.Getenv("ARTEX_MIHOMO_PATH")); p != "" {
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p, nil
		}
	}
	bin := "mihomo"
	switch runtime.GOOS {
	case "windows":
		bin = "mihomo-windows-" + runtime.GOARCH + ".exe"
	case "darwin", "linux":
		bin = "mihomo-" + runtime.GOOS + "-" + runtime.GOARCH
	}
	cands := make([]string, 0, 12)
	if s.dataDir != "" {
		cands = append(cands, filepath.Join(s.dataDir, "tools", "mihomo", bin))
	}
	if exe, err := os.Executable(); err == nil {
		d := filepath.Dir(exe)
		cands = append(cands,
			filepath.Join(d, "data", "tools", "mihomo", bin),
			filepath.Join(d, "tools", "mihomo", bin),
			filepath.Join(d, bin))
	}
	for _, name := range []string{"mihomo", "verge-mihomo", "clash-meta", "mihomo-alpha"} {
		if p, err := exec.LookPath(name); err == nil {
			cands = append(cands, p)
		}
	}
	if runtime.GOOS == "darwin" {
		cands = append(cands,
			"/Applications/Clash Verge.app/Contents/MacOS/verge-mihomo",
			"/Applications/Clash Verge.app/Contents/MacOS/verge-mihomo-alpha")
	}
	for _, c := range cands {
		if st, err := os.Stat(c); err == nil && !st.IsDir() {
			return c, nil
		}
	}
	return "", fmt.Errorf("未找到 mihomo 二进制：可设置 ARTEX_MIHOMO_PATH，或放到 data/tools/mihomo/%s，或安装到 PATH", bin)
}

func looksLikeClashConfig(raw string) bool {
	for _, line := range strings.Split(raw, "\n") {
		if strings.TrimSpace(line) == "proxies:" {
			return true
		}
	}
	return false
}

func subscriptionCandidateURLs(raw string) []string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return []string{raw}
	}
	out := []string{raw}
	seen := map[string]bool{raw: true}
	for _, kv := range [][2]string{{"flag", "meta"}, {"flag", "clash"}, {"target", "clash"}} {
		cp := *u
		q := cp.Query()
		if q.Get(kv[0]) == kv[1] {
			continue
		}
		q.Set(kv[0], kv[1])
		cp.RawQuery = q.Encode()
		cand := cp.String()
		if !seen[cand] {
			seen[cand] = true
			out = append(out, cand)
		}
	}
	return out
}

func detectSubscriptionProtocols(raw string) map[string]int {
	out := map[string]int{}
	text := raw
	for _, dec := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding} {
		if decoded, err := dec.DecodeString(strings.TrimSpace(raw)); err == nil && strings.Contains(string(decoded), "://") {
			text = string(decoded)
			break
		}
	}
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if i := strings.Index(line, "://"); i > 0 {
			out[strings.ToLower(line[:i])]++
		}
	}
	return out
}

func fetchSubscriptionBody(ctx context.Context, client *http.Client, rawURL string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "mihomo/1.19.0")
	req.Header.Set("Accept", "application/yaml,text/yaml,text/plain,*/*")
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("订阅拉取失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("订阅返回 HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func (s *Server) fetchClashSubscription(ctx context.Context, client *http.Client, rawURL string) (string, error) {
	var lastErr error
	var lastBody string
	for _, cand := range subscriptionCandidateURLs(rawURL) {
		body, err := fetchSubscriptionBody(ctx, client, cand)
		if err != nil {
			lastErr = err
			continue
		}
		lastBody = body
		if looksLikeClashConfig(body) {
			return body, nil
		}
		lastErr = fmt.Errorf("订阅响应不是 Clash 配置（未找到 proxies）")
	}
	if counts := detectSubscriptionProtocols(lastBody); len(counts) > 0 {
		parts := make([]string, 0, len(counts))
		for k, n := range counts {
			parts = append(parts, fmt.Sprintf("%s×%d", k, n))
		}
		return "", fmt.Errorf("订阅不是 Clash 配置，检测到节点：%s；需要 mihomo/Clash.Meta 格式或本地内核转换", strings.Join(parts, ", "))
	}
	if lastErr != nil {
		return "", lastErr
	}
	return "", fmt.Errorf("订阅不是 Clash 配置")
}

func buildMihomoConfig(raw string, mixedPort int) ([]byte, error) {
	var cfg map[string]any
	if err := yaml.Unmarshal([]byte(raw), &cfg); err != nil {
		return nil, fmt.Errorf("Clash 配置解析失败: %w", err)
	}
	if _, ok := cfg["proxies"]; !ok {
		return nil, fmt.Errorf("Clash 配置没有 proxies")
	}
	cfg["mixed-port"] = mixedPort
	cfg["allow-lan"] = false
	cfg["mode"] = "rule"
	cfg["log-level"] = "warning"
	delete(cfg, "port")
	delete(cfg, "socks-port")
	delete(cfg, "redir-port")
	delete(cfg, "tproxy-port")
	delete(cfg, "external-controller")
	delete(cfg, "secret")
	if _, ok := cfg["rules"]; !ok {
		if ps, ok := cfg["proxies"].([]any); ok && len(ps) > 0 {
			if m, ok := ps[0].(map[string]any); ok {
				if name, ok := m["name"].(string); ok && name != "" {
					cfg["rules"] = []any{"MATCH," + name}
				}
			}
		}
	}
	return yaml.Marshal(cfg)
}

func freeLocalPort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}

func (s *Server) startProxySubscription(ctx context.Context, rawURL string) (map[string]any, error) {
	if err := validateSubscriptionURL(rawURL); err != nil {
		return nil, err
	}
	rt := s.proxyPool()
	if rt == nil {
		return nil, fmt.Errorf("代理池不可用")
	}
	bin, err := s.mihomoBinary()
	if err != nil {
		return nil, err
	}
	client := &http.Client{Transport: rt.mgr.Client().Transport, Timeout: 30 * time.Second}
	rawCfg, err := s.fetchClashSubscription(ctx, client, rawURL)
	if err != nil {
		return nil, err
	}
	port, err := freeLocalPort()
	if err != nil {
		return nil, err
	}
	cfgBytes, err := buildMihomoConfig(rawCfg, port)
	if err != nil {
		return nil, err
	}
	if s.dataDir == "" {
		return nil, fmt.Errorf("数据目录未配置")
	}
	dir := filepath.Join(s.dataDir, "proxy-sub")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	cfgPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfgPath, cfgBytes, 0o600); err != nil {
		return nil, err
	}
	logPath := filepath.Join(dir, "mihomo.log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return nil, err
	}
	cmd := exec.Command(bin, "-d", dir, "-f", cfgPath)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	if err := cmd.Start(); err != nil {
		logFile.Close()
		return nil, fmt.Errorf("mihomo 启动失败: %w", err)
	}
	_ = logFile.Close()
	up := false
	start := time.Now()
	for time.Since(start) < 20*time.Second {
		conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 500*time.Millisecond)
		if err == nil {
			conn.Close()
			up = true
			break
		}
		time.Sleep(300 * time.Millisecond)
	}
	if !up {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
		return nil, fmt.Errorf("mihomo 未监听 127.0.0.1:%d，查看 %s", port, logPath)
	}
	entryID, _, err := rt.dbStore.Create(ctx, &db.ProxyEntry{
		Name: "clash-sub",
		URL:  fmt.Sprintf("http://127.0.0.1:%d", port),
	})
	if err != nil {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
		return nil, fmt.Errorf("登记本地代理失败: %w", err)
	}

	s.proxySubMu.Lock()
	defer s.proxySubMu.Unlock()
	if s.proxySub != nil {
		s.stopProxySubscriptionLocked(ctx)
	}
	s.proxySub = &proxySubscriptionState{
		cmd: cmd, port: port, entryID: entryID, bin: bin, dir: dir,
		logPath: logPath, source: rawURL, startedAt: time.Now(),
	}
	rt.mgr.SetActive(entryID)
	_ = s.m.pg.SetSetting("proxy_active_id", strconv.FormatInt(entryID, 10))
	s.syncPoolEgress(ctx, true)
	return s.proxySubscriptionStatusLocked(), nil
}

func (s *Server) stopProxySubscription(ctx context.Context) {
	s.proxySubMu.Lock()
	defer s.proxySubMu.Unlock()
	s.stopProxySubscriptionLocked(ctx)
}

// StopProxySubscription 供主进程优雅退出时回收本地 mihomo/代理条目。
func (s *Server) StopProxySubscription() {
	s.stopProxySubscription(context.Background())
}

func (s *Server) stopProxySubscriptionLocked(ctx context.Context) {
	st := s.proxySub
	if st == nil {
		return
	}
	s.proxySub = nil
	if st.cmd != nil && st.cmd.Process != nil {
		_ = st.cmd.Process.Kill()
		_, _ = st.cmd.Process.Wait()
	}
	if st.dir != "" {
		_ = os.RemoveAll(st.dir)
	}
	if st.entryID > 0 {
		if rt := s.proxyPool(); rt != nil {
			if rt.mgr.Active() == st.entryID {
				rt.mgr.SetActive(0)
				_ = s.m.pg.SetSetting("proxy_active_id", "0")
			}
			_ = rt.dbStore.Delete(ctx, st.entryID)
			s.syncPoolEgress(ctx, true)
		}
	}
}

func (s *Server) proxySubscriptionStatusLocked() map[string]any {
	out := map[string]any{
		"egress_mode":     s.m.EgressMode(),
		"effective_proxy": s.m.EffectiveProxy(),
		"running":         false,
	}
	if bin, err := s.mihomoBinary(); err == nil {
		out["mihomo_available"] = true
		out["mihomo_binary"] = bin
	} else {
		out["mihomo_available"] = false
		out["mihomo_error"] = err.Error()
	}
	if st := s.proxySub; st != nil {
		alive := false
		if conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", st.port), 500*time.Millisecond); err == nil {
			alive = true
			conn.Close()
		}
		out["running"] = alive
		out["local_proxy"] = fmt.Sprintf("http://127.0.0.1:%d", st.port)
		out["entry_id"] = st.entryID
		out["started_at"] = st.startedAt
		if st.lastErr != "" {
			out["error"] = st.lastErr
		}
	}
	return out
}

func (s *Server) proxyPoolSubscriptionStatus(w http.ResponseWriter, r *http.Request) {
	s.proxySubMu.Lock()
	defer s.proxySubMu.Unlock()
	writeJSON(w, http.StatusOK, s.proxySubscriptionStatusLocked())
}

func (s *Server) proxyPoolSubscriptionStart(w http.ResponseWriter, r *http.Request) {
	var body struct {
		URL string `json:"url"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "请求体解析失败"})
		return
	}
	out, err := s.startProxySubscription(r.Context(), strings.TrimSpace(body.URL))
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) proxyPoolSubscriptionStop(w http.ResponseWriter, r *http.Request) {
	s.stopProxySubscription(r.Context())
	s.proxySubMu.Lock()
	out := s.proxySubscriptionStatusLocked()
	s.proxySubMu.Unlock()
	writeJSON(w, http.StatusOK, out)
}
