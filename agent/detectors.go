package agent

// DetectorSet 把三个确定性检测器组合成 worker 的一层「转向建议」：
//   - stuck：输出同质 → 换维度
//   - deadsurface：目标/出口不可达 → 换面
//   - repeating：同参数重复调用 → 换参数
// 触发时把消息排队，由 steer 钩子在【下一次工具调用前】拦下并交还模型。

import "fmt"

// DetectorConfig 是三个检测器的开关与参数。
type DetectorConfig struct {
	StuckEnabled    bool
	DeadEnabled     bool
	RepeatEnabled   bool
	Stuck           StuckConfig
	Dead            DeadSurfaceConfig
	RepeatThreshold int
}

func DefaultDetectorConfig() DetectorConfig {
	return DetectorConfig{
		StuckEnabled:    true,
		DeadEnabled:     true,
		RepeatEnabled:   true,
		Stuck:           DefaultStuckConfig(),
		Dead:            DefaultDeadSurfaceConfig(),
		RepeatThreshold: defaultRepeatThreshold,
	}
}

// DetectorConfigFromSettings 从 settings 读开关（键：stuck_detect / dead_surface / repeat_detect）。
// 键缺失或取值无法识别时保持默认开启；只有显式关闭值才关（与现有 settings 语义一致）。
func DetectorConfigFromSettings(get func(key string) (string, bool, error)) DetectorConfig {
	cfg := DefaultDetectorConfig()
	if get == nil {
		return cfg
	}
	if v, ok, err := get("stuck_detect"); err == nil && ok {
		if on, known := parseDetectorSwitch(v); known {
			cfg.StuckEnabled = on
		}
	}
	if v, ok, err := get("dead_surface"); err == nil && ok {
		if on, known := parseDetectorSwitch(v); known {
			cfg.DeadEnabled = on
		}
	}
	if v, ok, err := get("repeat_detect"); err == nil && ok {
		if on, known := parseDetectorSwitch(v); known {
			cfg.RepeatEnabled = on
		}
	}
	return cfg
}

// parseDetectorSwitch 解析开关值；known=false 表示取值无法识别（调用方保持默认）。
func parseDetectorSwitch(v string) (enabled, known bool) {
	switch lowerTrim(v) {
	case "1", "true", "on", "yes", "开":
		return true, true
	case "0", "false", "off", "no", "关":
		return false, true
	}
	return false, false
}

func lowerTrim(s string) string {
	out := make([]rune, 0, len(s))
	for _, r := range s {
		if r == ' ' || r == '\t' || r == '\n' || r == '\r' {
			continue
		}
		if r >= 'A' && r <= 'Z' {
			r = r + ('a' - 'A')
		}
		out = append(out, r)
	}
	return string(out)
}

// DetectorSet 是单条意图内的检测器集合（非并发安全，钩子串行调用）。
type DetectorSet struct {
	stuck *StuckDetector
	dead  *DeadSurfaceDetector
	rep   *RepeatingDetector

	pending []string
}

func NewDetectorSet(cfg DetectorConfig) *DetectorSet {
	s := &DetectorSet{}
	if cfg.StuckEnabled {
		s.stuck = NewStuckDetector(cfg.Stuck)
	}
	if cfg.DeadEnabled {
		s.dead = NewDeadSurfaceDetector(cfg.Dead)
	}
	if cfg.RepeatEnabled {
		s.rep = NewRepeatingDetector(cfg.RepeatThreshold)
	}
	if s.stuck == nil && s.dead == nil && s.rep == nil {
		return nil
	}
	return s
}

// PreCheck 在工具执行前调用：重复调用直接拦截并回放上次结果。
func (s *DetectorSet) PreCheck(tool string, input []byte) (bool, string) {
	if s == nil || s.rep == nil {
		return false, ""
	}
	return s.rep.Check(tool, input)
}

// TakePending 取出待注入的转向消息并清空。
func (s *DetectorSet) TakePending() []string {
	if s == nil || len(s.pending) == 0 {
		return nil
	}
	out := s.pending
	s.pending = nil
	return out
}

// Observe 在工具执行后调用：更新检测器，触发时把消息排队并返回（供日志/活动记账）。
func (s *DetectorSet) Observe(tool string, input, result []byte, isErr bool) []string {
	if s == nil {
		return nil
	}
	if s.rep != nil {
		s.rep.Record(tool, input, result)
	}
	var fired []string
	if s.stuck != nil {
		if ev := s.stuck.Observe(tool, result, isErr); ev != nil {
			fired = append(fired, ev.Message())
		}
	}
	if s.dead != nil {
		if ev := s.dead.Observe(tool, input, result, isErr); ev != nil {
			fired = append(fired, ev.Message())
		}
	}
	s.pending = append(s.pending, fired...)
	return fired
}

var _ = fmt.Sprintf
