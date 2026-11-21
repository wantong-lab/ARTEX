package server

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestSettingHelpersDefaults(t *testing.T) {
	s := &Server{}
	if got := s.settingRaw("missing", "def"); got != "def" {
		t.Fatalf("settingRaw=%q", got)
	}
	if !s.settingBool("missing", true) || s.settingBool("missing", false) {
		t.Fatal("settingBool default mismatch")
	}
	if got := s.settingInt("missing", 24, 1, 168); got != 24 {
		t.Fatalf("settingInt=%d", got)
	}
	if got := s.settingHours("missing", 24); got != 24*time.Hour {
		t.Fatalf("settingHours=%v", got)
	}
	if got := s.settingMinutes("missing", 30); got != 30*time.Minute {
		t.Fatalf("settingMinutes=%v", got)
	}
}

func TestMaintainerTickRunsDueJobs(t *testing.T) {
	base := time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC)
	var mu sync.Mutex
	runs := 0
	ch := make(chan struct{}, 4)
	j := &maintenanceJob{
		name:     "t",
		enabled:  func() bool { return true },
		interval: func() time.Duration { return 5 * time.Minute },
		run: func(ctx context.Context) (string, error) {
			mu.Lock()
			runs++
			mu.Unlock()
			ch <- struct{}{}
			return "ok", nil
		},
	}
	m := &maintainer{now: func() time.Time { return base }, tick: time.Minute, jobs: []*maintenanceJob{j}}
	m.tickOnce(base) // 首次仅排期
	select {
	case <-ch:
		t.Fatal("must not run on scheduling tick")
	default:
	}
	m.tickOnce(base.Add(4 * time.Minute))
	select {
	case <-ch:
		t.Fatal("must not run before interval")
	default:
	}
	m.tickOnce(base.Add(5 * time.Minute))
	select {
	case <-ch:
	case <-time.After(2 * time.Second):
		t.Fatal("due job did not run")
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := runs
		mu.Unlock()
		if n == 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	mu.Lock()
	defer mu.Unlock()
	if runs != 1 {
		t.Fatalf("runs=%d", runs)
	}
	if j.next.IsZero() || !j.next.After(base.Add(5*time.Minute)) {
		t.Fatalf("next=%v", j.next)
	}
}

func TestMaintainerSkipsDisabled(t *testing.T) {
	base := time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC)
	ch := make(chan struct{}, 1)
	m := &maintainer{now: func() time.Time { return base }, tick: time.Minute, jobs: []*maintenanceJob{{
		name:     "off",
		enabled:  func() bool { return false },
		interval: func() time.Duration { return time.Minute },
		run: func(context.Context) (string, error) {
			ch <- struct{}{}
			return "", nil
		},
	}}}
	m.tickOnce(base)
	m.tickOnce(base.Add(time.Hour))
	select {
	case <-ch:
		t.Fatal("disabled job must not run")
	default:
	}
	if st := m.Status(); len(st) != 1 || st[0]["enabled"] != false {
		t.Fatalf("status=%v", st)
	}
}

func TestMaintainerRunNow(t *testing.T) {
	m := &maintainer{now: time.Now, tick: time.Minute, jobs: []*maintenanceJob{{
		name:     "manual",
		enabled:  func() bool { return false },
		interval: func() time.Duration { return time.Hour },
		run:      func(context.Context) (string, error) { return "done", nil },
	}}}
	if res, err := m.RunNow(context.Background(), "missing"); err == nil || res != "" {
		t.Fatalf("unknown job must error, got %q %v", res, err)
	}
	res, err := m.RunNow(context.Background(), "manual")
	if err != nil || res != "done" {
		t.Fatalf("RunNow=%q %v", res, err)
	}
	st := m.Status()
	if len(st) != 1 || st[0]["last_result"] != "done" {
		t.Fatalf("status=%v", st)
	}
}
