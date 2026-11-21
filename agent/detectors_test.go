package agent

import (
	"strings"
	"testing"
)

func TestNormalizeStuckOutput(t *testing.T) {
	a := normalizeStuckOutput("Error at 2026-11-21 10:00:00 from /tmp/abc123/run.sh id=98765")
	b := normalizeStuckOutput("Error at 2026-11-22 11:22:33 from /var/x/y.sh id=12345")
	if a != b {
		t.Fatalf("normalize should erase ts/path/num:\n a=%q\n b=%q", a, b)
	}
	if !strings.Contains(a, "<ts>") || !strings.Contains(a, "<path>") || !strings.Contains(a, "<num>") {
		t.Fatalf("placeholders missing: %q", a)
	}
}

func TestStuckDetectorSameSignature(t *testing.T) {
	d := NewStuckDetector(StuckConfig{Window: 10, SigPrefix: 200, SameSigTrigger: 3, Cooldown: 2, MaxFires: 2})
	var fired *StuckEvent
	for i := 0; i < 3; i++ {
		fired = d.Observe("Bash", []byte("HTTP/1.1 200 OK\nbody=hello"), false)
	}
	if fired == nil || fired.Kind != "same_signature" {
		t.Fatalf("want same_signature event, got %+v", fired)
	}
	// 冷却期内即使继续同质也不应再次触发
	if ev := d.Observe("Bash", []byte("HTTP/1.1 200 OK\nbody=hello"), false); ev != nil {
		t.Fatalf("cooldown must suppress re-fire, got %+v", ev)
	}
}

func TestStuckDetectorSameFailure(t *testing.T) {
	d := NewStuckDetector(StuckConfig{SigPrefix: 200, SameSigTrigger: 0, FailTrigger: 3, FailSimilarity: 0.5, Cooldown: 0, MaxFires: 1})
	var fired *StuckEvent
	for i := 0; i < 3; i++ {
		fired = d.Observe("curl", []byte("curl: (7) Failed to connect to 10.0.0.1 port 80"), true)
	}
	if fired == nil || fired.Kind != "same_failure" {
		t.Fatalf("want same_failure event, got %+v", fired)
	}
}

func TestDeadSurfaceHost(t *testing.T) {
	d := NewDeadSurfaceDetector(DeadSurfaceConfig{HostTrigger: 3, HTTPTrigger: 2, EgressTrigger: 2, Cooldown: 0, MaxFires: 1})
	in := []byte(`{"command":"nmap -p 443 10.9.8.7"}`)
	var fired *DeadSurfaceEvent
	for i := 0; i < 3; i++ {
		fired = d.Observe("Bash", in, []byte("connect: connection refused"), true)
	}
	if fired == nil || fired.Kind != "dead_surface" || fired.Host != "10.9.8.7" {
		t.Fatalf("want dead_surface on 10.9.8.7, got %+v", fired)
	}
}

func TestDeadSurfaceSuccessResets(t *testing.T) {
	d := NewDeadSurfaceDetector(DeadSurfaceConfig{HostTrigger: 3, HTTPTrigger: 2, EgressTrigger: 2, Cooldown: 0, MaxFires: 1})
	in := []byte(`{"command":"curl http://10.9.8.8/"}`)
	for i := 0; i < 2; i++ {
		d.Observe("Bash", in, []byte("connection refused"), true)
	}
	d.Observe("Bash", in, []byte("HTTP/1.1 200 OK"), false) // 成功重置
	if ev := d.Observe("Bash", in, []byte("connection refused"), true); ev != nil {
		t.Fatalf("counter should reset after success, got %+v", ev)
	}
}

func TestDeadSurfaceEgress(t *testing.T) {
	d := NewDeadSurfaceDetector(DeadSurfaceConfig{HostTrigger: 5, HTTPTrigger: 5, EgressTrigger: 2, Cooldown: 0, MaxFires: 1})
	in := []byte(`{"command":"curl -x http://127.0.0.1:7890 https://target.example.com/"}`)
	var fired *DeadSurfaceEvent
	for i := 0; i < 2; i++ {
		fired = d.Observe("Bash", in, []byte("connection refused"), true)
	}
	if fired == nil || fired.Kind != "egress_dead" {
		t.Fatalf("want egress_dead, got %+v", fired)
	}
}

func TestRepeatingDetector(t *testing.T) {
	d := NewRepeatingDetector(3)
	in := []byte(`{"command":"curl http://t/1"}`)
	if block, _ := d.Check("Bash", in); block {
		t.Fatal("first call must not block")
	}
	d.Record("Bash", in, []byte("first result"))
	if block, _ := d.Check("Bash", in); block {
		t.Fatal("second call must not block")
	}
	d.Record("Bash", in, []byte("second result"))
	block, msg := d.Check("Bash", in)
	if !block {
		t.Fatal("third identical call must block")
	}
	if !strings.Contains(msg, "重复调用拦截") || !strings.Contains(msg, "second result") {
		t.Fatalf("block message should replay last result: %q", msg)
	}
	d.Record("Bash", in, []byte("second result"))
	if block, _ := d.Check("Bash", []byte(`{"command":"curl http://t/2"}`)); block {
		t.Fatal("different input must not block")
	}
}

func TestParseDetectorSwitch(t *testing.T) {
	cases := map[string]struct{ enabled, known bool }{
		"1": {true, true}, "true": {true, true}, " ON ": {true, true},
		"0": {false, true}, "off": {false, true}, "false": {false, true},
		"": {false, false}, "maybe": {false, false},
	}
	for in, want := range cases {
		on, known := parseDetectorSwitch(in)
		if on != want.enabled || known != want.known {
			t.Fatalf("parseDetectorSwitch(%q)=(%v,%v), want (%v,%v)", in, on, known, want.enabled, want.known)
		}
	}
}

func TestDetectorConfigFromSettings(t *testing.T) {
	get := func(key string) (string, bool, error) {
		switch key {
		case "stuck_detect":
			return "off", true, nil
		case "repeat_detect":
			return "garbage", true, nil
		}
		return "", false, nil
	}
	cfg := DetectorConfigFromSettings(get)
	if cfg.StuckEnabled {
		t.Fatal("stuck_detect=off should disable")
	}
	if !cfg.DeadEnabled || !cfg.RepeatEnabled {
		t.Fatal("missing/unknown settings must keep defaults on")
	}
}

func TestDetectorSetObserveQueuesSteer(t *testing.T) {
	cfg := DefaultDetectorConfig()
	cfg.Stuck = StuckConfig{Window: 10, SigPrefix: 200, SameSigTrigger: 2, Cooldown: 0, MaxFires: 1}
	cfg.Dead = DeadSurfaceConfig{HostTrigger: 99, HTTPTrigger: 99, EgressTrigger: 99, Cooldown: 0, MaxFires: 1}
	set := NewDetectorSet(cfg)
	in := []byte(`{"command":"echo hi"}`)
	set.Observe("Bash", in, []byte("same output"), false)
	fired := set.Observe("Bash", in, []byte("same output"), false)
	if len(fired) != 1 {
		t.Fatalf("want 1 steer message, got %v", fired)
	}
	pending := set.TakePending()
	if len(pending) != 1 || set.TakePending() != nil {
		t.Fatalf("TakePending should drain once: %v", pending)
	}
}
