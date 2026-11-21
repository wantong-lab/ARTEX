package server

// 意图终态沉淀（M3-01）：完成的意图提炼「打法」，失败/耗尽的提炼「负面清单」，
// 经脱敏后写入语义记忆（memory.Add 异步索引，embedder 不可达时只存不向量化）。
// 全部 best-effort：memory 未启用、DB 不可用、内容为空都静默跳过，不阻塞主流程。

import (
	"encoding/json"
	"regexp"
	"strings"

	"github.com/Autumn-27/artex/agent"
	pgdb "github.com/Autumn-27/artex/db"
)

var (
	reReflectURL  = regexp.MustCompile(`(?i)\bhttps?://[^\s\x27"<>]+`)
	reReflectIP   = regexp.MustCompile(`\b(?:\d{1,3}\.){3}\d{1,3}\b`)
	reReflectHost = regexp.MustCompile(`(?i)\b(?:[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]{2,24}\b`)
)

// anonymizeContent 脱敏：具体 IP/URL/域名替换为占位符，保留可复用的打法结构。
func anonymizeContent(s string) string {
	s = reReflectURL.ReplaceAllString(s, "<url>")
	s = reReflectIP.ReplaceAllString(s, "<ip>")
	s = reReflectHost.ReplaceAllString(s, "<domain>")
	return strings.TrimSpace(s)
}

// intentSummary 取意图节点 payload 里的 summary。
func intentSummary(n *pgdb.Node) string {
	if n == nil || len(n.Payload) == 0 {
		return ""
	}
	var p struct {
		Summary string `json:"summary"`
	}
	if json.Unmarshal(n.Payload, &p) != nil {
		return ""
	}
	return strings.TrimSpace(p.Summary)
}

// ReflectIntent 把一条意图终态沉淀到语义记忆。
func (m *Manager) ReflectIntent(taskID int64, intent *pgdb.Node, state string, wrote agent.WriteCounts) {
	if m == nil || intent == nil {
		return
	}
	summary := intentSummary(intent)
	if summary == "" {
		return
	}
	var kind, label string
	switch state {
	case "done":
		if wrote.Facts <= 0 && wrote.Assets <= 0 {
			return // 空跑完成没有打法可沉淀
		}
		kind, label = "technique", "打法"
	case "blocked", "exhausted":
		kind, label = "negative", "负面清单"
	default:
		return
	}
	mem := m.Memory()
	if mem == nil || !mem.Enabled() {
		return
	}
	mem.Add(kind, "intent:"+itox(intent.ID), taskID, anonymizeContent(label+"："+summary+"；结论："+state))
}

// itox 是 int64→string 的小助手（避免 server 包引入额外依赖面）。
func itox(v int64) string {
	if v == 0 {
		return "0"
	}
	neg := v < 0
	if neg {
		v = -v
	}
	var buf [20]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
