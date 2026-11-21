package guard

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestExtractTargetsFromCommand(t *testing.T) {
	in, _ := json.Marshal(map[string]any{
		"command": "curl -s https://api.example.com:8443/x; nmap -p443 10.1.2.3; ping host.internal",
	})
	got := map[string]bool{}
	for _, x := range ExtractTargets("Bash", in) {
		got[x.Host] = true
	}
	for _, want := range []string{"api.example.com", "10.1.2.3", "host.internal"} {
		if !got[want] {
			t.Fatalf("missing target %q in %v", want, got)
		}
	}
}

func TestExtractTargetsSkipsFileNames(t *testing.T) {
	in, _ := json.Marshal(map[string]any{
		"command": "cat > shell.php <<EOF\n<?php eval($_POST[1]); ?>\nEOF",
	})
	for _, x := range ExtractTargets("Bash", in) {
		if x.Host == "shell.php" {
			t.Fatal("file name must not be treated as a network target")
		}
	}
}

func TestScopeMatcherThreeStates(t *testing.T) {
	m := NewScopeMatcher([]ScopeRule{
		{Kind: "root_domain", Value: "example.com"},
		{Kind: "cidr", Value: "10.1.2.0/24"},
		{Kind: "ip", Value: "203.0.113.9"},
	})
	cases := map[string]ScopeState{
		"api.example.com":  InScope,
		"EXAMPLE.com":      InScope,
		"10.1.2.99":        InScope,
		"203.0.113.9":      InScope,
		"evil.example.net": OutOfScope,
		"10.1.3.1":         OutOfScope,
		"127.0.0.1":        InScope,
		"localhost":        InScope,
	}
	for host, want := range cases {
		if got := m.Match(host); got != want {
			t.Fatalf("Match(%q)=%v, want %v", host, got, want)
		}
	}
	if got := NewScopeMatcher(nil).Match("api.example.com"); got != Unknown {
		t.Fatalf("empty scope must be Unknown, got %v", got)
	}
	var nilM *ScopeMatcher
	if got := nilM.Match("api.example.com"); got != Unknown {
		t.Fatalf("nil matcher must be Unknown, got %v", got)
	}
}

func TestScopeMatcherTargets(t *testing.T) {
	m := NewScopeMatcher([]ScopeRule{{Kind: "cidr", Value: "10.1.2.0/24"}})
	state, out := m.MatchTargets([]Target{{Host: "10.1.2.5"}, {Host: "10.9.9.9"}})
	if state != OutOfScope || out.Host != "10.9.9.9" {
		t.Fatalf("state=%v out=%+v", state, out)
	}
	if state, _ := m.MatchTargets([]Target{{Host: "10.1.2.5"}}); state != InScope {
		t.Fatalf("all-in targets must be InScope, got %v", state)
	}
}

func staticScope(rules ...ScopeRule) ScopeProvider {
	return func(context.Context) (*ScopeMatcher, error) { return NewScopeMatcher(rules), nil }
}

func TestRoEModes(t *testing.T) {
	in, _ := json.Marshal(map[string]any{"command": "curl http://10.9.9.9/"})
	scope := staticScope(ScopeRule{Kind: "cidr", Value: "10.1.2.0/24"})
	ctx := context.Background()

	if block, note := NewRoE(ModeOff, scope).Evaluate(ctx, "Bash", in); block || note != "" {
		t.Fatalf("off should not judge: block=%v note=%q", block, note)
	}
	block, note := NewRoE(ModeWarn, scope).Evaluate(ctx, "Bash", in)
	if block {
		t.Fatal("warn must not block")
	}
	if !strings.Contains(note, "出界告警") {
		t.Fatalf("warn note=%q", note)
	}
	block, note = NewRoE(ModeStrict, scope).Evaluate(ctx, "Bash", in)
	if !block || !strings.Contains(note, "不在本任务授权范围内") {
		t.Fatalf("strict should block: block=%v note=%q", block, note)
	}
	loop, _ := json.Marshal(map[string]any{"command": "curl http://127.0.0.1:8080/health"})
	if block, _ := NewRoE(ModeStrict, scope).Evaluate(ctx, "Bash", loop); block {
		t.Fatal("strict must not block loopback")
	}
	if block, _ := NewRoE(ModeStrict, func(context.Context) (*ScopeMatcher, error) { return nil, nil }).Evaluate(ctx, "Bash", in); block {
		t.Fatal("unknown scope must not block")
	}
}

func TestNormalizeMode(t *testing.T) {
	if NormalizeMode(" STRICT ") != ModeStrict {
		t.Fatal("normalize strict")
	}
	if NormalizeMode("whatever") != "" {
		t.Fatal("unknown mode should return empty")
	}
}
