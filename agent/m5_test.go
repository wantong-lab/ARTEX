package agent

import (
	"encoding/json"
	"testing"
	"time"
)

func payloadOf(t *testing.T, m map[string]any) []byte {
	t.Helper()
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}

func TestClassifyIntentBudget(t *testing.T) {
	cases := []struct {
		summary string
		tags    []string
		want    string
		secs    int
		hasNote bool
	}{
		{"侦察目标存活与可达性", nil, "recon", 900, true},
		{"目录扫描与端口枚举", nil, "enum", 1800, true},
		{"弱口令爆破登录口", nil, "brute", 2400, true},
		{"SQL注入利用拿数据", nil, "exploit", 2400, true},
		{"整理本阶段报告", nil, "general", 1800, false},
		{"利用验证", []string{"recon"}, "recon", 900, true}, // chain_tags 优先
	}
	for _, c := range cases {
		m := map[string]any{"summary": c.summary}
		if c.tags != nil {
			m["chain_tags"] = c.tags
		}
		got := ClassifyIntentBudget(payloadOf(t, m))
		if got.Class != c.want || got.Seconds != c.secs {
			t.Fatalf("%q: got %+v want %s/%d", c.summary, got, c.want, c.secs)
		}
		if (got.Note != "") != c.hasNote {
			t.Fatalf("%q: note presence mismatch (%q)", c.summary, got.Note)
		}
	}
	// 空 payload 也要稳定落到 general。
	if got := ClassifyIntentBudget(nil); got.Class != "general" || got.Seconds != IntentBudgetGeneral {
		t.Fatalf("empty payload: %+v", got)
	}
}

func TestFrontierLimiterRejectsWhenFull(t *testing.T) {
	l := NewFrontierLimiter(0) // 默认 8
	if l.Cap() != FrontierMaxWorkers {
		t.Fatalf("default cap=%d", l.Cap())
	}
	for i := 0; i < FrontierMaxWorkers; i++ {
		if !l.TryAcquire() {
			t.Fatalf("acquire %d should succeed", i)
		}
	}
	if l.TryAcquire() {
		t.Fatal("frontier full: must reject new worker")
	}
	if l.InUse() != FrontierMaxWorkers {
		t.Fatalf("inuse=%d", l.InUse())
	}
	l.Release()
	if !l.TryAcquire() {
		t.Fatal("after release acquire should succeed")
	}
}

func TestRelayTableAndIdempotency(t *testing.T) {
	if got := NextRelayRole("recon"); got != "enum" {
		t.Fatalf("recon -> %q", got)
	}
	if got := NextRelayRole("enum"); got != "exploit" {
		t.Fatalf("enum -> %q", got)
	}
	if got := NextRelayRole("exploit"); got != "" {
		t.Fatalf("exploit should terminate relay, got %q", got)
	}
	ledger := NewRelayLedger()
	if !ledger.Claim(1, 10, "recon", "enum") {
		t.Fatal("first relay claim must succeed")
	}
	if ledger.Claim(1, 10, "recon", "enum") {
		t.Fatal("duplicate relay must be refused (idempotent)")
	}
	if ledger.Count() != 1 {
		t.Fatalf("count=%d", ledger.Count())
	}
	if !ledger.Claim(1, 11, "recon", "enum") {
		t.Fatal("different intent should relay once")
	}
}

func TestTaskBusBroadcast(t *testing.T) {
	bus := NewTaskBus()
	ch1, cancel1 := bus.Subscribe(7)
	ch2, cancel2 := bus.Subscribe(7)
	defer cancel2()
	bus.Publish(TaskEvent{TaskID: 7, Kind: "intent_done", Summary: "x"})
	recv := func(ch <-chan TaskEvent) TaskEvent {
		select {
		case ev := <-ch:
			return ev
		case <-time.After(time.Second):
			t.Fatal("event not delivered")
			return TaskEvent{}
		}
	}
	if ev := recv(ch1); ev.Kind != "intent_done" || ev.TaskID != 7 {
		t.Fatalf("ev=%+v", ev)
	}
	if ev := recv(ch2); ev.Summary != "x" {
		t.Fatalf("ev=%+v", ev)
	}
	cancel1()
	cancel1() // 重复取消不 panic
	if bus.Subscribers(7) != 1 {
		t.Fatalf("subscribers=%d", bus.Subscribers(7))
	}
	bus.Publish(TaskEvent{TaskID: 7, Kind: "second"})
	if ev := recv(ch2); ev.Kind != "second" {
		t.Fatalf("ev=%+v", ev)
	}
	// 别的任务收不到。
	if bus.Subscribers(8) != 0 {
		t.Fatal("task 8 should have no subscribers")
	}
}
