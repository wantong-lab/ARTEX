package agent

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestUAPoolFromSetting(t *testing.T) {
	if got := UAPoolFromSetting(`["A"," B "]`); len(got) != 2 || got[0] != "A" || got[1] != "B" {
		t.Fatalf("pool=%v", got)
	}
	if got := UAPoolFromSetting("not-json"); len(got) != 2 {
		t.Fatalf("invalid setting should fall back to default, got %v", got)
	}
	if got := UAPoolFromSetting(`[]`); len(got) != 2 {
		t.Fatalf("empty array should fall back to default, got %v", got)
	}
}

func TestPickUAStable(t *testing.T) {
	pool := []string{"UA-A", "UA-B", "UA-C"}
	first := PickUA(pool, 42)
	for i := 0; i < 5; i++ {
		if got := PickUA(pool, 42); got != first {
			t.Fatalf("same task must keep UA: %q vs %q", got, first)
		}
	}
	if first != "UA-A" && first != "UA-B" && first != "UA-C" {
		t.Fatalf("UA not from pool: %q", first)
	}
}

func TestNudgeReconThresholds(t *testing.T) {
	n := NewNudgeState(NudgeConfig{ReconEnabled: true, ReconFirstAt: 3, ReconInterval: 4, ReconMax: 2})
	fire := 0
	for i := 0; i < 7; i++ {
		if strings.Contains(n.OnBash("curl -s http://x/1"), "批量工具") {
			fire++
		}
	}
	if fire != 2 {
		t.Fatalf("want 2 recon nudges (third and seventh call), got %d", fire)
	}
}

func TestNudgeSilentAfterBatchTool(t *testing.T) {
	n := NewNudgeState(NudgeConfig{ReconEnabled: true, ReconFirstAt: 1, ReconInterval: 1, ReconMax: 5})
	n.OnBash("nuclei -u http://x") // 用过批量工具 → 侦查提醒永久沉默
	for i := 0; i < 5; i++ {
		if got := n.OnBash("curl -s http://x/1"); got != "" {
			t.Fatalf("recon nudge must stay silent after batch tool, got %q", got)
		}
	}
}

func TestNudgeArsenal(t *testing.T) {
	n := NewNudgeState(NudgeConfig{ArsenalEnabled: true, ArsenalMax: 2})
	cmd := "cat > shell.php <<EOF\n<?php eval($_POST[1]); ?>\nEOF"
	if got := n.OnBash(cmd); !strings.Contains(got, "webshell") {
		t.Fatalf("want arsenal nudge, got %q", got)
	}
	n.OnBash(cmd)
	if got := n.OnBash(cmd); got != "" {
		t.Fatalf("arsenal nudge must stop at max, got %q", got)
	}
}

func TestPrependBashStderr(t *testing.T) {
	out, ok := PrependBashStderr([]byte(`{"command":"id"}`), "提示文本")
	if !ok {
		t.Fatal("valid bash input should be modified")
	}
	var payload struct {
		Command string `json:"command"`
	}
	if err := json.Unmarshal(out, &payload); err != nil {
		t.Fatalf("modified input is not valid JSON: %v", err)
	}
	if !strings.Contains(payload.Command, ">&2") || !strings.Contains(payload.Command, "id") {
		t.Fatalf("command=%q", payload.Command)
	}
	if _, ok := PrependBashStderr([]byte("not-json"), "x"); ok {
		t.Fatal("invalid JSON must not be modified")
	}
}

func TestNudgeConfigFromSettings(t *testing.T) {
	get := func(key string) (string, bool, error) {
		if key == "recon_nudge" {
			return "off", true, nil
		}
		return "", false, nil
	}
	cfg := NudgeConfigFromSettings(get)
	if cfg.ReconEnabled {
		t.Fatal("recon_nudge=off should disable")
	}
	if !cfg.ArsenalEnabled {
		t.Fatal("missing setting must keep default on")
	}
}
