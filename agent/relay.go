package agent

// 角色接力（M5-01）：意图完成后按类别给出下一步角色建议，并用幂等账本防止同一
// 条意图被重复接力（worker 重跑/事件重放时不会重复派发下一条意图）。

import (
	"fmt"
	"sync"
)

// relayNext 是接力表：当前类别 -> 建议的下一角色类别（空 = 到此为止）。
var relayNext = map[string]string{
	"recon": "enum",
	"enum":  "exploit",
	"brute": "exploit",
}

// NextRelayRole 返回接力表里的下一角色；没有则返回空串。
func NextRelayRole(class string) string {
	return relayNext[class]
}

// RelayLedger 记录已发生的接力（key = taskID/intentID/from/to）。
type RelayLedger struct {
	mu   sync.Mutex
	seen map[string]struct{}
}

func NewRelayLedger() *RelayLedger { return &RelayLedger{seen: map[string]struct{}{}} }

// Claim 原子占用一次接力：首次返回 true，重复返回 false（幂等）。
func (l *RelayLedger) Claim(taskID, intentID int64, fromRole, toRole string) bool {
	if l == nil {
		return true
	}
	key := fmt.Sprintf("%d/%d/%s->%s", taskID, intentID, fromRole, toRole)
	l.mu.Lock()
	defer l.mu.Unlock()
	if _, ok := l.seen[key]; ok {
		return false
	}
	l.seen[key] = struct{}{}
	return true
}

// Count 返回已记录接力数（测试/审计用）。
func (l *RelayLedger) Count() int {
	if l == nil {
		return 0
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.seen)
}
