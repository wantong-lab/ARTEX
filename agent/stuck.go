package agent

// 停滞检测（M1-03）：worker 循环内按 tool_result 判定「信息梯度为零」——
// 同一输出反复出现、或同一工具反复失败且输出越来越像。纯函数、无 I/O，
// 由 steer 钩子在 PostToolUse 喂入、PreToolUse 注入转向指令。
//
// 动机：模型会凭「感觉快成了」对同一个死胡同连续重试，把意图预算烧光；
// 阈值来自二开版的线上实测（同签名连续 12 次 / 同工具失败 15 次且相似度 0.8）。

import (
	"fmt"
	"hash/fnv"
	"regexp"
	"strings"
)

// StuckConfig 是停滞检测参数。默认值即线上值，测试可缩小阈值。
type StuckConfig struct {
	Window         int     // 滚动窗口：保留最近 N 个观察
	SigPrefix      int     // 归一化输出取前 N 字符进签名
	SameSigTrigger int     // 同签名连续 >= N 次 → 触发
	FailTrigger    int     // 同工具连续失败 >= N 次且输出相似 → 触发
	FailSimilarity float64 // 失败相似度阈值（0~1，bigram Dice）
	Cooldown       int     // 触发后 N 步内不再触发
	MaxFires       int     // 单条意图最多触发次数
}

func DefaultStuckConfig() StuckConfig {
	return StuckConfig{Window: 25, SigPrefix: 200, SameSigTrigger: 12, FailTrigger: 15, FailSimilarity: 0.8, Cooldown: 10, MaxFires: 3}
}

// StuckEvent 是一次停滞判定结果。
type StuckEvent struct {
	Tool   string // 触发时的工具名
	Kind   string // same_signature | same_failure
	Count  int    // 触发时的连续次数
	N      int    // 本意图第几次触发（1 起）
	Sample string // 归一化输出片段（截断），便于人类核对
}

// Message 返回给模型看的转向指令。
func (e *StuckEvent) Message() string {
	reason := "最近多次工具输出几乎同质"
	if e.Kind == "same_failure" {
		reason = "同一工具连续失败且输出高度相似"
	}
	return fmt.Sprintf("【停滞检测·%s】%s（%s 连续 %d 次，本意图第 %d 次）。'信息梯度为零，继续同轴加力只会烧预算：请换一个维度（换注入位置/参数面/接口/主机），或把已确认结论写成 fact 后收尾，交回规划者换方向。参考输出片段：%s", e.Tool, reason, e.Tool, e.Count, e.N, e.Sample)
}

// 归一化规则（顺序敏感）：时间戳 → 路径 → 长 hex → 长随机串 → 数字 → 空白折叠。
// 目的：同一拦截页/同一崩溃输出里的时间戳、请求 id、临时路径、随机 token 不该
// 把「同质」伪装成「不同」。
var (
	stuckReTimestamp = regexp.MustCompile(`\d{4}[-/]\d{1,2}[-/]\d{1,2}([t ]\d{1,2}:\d{2}(:\d{2})?(\.\d+)?(z|[+-]\d{2}:?\d{2})?)?|\d{1,2}:\d{2}:\d{2}(\.\d+)?`)
	stuckRePath      = regexp.MustCompile(`(?:[a-z]:)?[\w.~+-]*(?:[/\\][\w.~+-]+)+/?`)
	stuckReHex       = regexp.MustCompile(`\b[0-9a-f]{8,}\b`)
	stuckReRand      = regexp.MustCompile(`\b[\w+-]{24,}\b`)
	stuckReDigits    = regexp.MustCompile(`\d+`)
	stuckReSpace     = regexp.MustCompile(`\s+`)
)

func normalizeStuckOutput(s string) string {
	s = strings.ToLower(s)
	s = stuckReTimestamp.ReplaceAllString(s, "<ts>")
	s = stuckRePath.ReplaceAllString(s, "<path>")
	s = stuckReHex.ReplaceAllString(s, "<hex>")
	s = stuckReRand.ReplaceAllString(s, "<rand>")
	s = stuckReDigits.ReplaceAllString(s, "<num>")
	s = stuckReSpace.ReplaceAllString(s, " ")
	return strings.TrimSpace(s)
}

func stuckSignature(norm string, prefix int) string {
	if prefix > 0 && len(norm) > prefix {
		norm = norm[:prefix]
	}
	h := fnv.New64a()
	_, _ = h.Write([]byte(norm))
	return fmt.Sprintf("%x", h.Sum64())
}

// bigramDice 计算两个字符串的 bigram Dice 相似度（0~1）。
func bigramDice(a, b string) float64 {
	if a == b {
		return 1
	}
	if len(a) < 2 || len(b) < 2 {
		return 0
	}
	grams := func(s string) map[string]int {
		m := map[string]int{}
		for i := 0; i+1 < len(s); i++ {
			m[s[i:i+2]]++
		}
		return m
	}
	ga, gb := grams(a), grams(b)
	inter := 0
	for g, ca := range ga {
		if cb, ok := gb[g]; ok {
			if ca < cb {
				inter += ca
			} else {
				inter += cb
			}
		}
	}
	return 2 * float64(inter) / float64(len(a)+len(b)-2)
}

// StuckDetector 是单条意图内的停滞检测器（非并发安全，钩子串行调用）。
type StuckDetector struct {
	cfg StuckConfig

	window []string // 最近归一化输出（仅保留 Window 个）

	sameSig    string
	sameStreak int

	failTool       string
	failStreak     int
	failLastNorm   string
	failSimilarSum float64

	cooldown int
	fires    int
}

func NewStuckDetector(cfg StuckConfig) *StuckDetector { return &StuckDetector{cfg: cfg} }

// Observe 喂入一次工具结果；触发时返回事件，否则 nil。
func (d *StuckDetector) Observe(tool string, result []byte, isErr bool) *StuckEvent {
	if d == nil || (d.cfg.SameSigTrigger <= 0 && d.cfg.FailTrigger <= 0) {
		return nil
	}
	if d.cooldown > 0 {
		d.cooldown--
	}
	norm := normalizeStuckOutput(string(result))
	sig := stuckSignature(norm, d.cfg.SigPrefix)
	if sig == d.sameSig {
		d.sameStreak++
	} else {
		d.sameSig, d.sameStreak = sig, 1
	}
	d.window = append(d.window, norm)
	if d.cfg.Window > 0 && len(d.window) > d.cfg.Window {
		d.window = d.window[len(d.window)-d.cfg.Window:]
	}
	if !isErr {
		d.failTool, d.failStreak, d.failLastNorm, d.failSimilarSum = "", 0, "", 0
	} else {
		if tool != d.failTool {
			d.failTool, d.failStreak, d.failLastNorm, d.failSimilarSum = tool, 0, "", 0
		}
		if d.failLastNorm != "" {
			d.failSimilarSum += bigramDice(d.failLastNorm, norm)
		}
		d.failLastNorm = norm
		d.failStreak++
	}
	if d.cooldown > 0 || d.fires >= d.cfg.MaxFires {
		return nil
	}
	kind := ""
	sample := norm
	if len(sample) > 120 {
		sample = sample[:120]
	}
	if d.cfg.SameSigTrigger > 0 && d.sameStreak >= d.cfg.SameSigTrigger {
		kind = "same_signature"
	} else if d.cfg.FailTrigger > 0 && d.failStreak >= d.cfg.FailTrigger {
		var sim float64
		if d.failStreak > 1 {
			sim = d.failSimilarSum / float64(d.failStreak-1)
		}
		if sim >= d.cfg.FailSimilarity {
			kind = "same_failure"
		}
	}
	if kind == "" {
		return nil
	}
	d.fires++
	d.cooldown = d.cfg.Cooldown
	e := &StuckEvent{Tool: tool, Kind: kind, Count: d.sameStreak, N: d.fires, Sample: sample}
	if kind == "same_failure" {
		e.Count = d.failStreak
	}
	d.sameSig, d.sameStreak = "", 0
	d.failTool, d.failStreak, d.failLastNorm, d.failSimilarSum = "", 0, "", 0
	return e
}
