package guard

// RoE 范围执行（M2-04）：按 settings 模式对工具调用做范围判定。
// 模式：off（不判）/ warn（只记告警，不拦）/ strict（出界即拦）。
// scope 由调用方按任务注入（读 task_scope），每次调用重新取，审批通过后下一次工具
// 调用即生效。

import (
	"context"
	"encoding/json"
	"strings"
)

const (
	ModeOff    = "off"
	ModeWarn   = "warn"
	ModeStrict = "strict"
)

// NormalizeMode 归一化模式值；无法识别返回 ""（调用方保持默认）。
func NormalizeMode(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case ModeOff:
		return ModeOff
	case ModeWarn:
		return ModeWarn
	case ModeStrict:
		return ModeStrict
	}
	return ""
}

// ScopeProvider 返回当前任务的 scope 匹配器（无配置返回 nil）。
type ScopeProvider func(ctx context.Context) (*ScopeMatcher, error)

type RoE struct {
	mode  string
	scope ScopeProvider
}

// NewRoE 构造范围执行器；mode 为空按 warn。
func NewRoE(mode string, scope ScopeProvider) *RoE {
	if NormalizeMode(mode) == "" {
		mode = ModeWarn
	}
	return &RoE{mode: NormalizeMode(mode), scope: scope}
}

func (r *RoE) Mode() string {
	if r == nil {
		return ModeOff
	}
	return r.mode
}

// Evaluate 判定一次工具调用：block=true 表示 strict 下确认出界；note 非空表示
// 需要记一条审计（warn 告警或 strict 拦截原因）。
func (r *RoE) Evaluate(ctx context.Context, tool string, input json.RawMessage) (block bool, note string) {
	if r == nil || r.mode == ModeOff {
		return false, ""
	}
	targets := ExtractTargets(tool, input)
	if len(targets) == 0 {
		return false, ""
	}
	if r.scope == nil {
		return false, ""
	}
	m, err := r.scope(ctx)
	if err != nil || m == nil {
		return false, ""
	}
	state, out := m.MatchTargets(targets)
	if state != OutOfScope {
		return false, ""
	}
	msg := "目标 " + out.Host + " 不在本任务授权范围内（scope=" + state.String() + "）"
	if r.mode == ModeStrict {
		return true, msg
	}
	return false, "出界告警：" + msg
}
