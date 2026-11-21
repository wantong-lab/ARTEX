package agent

// 死面熔断（M1-03）：按【目标 host】统计传输层连续失败、5xx、出口代理失败，
// 撞线经 steer 钩子把「别再对这个面死磕」的指令还给模型。
// 与停滞检测互补：那层看「输出像不像」，这层看「这个 host 还通不通」——
// 连接失败五花八门、措辞各异，同质检测抓不到。

import (
	"fmt"
	"net"
	"regexp"
	"strings"
)

type DeadSurfaceConfig struct {
	HostTrigger   int // 同一 host 连续传输层失败 ≥ N → 熔断
	HTTPTrigger   int // 同一 host 连续 5xx ≥ N → 熔断
	EgressTrigger int // 出口代理（本地回环代理口）连续失败 ≥ N → 熔断
	Cooldown      int // 触发后 N 步冷却
	MaxFires      int // 单条意图最多触发次数
}

func DefaultDeadSurfaceConfig() DeadSurfaceConfig {
	return DeadSurfaceConfig{HostTrigger: 5, HTTPTrigger: 8, EgressTrigger: 3, Cooldown: 8, MaxFires: 3}
}

type DeadSurfaceEvent struct {
	Tool  string
	Kind  string // dead_surface | http_dead | egress_dead
	Host  string
	Count int
	N     int
}

func (e *DeadSurfaceEvent) Message() string {
	switch e.Kind {
	case "egress_dead":
		return fmt.Sprintf("【死面熔断·出口故障】本地出口代理 %s 连续失败 %d 次（本意图第 %d 次）。当前一切出网请求都在白烧：先确认代理/隧道是否存活，再继续；若短时间无法恢复，记录 fact 并收尾交回规划者。", e.Host, e.Count, e.N)
	case "http_dead":
		return fmt.Sprintf("【死面熔断·服务异常】对 %s 的连续 5xx 已达 %d 次（本意图第 %d 次）。该面大概率不可用（服务降级/边缘封禁），停止继续打；换资产/换路径，或把观察写成 fact 交回规划者。", e.Host, e.Count, e.N)
	default:
		return fmt.Sprintf("【死面熔断·目标不可达】对 %s 的连续传输层失败已达 %d 次（本意图第 %d 次）。该面已不可达：停止重试，换目标/换路径；不要对同一 host 继续消耗预算。", e.Host, e.Count, e.N)
	}
}

var (
	deadTransportRe = regexp.MustCompile(`(?i)(timed?\s*out|connection\s+(timed?\s*out|refused)|couldn.?.?t\s+connect|failed\s+to\s+connect|no\s+route\s+to\s+host|network\s+is\s+unreachable|could\s+not\s+resolve\s+host|name\s+or\s+service\s+not\s+known|exit\s+(code\s+)?(5|6|7|28)\b)`)
	deadHTTP5xxRe   = regexp.MustCompile(`(?i)(HTTP/[0-9.]+\s+5\d\d\b|\b5\d\d\s+(internal\s+server\s+error|bad\s+gateway|service\s+unavailable|gateway\s+time\s*out))`)
	deadHostTokenRe = regexp.MustCompile(`(?i)\b((?:[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]{2,63}|\d{1,3}(?:\.\d{1,3}){3})(?::\d{1,5})?\b`)
)

// extractTargetHost 从工具输入里找第一个非本地 host（命令/URL 原文都可以）。
func extractTargetHost(input []byte) string {
	for _, m := range deadHostTokenRe.FindAllStringSubmatch(string(input), -1) {
		h := strings.ToLower(strings.Trim(m[1], "."))
		if isLocalHost(h) {
			continue
		}
		return h
	}
	return ""
}

// firstHostToken 返回输入里第一个 host:port 的 host（含本地地址），用于识别出口代理。
func firstHostToken(input []byte) string {
	m := deadHostTokenRe.FindStringSubmatch(string(input))
	if len(m) < 2 {
		return ""
	}
	return strings.ToLower(strings.Trim(m[1], "."))
}

func isLocalHost(h string) bool {
	switch strings.ToLower(h) {
	case "localhost", "127.0.0.1", "::1", "0.0.0.0":
		return true
	}
	if ip := net.ParseIP(h); ip != nil {
		return ip.IsLoopback()
	}
	return false
}

// DeadSurfaceDetector 是单条意图内的死面熔断器（非并发安全，钩子串行调用）。
type DeadSurfaceDetector struct {
	cfg DeadSurfaceConfig

	hostFail map[string]int
	host5xx  map[string]int
	egress   int

	cooldown int
	fires    int
}

func NewDeadSurfaceDetector(cfg DeadSurfaceConfig) *DeadSurfaceDetector {
	return &DeadSurfaceDetector{cfg: cfg, hostFail: map[string]int{}, host5xx: map[string]int{}}
}

func (d *DeadSurfaceDetector) Observe(tool string, input, result []byte, isErr bool) *DeadSurfaceEvent {
	if d == nil || d.cfg.HostTrigger <= 0 {
		return nil
	}
	if d.cooldown > 0 {
		d.cooldown--
	}
	text := string(result)
	rawHost := firstHostToken(input)
	local := rawHost != "" && isLocalHost(rawHost)
	host := rawHost
	if !local {
		host = extractTargetHost(input)
	}
	transportFail := isErr || deadTransportRe.MatchString(text)
	http5xx := deadHTTP5xxRe.MatchString(text)

	// 成功即重置该方向的连续计数（避免历史失败影响后续判断）。
	if !transportFail && !http5xx {
		if host != "" {
			d.hostFail[host] = 0
			d.host5xx[host] = 0
		}
		d.egress = 0
		return nil
	}

	kind, count, target := "", 0, host
	if transportFail {
		if local {
			d.egress++
			target = host
			if d.cfg.EgressTrigger > 0 && d.egress >= d.cfg.EgressTrigger {
				kind, count = "egress_dead", d.egress
			}
		} else if host != "" {
			d.hostFail[host]++
			if d.cfg.HostTrigger > 0 && d.hostFail[host] >= d.cfg.HostTrigger {
				kind, count = "dead_surface", d.hostFail[host]
			}
		}
	} else if http5xx && host != "" {
		d.host5xx[host]++
		if d.cfg.HTTPTrigger > 0 && d.host5xx[host] >= d.cfg.HTTPTrigger {
			kind, count = "http_dead", d.host5xx[host]
		}
	}
	if kind == "" || d.cooldown > 0 || d.fires >= d.cfg.MaxFires {
		return nil
	}
	d.fires++
	d.cooldown = d.cfg.Cooldown
	if kind == "egress_dead" {
		d.egress = 0
	} else {
		d.hostFail[host] = 0
		d.host5xx[host] = 0
	}
	return &DeadSurfaceEvent{Tool: tool, Kind: kind, Host: target, Count: count, N: d.fires}
}
