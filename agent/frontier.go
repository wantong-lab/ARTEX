package agent

// FrontierLimiter 限制同一任务的 frontier 并发（M5-01）：默认上限 8，满了就拒派
// 新 worker，而不是把意图领走后排队占住 frontier 位置。

// FrontierMaxWorkers 是默认上限（ROADMAP M5-01：frontier 上限 8）。
const FrontierMaxWorkers = 8

type FrontierLimiter struct {
	sem chan struct{}
}

func NewFrontierLimiter(max int) *FrontierLimiter {
	if max <= 0 {
		max = FrontierMaxWorkers
	}
	return &FrontierLimiter{sem: make(chan struct{}, max)}
}

// TryAcquire 尝试占用一个并发位；满时返回 false（拒派）。
func (l *FrontierLimiter) TryAcquire() bool {
	if l == nil {
		return true
	}
	select {
	case l.sem <- struct{}{}:
		return true
	default:
		return false
	}
}

func (l *FrontierLimiter) Release() {
	if l == nil {
		return
	}
	select {
	case <-l.sem:
	default:
	}
}

func (l *FrontierLimiter) InUse() int {
	if l == nil {
		return 0
	}
	return len(l.sem)
}

func (l *FrontierLimiter) Cap() int {
	if l == nil {
		return 0
	}
	return cap(l.sem)
}
