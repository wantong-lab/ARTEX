package agent

// 重复调用拦截（M1-03）：连续多次用完全相同的参数调同一个工具时拦截，
// 并把上一次输出回放给模型——同一请求重复发起既浪费预算，也常是同质重试的信号。

import (
	"fmt"
	"strings"
)

const defaultRepeatThreshold = 3

type RepeatingDetector struct {
	threshold  int
	lastKey    string
	streak     int
	lastResult string
}

func NewRepeatingDetector(threshold int) *RepeatingDetector {
	if threshold <= 1 {
		threshold = defaultRepeatThreshold
	}
	return &RepeatingDetector{threshold: threshold}
}

func repeatKey(tool string, input []byte) string {
	return tool + "\x00" + strings.TrimSpace(string(input))
}

// Check 在工具执行前调用：已连续相同调用达到阈值时返回拦截与回放文本。
func (d *RepeatingDetector) Check(tool string, input []byte) (bool, string) {
	if d == nil || d.threshold <= 1 {
		return false, ""
	}
	if repeatKey(tool, input) != d.lastKey || d.streak < d.threshold-1 {
		return false, ""
	}
	prev := d.lastResult
	if len(prev) > 600 {
		prev = prev[:600] + "…"
	}
	return true, fmt.Sprintf("【重复调用拦截】%s 已连续 %d 次使用完全相同的参数，本次调用未执行。上一次结果：%s\n请基于该结果换参数/换路径，而不是原样重试。", tool, d.streak, prev)
}

// Record 在工具执行后调用（或拦截后调用），维护连续计数与上一份结果。
func (d *RepeatingDetector) Record(tool string, input, result []byte) {
	if d == nil {
		return
	}
	key := repeatKey(tool, input)
	if key == d.lastKey {
		d.streak++
	} else {
		d.lastKey, d.streak = key, 1
	}
	d.lastResult = string(result)
}
