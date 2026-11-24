package server

// Clash 订阅代理：订阅 -> 本地 mihomo mixed 口 -> 代理池本地条目。

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
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
	"github.com/Autumn-27/artex/proxypool"
)

type proxySubscriptionState struct {
	cmd        *exec.Cmd
	port       int
	entryID    int64
	bin        string
	dir        string
	logPath    string
	source     string
	sourceID   int64
	ctlPort    int
	ctlSecret  string
	nodes      []mihomoNode
	current    string
	autoRotate bool
	banStrikes int
	banWindow  []time.Time
	startedAt  time.Time
	lastErr    string
}

func (s *Server) egressSourceStore() *db.EgressSourceStore {
	return db.NewEgressSourceStore(s.m.pg, s.jwtKey)
}

func (s *Server) egressControlPlaneEnabled() bool {
	return s.m != nil && s.m.pg != nil && s.m.pg.GetBool("egress_control_plane_enabled", false)
}

func fetchErrText(err error) string {
	if err == nil {
		return ""
	}
	s := err.Error()
	for _, marker := range []string{"http://", "https://"} {
		if i := strings.Index(s, marker); i >= 0 {
			s = s[:i] + "<url>"
			break
		}
	}
	if len(s) > 300 {
		s = s[:300]
	}
	return s
}

func (s *Server) mihomoControllerRequest(ctx context.Context, st *proxySubscriptionState, method, path string, body any) ([]byte, int, error) {
	if st == nil || st.ctlPort <= 0 || st.ctlSecret == "" {
		return nil, 0, fmt.Errorf("mihomo 控制面未就绪")
	}
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, 0, err
		}
		rdr = strings.NewReader(string(b))
	}
	req, err := http.NewRequestWithContext(ctx, method, fmt.Sprintf("http://127.0.0.1:%d%s", st.ctlPort, path), rdr)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Authorization", "Bearer "+st.ctlSecret)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return data, resp.StatusCode, fmt.Errorf("mihomo 控制面返回 HTTP %d", resp.StatusCode)
	}
	return data, resp.StatusCode, nil
}

func (s *Server) applyMihomoSelection(ctx context.Context, st *proxySubscriptionState, mode, node string) error {
	name := "ARTEX-AUTO"
	if mode == "manual" && strings.TrimSpace(node) != "" {
		name = node
	}
	_, _, err := s.mihomoControllerRequest(ctx, st, http.MethodPut, "/proxies/ARTEX-SELECT", map[string]any{"name": name})
	return err
}

func (s *Server) controlHTTPClient() *http.Client {
	if p := s.m.ControlProxy(); p != "" {
		if tr, err := proxypool.TransportFor(p); err == nil {
			return &http.Client{Transport: tr, Timeout: 30 * time.Second}
		}
	}
	return &http.Client{Timeout: 30 * time.Second}
}

func (s *Server) proxySubDir() string {
	if s.dataDir == "" {
		return ""
	}
	return filepath.Join(s.dataDir, "proxy-sub")
}

func portAvailable(port int) bool {
	if port <= 0 {
		return false
	}
	l, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return false
	}
	_ = l.Close()
	return true
}

// cleanupStaleMihomo 在 HTTP 服务启动前清理上次进程遗留的受管子目录。
func (s *Server) cleanupStaleMihomo() {
	base := s.proxySubDir()
	if base == "" {
		return
	}
	entries, err := os.ReadDir(base)
	if err != nil {
		return
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(base, e.Name())
		cleanupOrphanMihomo(dir)
		_ = os.RemoveAll(dir)
	}
}

// cleanupOrphanMihomo 只清理本目录 pid 文件记录且命令行像 mihomo 的进程。
func cleanupOrphanMihomo(dir string) {
	if dir == "" {
		return
	}
	b, err := os.ReadFile(filepath.Join(dir, "pid"))
	if err != nil {
		return
	}
	var pid int
	var bin string
	if _, err := fmt.Sscanf(strings.TrimSpace(string(b)), "%d %s", &pid, &bin); err != nil || pid <= 0 {
		return
	}
	out, err := exec.Command("ps", "-p", strconv.Itoa(pid), "-o", "command=").Output()
	if err != nil {
		return
	}
	if !strings.Contains(strings.ToLower(string(out)), "mihomo") {
		return
	}
	if p, err := os.FindProcess(pid); err == nil {
		_ = p.Kill()
	}
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

type mihomoNode struct {
	Name   string `json:"name"`
	Type   string `json:"type"`
	Server string `json:"server"`
	Port   int    `json:"port"`
}

func parseMihomoNodes(raw string) []mihomoNode {
	var cfg map[string]any
	if yaml.Unmarshal([]byte(raw), &cfg) != nil {
		return nil
	}
	ps, _ := cfg["proxies"].([]any)
	out := make([]mihomoNode, 0, len(ps))
	for _, p := range ps {
		m, ok := p.(map[string]any)
		if !ok {
			continue
		}
		n := mihomoNode{}
		n.Name, _ = m["name"].(string)
		n.Type, _ = m["type"].(string)
		n.Server, _ = m["server"].(string)
		switch v := m["port"].(type) {
		case int:
			n.Port = v
		case int64:
			n.Port = int(v)
		case float64:
			n.Port = int(v)
		}
		if n.Name != "" && !isInfoNode(n.Name) {
			out = append(out, n)
		}
	}
	return out
}

func isInfoNode(name string) bool {
	for _, prefix := range []string{"剩余流量", "套餐到期", "距离下次重置", "到期时间"} {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

func buildMihomoConfig(raw string, mixedPort int) ([]byte, error) {
	return buildMihomoConfigWithSelection(raw, mixedPort, "", "auto", 0, "")
}

func buildMihomoConfigWithSelection(raw string, mixedPort int, selected, mode string, extPort int, secret string) ([]byte, error) {
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
	for _, k := range []string{"port", "socks-port", "redir-port", "tproxy-port", "external-controller", "secret", "tun", "proxy-providers", "rule-providers", "script", "profile", "interface-name", "dns", "proxy-groups", "rules", "geodata-mode", "geo-auto-update", "geox-url"} {
		delete(cfg, k)
	}
	nodes := parseMihomoNodes(raw)
	if len(nodes) == 0 {
		return nil, fmt.Errorf("Clash 配置没有可用节点")
	}
	names := make([]any, 0, len(nodes))
	byName := map[string]bool{}
	for _, n := range nodes {
		names = append(names, n.Name)
		byName[n.Name] = true
	}
	autoGroup := map[string]any{
		"name": "ARTEX-AUTO", "type": "load-balance", "strategy": "consistent-hashing", "proxies": names,
		"url": "http://www.gstatic.com/generate_204", "interval": 300,
	}
	selectNames := make([]any, 0, len(nodes)+1)
	if mode == "manual" && selected != "" && byName[selected] {
		selectNames = append(selectNames, selected)
	}
	selectNames = append(selectNames, "ARTEX-AUTO")
	for _, n := range nodes {
		if n.Name != selected || mode != "manual" {
			selectNames = append(selectNames, n.Name)
		}
	}
	selectGroup := map[string]any{
		"name": "ARTEX-SELECT", "type": "select", "proxies": selectNames,
	}
	cfg["proxy-groups"] = []any{selectGroup, autoGroup}
	cfg["rules"] = []any{"MATCH,ARTEX-SELECT"}
	if extPort > 0 && secret != "" {
		cfg["external-controller"] = fmt.Sprintf("127.0.0.1:%d", extPort)
		cfg["secret"] = secret
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
	s.proxySubMu.Lock()
	defer s.proxySubMu.Unlock()
	rt := s.proxyPool()
	if rt == nil {
		return nil, fmt.Errorf("代理池不可用")
	}
	bin, err := s.mihomoBinary()
	if err != nil {
		return nil, err
	}
	srcStore := s.egressSourceStore()
	src, err := srcStore.UpsertSubscription(ctx, "clash-sub", rawURL)
	if err != nil {
		return nil, fmt.Errorf("保存订阅源失败: %w", err)
	}
	fail := func(e error) (map[string]any, error) {
		_ = srcStore.UpdateObserved(ctx, src.ID, "failed", fetchErrText(e), 0, 0)
		return nil, e
	}
	client := s.controlHTTPClient()
	rawCfg, fetchErr := s.fetchClashSubscription(ctx, client, rawURL)
	degraded := false
	if fetchErr != nil {
		if strings.TrimSpace(src.LastGoodConfig) == "" {
			return fail(fmt.Errorf("订阅拉取失败: %w", fetchErr))
		}
		rawCfg = src.LastGoodConfig
		degraded = true
	}
	port := 0
	if src.ManagedPort > 0 && portAvailable(src.ManagedPort) {
		port = src.ManagedPort
	} else {
		port, err = freeLocalPort()
		if err != nil {
			return fail(err)
		}
	}
	extPort, err := freeLocalPort()
	if err != nil {
		return fail(err)
	}
	var secretBytes [18]byte
	if _, err := rand.Read(secretBytes[:]); err != nil {
		return fail(err)
	}
	secret := base64.RawURLEncoding.EncodeToString(secretBytes[:])
	cfgBytes, err := buildMihomoConfigWithSelection(rawCfg, port, src.SelectedNode, src.SelectionMode, extPort, secret)
	if err != nil {
		return fail(err)
	}
	if s.dataDir == "" {
		return fail(fmt.Errorf("数据目录未配置"))
	}
	baseDir := filepath.Join(s.dataDir, "proxy-sub")
	dir := filepath.Join(baseDir, strconv.Itoa(port))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fail(err)
	}
	cfgPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfgPath, cfgBytes, 0o600); err != nil {
		return fail(err)
	}
	logPath := filepath.Join(dir, "mihomo.log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return fail(err)
	}
	cmd := exec.Command(bin, "-d", dir, "-f", cfgPath)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	if err := cmd.Start(); err != nil {
		logFile.Close()
		return fail(fmt.Errorf("mihomo 启动失败: %w", err))
	}
	_ = logFile.Close()
	_ = os.WriteFile(filepath.Join(dir, "pid"), []byte(fmt.Sprintf("%d %s", cmd.Process.Pid, bin)), 0o600)
	up := false
	start := time.Now()
	for time.Since(start) < 60*time.Second {
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
		return fail(fmt.Errorf("mihomo 未监听 127.0.0.1:%d，查看 %s", port, logPath))
	}
	entryID, _, err := rt.dbStore.Create(ctx, &db.ProxyEntry{
		Name: "clash-sub",
		URL:  fmt.Sprintf("http://127.0.0.1:%d", port),
	})
	if err != nil {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
		return fail(fmt.Errorf("登记本地代理失败: %w", err))
	}

	if s.proxySub != nil {
		s.stopProxySubscriptionLocked(ctx, false)
	}
	go func(c *exec.Cmd) { _ = c.Wait() }(cmd)
	s.proxySub = &proxySubscriptionState{
		cmd: cmd, port: port, entryID: entryID, bin: bin, dir: dir,
		logPath: logPath, source: rawURL, sourceID: src.ID, startedAt: time.Now(),
		ctlPort: extPort, ctlSecret: secret, nodes: parseMihomoNodes(rawCfg),
		current: src.SelectedNode, autoRotate: src.AutoRotate,
	}
	if s.proxySub.current == "" {
		s.proxySub.current = "ARTEX-AUTO"
	}
	_ = s.applyMihomoSelection(ctx, s.proxySub, src.SelectionMode, src.SelectedNode)
	rt.mgr.SetActive(entryID)
	_ = s.m.pg.SetSetting("proxy_active_id", strconv.FormatInt(entryID, 10))
	state := "ready"
	if degraded {
		state = "degraded"
	}
	_ = srcStore.UpdateObserved(ctx, src.ID, state, fetchErrText(fetchErr), entryID, port)
	if fetchErr == nil {
		_ = srcStore.SaveLastGood(ctx, src.ID, rawCfg)
	}
	s.syncPoolEgress(ctx, true)
	return s.proxySubscriptionStatusLocked(), nil
}

func (s *Server) stopProxySubscription(ctx context.Context) {
	s.proxySubMu.Lock()
	defer s.proxySubMu.Unlock()
	s.stopProxySubscriptionLocked(ctx, true)
}

// StopProxySubscription 供主进程优雅退出时回收本地 mihomo/代理条目。
func (s *Server) StopProxySubscription() {
	s.proxySubMu.Lock()
	defer s.proxySubMu.Unlock()
	s.stopProxySubscriptionLocked(context.Background(), false)
}

func (s *Server) stopProxySubscriptionLocked(ctx context.Context, markDisabled bool) {
	st := s.proxySub
	if st == nil {
		return
	}
	s.proxySub = nil
	if markDisabled && st.sourceID > 0 {
		store := s.egressSourceStore()
		_ = store.SetDesired(ctx, st.sourceID, "disabled")
		_ = store.UpdateObserved(ctx, st.sourceID, "disabled", "", 0, 0)
	}
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
	if src, err := s.egressSourceStore().GetSubscription(context.Background()); err == nil && src != nil {
		out["source_id"] = src.ID
		out["desired_state"] = src.DesiredState
		out["observed_state"] = src.ObservedState
		out["managed_proxy_id"] = src.ManagedProxyID
		out["managed_port"] = src.ManagedPort
		out["last_fetch_at"] = src.LastFetchAt
		out["last_error"] = src.LastError
		out["selected_node"] = src.SelectedNode
		out["selection_mode"] = src.SelectionMode
		out["auto_rotate"] = src.AutoRotate
		if st := s.proxySub; st != nil {
			out["ban_strikes"] = st.banStrikes
			out["current_node"] = st.current
		}
	}
	out["fail_closed"] = s.m.FailClosed()
	out["control_plane_enabled"] = s.egressControlPlaneEnabled()
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

// recoverProxySubscription 在服务启动后恢复 desired_state=active 的订阅源。
func (s *Server) recoverProxySubscription(ctx context.Context) {
	if !s.egressControlPlaneEnabled() {
		return
	}
	src, err := s.egressSourceStore().GetSubscription(ctx)
	if err != nil || src == nil || src.DesiredState != "active" || strings.TrimSpace(src.ConfigURL) == "" {
		return
	}
	s.proxySubMu.Lock()
	running := s.proxySub != nil
	s.proxySubMu.Unlock()
	if running {
		return
	}
	if _, err := s.startProxySubscription(ctx, src.ConfigURL); err != nil {
		_ = s.egressSourceStore().UpdateObserved(ctx, src.ID, "failed", fetchErrText(err), 0, 0)
	}
}

func proxySubscriptionProcessAlive(st *proxySubscriptionState) bool {
	if st == nil || st.cmd == nil || st.cmd.Process == nil || st.cmd.ProcessState != nil {
		return false
	}
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", st.port), 500*time.Millisecond)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

func (s *Server) watchProxySubscription(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	failures := 0
	backoff := 5 * time.Second
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		if !s.egressControlPlaneEnabled() {
			failures, backoff = 0, 5*time.Second
			continue
		}
		src, err := s.egressSourceStore().GetSubscription(ctx)
		if err != nil || src == nil || src.DesiredState != "active" || strings.TrimSpace(src.ConfigURL) == "" {
			failures, backoff = 0, 5*time.Second
			continue
		}
		s.proxySubMu.Lock()
		alive := proxySubscriptionProcessAlive(s.proxySub)
		s.proxySubMu.Unlock()
		if alive {
			failures, backoff = 0, 5*time.Second
			continue
		}
		if failures >= 5 {
			continue
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		if _, err := s.startProxySubscription(ctx, src.ConfigURL); err != nil {
			failures++
			log.Printf("[egress] 订阅 mihomo 守护重启失败(%d): %v", failures, err)
			switch backoff {
			case 5 * time.Second:
				backoff = 15 * time.Second
			case 15 * time.Second:
				backoff = 30 * time.Second
			default:
				backoff = 60 * time.Second
			}
		} else {
			failures, backoff = 0, 5*time.Second
		}
	}
}

func (s *Server) proxyPoolSubscriptionNodes(w http.ResponseWriter, r *http.Request) {
	src, err := s.egressSourceStore().GetSubscription(r.Context())
	if err != nil || src == nil {
		writeJSON(w, http.StatusOK, map[string]any{"nodes": []any{}, "selected_node": "", "selection_mode": "auto"})
		return
	}
	nodes := parseMihomoNodes(src.LastGoodConfig)
	writeJSON(w, http.StatusOK, map[string]any{
		"nodes": nodes, "selected_node": src.SelectedNode, "selection_mode": src.SelectionMode,
	})
}

func (s *Server) proxyPoolSubscriptionSelect(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Mode string `json:"mode"`
		Name string `json:"name"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "请求体解析失败"})
		return
	}
	mode := strings.ToLower(strings.TrimSpace(body.Mode))
	if mode != "auto" && mode != "manual" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "mode 仅支持 auto/manual"})
		return
	}
	src, err := s.egressSourceStore().GetSubscription(r.Context())
	if err != nil || src == nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "没有订阅源"})
		return
	}
	if mode == "manual" {
		ok := false
		for _, n := range parseMihomoNodes(src.LastGoodConfig) {
			if n.Name == body.Name {
				ok = true
				break
			}
		}
		if !ok || strings.TrimSpace(body.Name) == "" {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "节点不存在"})
			return
		}
	}
	s.proxySubMu.Lock()
	st := s.proxySub
	if st != nil {
		_ = s.applyMihomoSelection(r.Context(), st, mode, body.Name)
	}
	s.proxySubMu.Unlock()
	node := ""
	if mode == "manual" {
		node = body.Name
	}
	_ = s.egressSourceStore().UpdateSelection(r.Context(), src.ID, mode, node)
	_ = s.egressSourceStore().UpdateAutoRotate(r.Context(), src.ID, mode == "auto")
	s.proxySubMu.Lock()
	if s.proxySub != nil {
		s.proxySub.autoRotate = mode == "auto"
		if mode == "manual" {
			s.proxySub.current = node
		}
	}
	s.proxySubMu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "mode": mode, "selected_node": node})
}

func (s *Server) proxyPoolSubscriptionProbe(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body); err != nil || strings.TrimSpace(body.Name) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "name 必填"})
		return
	}
	src, err := s.egressSourceStore().GetSubscription(r.Context())
	if err != nil || src == nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "没有订阅源"})
		return
	}
	ok := false
	for _, n := range parseMihomoNodes(src.LastGoodConfig) {
		if n.Name == body.Name {
			ok = true
			break
		}
	}
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "节点不存在"})
		return
	}
	s.proxySubMu.Lock()
	st := s.proxySub
	var data []byte
	var code int
	var err2 error
	if st != nil {
		p := "/proxies/" + url.PathEscape(body.Name) + "/delay?url=" + url.QueryEscape("http://www.gstatic.com/generate_204") + "&timeout=3000"
		data, code, err2 = s.mihomoControllerRequest(r.Context(), st, http.MethodGet, p, nil)
	}
	s.proxySubMu.Unlock()
	if st == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "订阅代理未运行"})
		return
	}
	if err2 != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": err2.Error(), "status": code})
		return
	}
	var out struct {
		Delay int    `json:"delay"`
		Msg   string `json:"message"`
	}
	_ = json.Unmarshal(data, &out)
	if out.Delay <= 0 && out.Msg != "" {
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": out.Msg})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "delay": out.Delay})
}

func (s *Server) mihomoSelectNode(ctx context.Context, st *proxySubscriptionState, name string) error {
	_, _, err := s.mihomoControllerRequest(ctx, st, http.MethodPut, "/proxies/ARTEX-SELECT", map[string]any{"name": name})
	return err
}

// rotateProxySubscriptionLocked 按封禁信号切到下一个节点；调用方必须持有 proxySubMu。
func (s *Server) rotateProxySubscriptionLocked(ctx context.Context, reason string) (string, error) {
	st := s.proxySub
	if st == nil || len(st.nodes) == 0 {
		return "", fmt.Errorf("订阅代理未运行")
	}
	if !st.autoRotate {
		return "", nil
	}
	now := time.Now()
	cutoff := now.Add(-5 * time.Minute)
	win := st.banWindow[:0]
	for _, t := range st.banWindow {
		if t.After(cutoff) {
			win = append(win, t)
		}
	}
	st.banWindow = append(win, now)
	st.banStrikes++
	if len(st.banWindow) >= len(st.nodes) {
		st.autoRotate = false
		_ = s.egressSourceStore().UpdateAutoRotate(ctx, st.sourceID, false)
		_ = s.egressSourceStore().UpdateLastError(ctx, st.sourceID, fmt.Sprintf("节点池疑似耗尽（5 分钟内切换 %d 次），已暂停自动换 IP", len(st.banWindow)))
		return "", fmt.Errorf("节点池疑似耗尽（5 分钟内切换 %d 次），已暂停自动换 IP", len(st.banWindow))
	}
	cur := st.current
	next := ""
	for i, n := range st.nodes {
		if n.Name == cur {
			next = st.nodes[(i+1)%len(st.nodes)].Name
			break
		}
	}
	if next == "" {
		next = st.nodes[0].Name
	}
	if err := s.mihomoSelectNode(ctx, st, next); err != nil {
		return "", err
	}
	st.current = next
	_ = s.egressSourceStore().UpdateSelection(ctx, st.sourceID, "auto", next)
	return next, nil
}

func (s *Server) noteProxyPoolBan(status int) {
	if status != http.StatusForbidden && status != http.StatusTooManyRequests {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		s.proxySubMu.Lock()
		defer s.proxySubMu.Unlock()
		if s.proxySub == nil || !s.proxySub.autoRotate {
			return
		}
		if _, err := s.rotateProxySubscriptionLocked(ctx, "http status"); err != nil {
			log.Printf("[egress] 封禁自动切换失败: %v", err)
		}
	}()
}

func (s *Server) proxyPoolSubscriptionAutoRotate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "请求体解析失败"})
		return
	}
	src, err := s.egressSourceStore().GetSubscription(r.Context())
	if err != nil || src == nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "没有订阅源"})
		return
	}
	if err := s.egressSourceStore().UpdateAutoRotate(r.Context(), src.ID, body.Enabled); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	s.proxySubMu.Lock()
	if s.proxySub != nil {
		s.proxySub.autoRotate = body.Enabled
		if body.Enabled {
			_ = s.applyMihomoSelection(r.Context(), s.proxySub, "auto", "")
			s.proxySub.current = "ARTEX-AUTO"
		}
	}
	s.proxySubMu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "auto_rotate": body.Enabled})
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

func (s *Server) proxyPoolSubscriptionRefresh(w http.ResponseWriter, r *http.Request) {
	src, err := s.egressSourceStore().GetSubscription(r.Context())
	if err != nil || src == nil || strings.TrimSpace(src.ConfigURL) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "没有可刷新的订阅源"})
		return
	}
	out, err := s.startProxySubscription(r.Context(), src.ConfigURL)
	if err != nil {
		s.proxySubMu.Lock()
		running := s.proxySub != nil
		s.proxySubMu.Unlock()
		state := "failed"
		if running {
			state = "degraded"
		}
		_ = s.egressSourceStore().UpdateObserved(r.Context(), src.ID, state, fetchErrText(err), src.ManagedProxyID, src.ManagedPort)
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": fetchErrText(err)})
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
