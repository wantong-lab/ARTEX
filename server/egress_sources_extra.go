package server

import (
	"context"
	"crypto/rand"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/Autumn-27/artex/db"
	"github.com/Autumn-27/artex/proxypool"
	"gopkg.in/yaml.v3"
)

func shareLinksToClashYAML(raw string) (string, error) {
	nodes, _, err := proxypool.ParseShareLinks(raw)
	if err != nil {
		return "", err
	}
	b, err := yaml.Marshal(map[string]any{"proxies": nodes})
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func (s *Server) startLinksProxySubscription(ctx context.Context, links string) (map[string]any, error) {
	cfg, err := shareLinksToClashYAML(links)
	if err != nil {
		return nil, err
	}
	return s.startProxySubscriptionLoaded(ctx, "clash-sub", "links", links, func(context.Context, *db.EgressSource) (string, error) {
		return cfg, nil
	})
}

type gatewaySpec struct {
	raw     string
	scheme  string
	host    string
	port    int
	user    string
	pass    string
	dynamic bool
	region  string
}

func parseGatewaySpec(raw string) (*gatewaySpec, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return nil, fmt.Errorf("网关地址非法: %w", err)
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "socks5" && scheme != "http" && scheme != "https" {
		return nil, fmt.Errorf("不支持的网关协议 %q", scheme)
	}
	host := u.Hostname()
	if host == "" {
		return nil, fmt.Errorf("网关缺少 host")
	}
	port := 0
	if ps := u.Port(); ps != "" {
		port, err = strconv.Atoi(ps)
		if err != nil || port <= 0 || port > 65535 {
			return nil, fmt.Errorf("网关端口非法")
		}
	}
	if port == 0 {
		switch scheme {
		case "socks5":
			port = 1080
		case "https":
			port = 443
		default:
			port = 8080
		}
	}
	g := &gatewaySpec{raw: raw, scheme: scheme, host: host, port: port}
	if u.User != nil {
		g.user = u.User.Username()
		g.pass, _ = u.User.Password()
	}
	g.dynamic = strings.Contains(g.user, "region") || strings.Contains(g.user, "sid") || strings.Contains(g.user, "rand")
	if i := strings.Index(g.user, "region-"); i >= 0 {
		rest := g.user[i+len("region-"):]
		if j := strings.Index(rest, "-"); j >= 0 {
			g.region = rest[:j]
		} else {
			g.region = rest
		}
	}
	return g, nil
}

func gatewaySID() string {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "00000000"
	}
	return fmt.Sprintf("%08x", b)
}

func gatewayUser(g *gatewaySpec, sid string) string {
	if !g.dynamic || sid == "" {
		return g.user
	}
	if i := strings.Index(g.user, "sid-"); i >= 0 {
		rest := g.user[i+4:]
		if j := strings.Index(rest, "-"); j >= 0 {
			return g.user[:i+4] + sid + rest[j:]
		}
		return g.user[:i+4] + sid
	}
	if strings.HasSuffix(g.user, "-") {
		return g.user + "sid-" + sid
	}
	return g.user + "-sid-" + sid
}

func gatewayToClashYAML(raw string, count int) (string, error) {
	g, err := parseGatewaySpec(raw)
	if err != nil {
		return "", err
	}
	if count <= 0 || count > 20 {
		count = 5
	}
	if !g.dynamic {
		count = 1
	}
	nodes := make([]map[string]any, 0, count)
	for i := 0; i < count; i++ {
		sid := gatewaySID()
		user := gatewayUser(g, sid)
		name := fmt.Sprintf("gw-%s-%d", g.host, g.port)
		if g.dynamic {
			name = fmt.Sprintf("gw-%s-%s", g.region, sid)
			if g.region == "" {
				name = "gw-" + sid
			}
		}
		n := map[string]any{"name": name, "type": g.scheme, "server": g.host, "port": g.port}
		if user != "" {
			n["username"] = user
			if g.pass != "" {
				n["password"] = g.pass
			}
		}
		nodes = append(nodes, n)
	}
	b, err := yaml.Marshal(map[string]any{"proxies": nodes})
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func (s *Server) startGatewayProxySubscription(ctx context.Context, raw string, count int) (map[string]any, error) {
	cfg, err := gatewayToClashYAML(raw, count)
	if err != nil {
		return nil, err
	}
	st, err := s.startProxySubscriptionLoaded(ctx, "clash-sub", "gateway", raw, func(context.Context, *db.EgressSource) (string, error) {
		return cfg, nil
	})
	if err != nil {
		return nil, err
	}
	if src, gerr := s.egressSourceStore().GetSubscription(ctx); gerr == nil && src != nil {
		_ = s.egressSourceStore().UpdateGatewayCount(ctx, src.ID, count)
	}
	return st, nil
}

func (s *Server) startProxySubscriptionSource(ctx context.Context, src *db.EgressSource) (map[string]any, error) {
	if src == nil {
		return nil, fmt.Errorf("没有可用来源")
	}
	switch src.Kind {
	case "links":
		cfg, err := shareLinksToClashYAML(src.ConfigURL)
		if err != nil {
			return nil, err
		}
		return s.startProxySubscriptionLoaded(ctx, src.Name, "links", src.ConfigURL, func(context.Context, *db.EgressSource) (string, error) {
			return cfg, nil
		})
	case "gateway":
		cfg, err := gatewayToClashYAML(src.ConfigURL, src.GatewayCount)
		if err != nil {
			return nil, err
		}
		return s.startProxySubscriptionLoaded(ctx, src.Name, "gateway", src.ConfigURL, func(context.Context, *db.EgressSource) (string, error) {
			return cfg, nil
		})
	default:
		return s.startProxySubscription(ctx, src.ConfigURL)
	}
}
