package agent

// 同任务广播总线（M5-01）：进程内发布/订阅，用于把意图终态、接力建议等信息广播
// 给同任务订阅者（如 UI 观察器/编排器）。慢订阅者不阻塞发布方：缓冲满时丢弃。

import (
	"sync"
	"time"
)

// TaskEvent 是一条广播事件。
type TaskEvent struct {
	TaskID  int64     `json:"task_id"`
	Kind    string    `json:"kind"`
	Summary string    `json:"summary"`
	Detail  string    `json:"detail,omitempty"`
	At      time.Time `json:"at"`
}

const taskBusBuf = 64

// TaskBus 是按任务分组的进程内广播总线。
type TaskBus struct {
	mu     sync.Mutex
	nextID int
	subs   map[int64]map[int]chan TaskEvent
}

func NewTaskBus() *TaskBus {
	return &TaskBus{subs: map[int64]map[int]chan TaskEvent{}}
}

// Subscribe 订阅某任务的事件；返回的 cancel 用于退订（可重复调用）。
func (b *TaskBus) Subscribe(taskID int64) (<-chan TaskEvent, func()) {
	ch := make(chan TaskEvent, taskBusBuf)
	if b == nil {
		return ch, func() {}
	}
	b.mu.Lock()
	b.nextID++
	id := b.nextID
	if b.subs[taskID] == nil {
		b.subs[taskID] = map[int]chan TaskEvent{}
	}
	b.subs[taskID][id] = ch
	b.mu.Unlock()
	var once sync.Once
	cancel := func() {
		once.Do(func() {
			b.mu.Lock()
			if m, ok := b.subs[taskID]; ok {
				delete(m, id)
				if len(m) == 0 {
					delete(b.subs, taskID)
				}
			}
			b.mu.Unlock()
			close(ch)
		})
	}
	return ch, cancel
}

// Publish 非阻塞广播：订阅者缓冲满时丢弃该条（不让发布方卡住）。
func (b *TaskBus) Publish(ev TaskEvent) {
	if b == nil {
		return
	}
	if ev.At.IsZero() {
		ev.At = time.Now()
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, ch := range b.subs[ev.TaskID] {
		select {
		case ch <- ev:
		default:
		}
	}
}

// Subscribers 返回某任务的订阅者数量。
func (b *TaskBus) Subscribers(taskID int64) int {
	if b == nil {
		return 0
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.subs[taskID])
}
