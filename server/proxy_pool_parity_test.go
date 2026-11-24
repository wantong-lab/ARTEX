package server

import (
	"testing"

	"gopkg.in/yaml.v3"
)

func TestBuildMihomoConfigParityGroups(t *testing.T) {
	raw := `proxies:
  - name: a
    type: socks5
    server: 1.1.1.1
    port: 1080
  - name: b
    type: socks5
    server: 2.2.2.2
    port: 1080
`
	b, err := buildMihomoConfigWithSelection(raw, 1234, "b", "manual", 0, "")
	if err != nil {
		t.Fatal(err)
	}
	var cfg map[string]any
	if err := yaml.Unmarshal(b, &cfg); err != nil {
		t.Fatal(err)
	}
	groups, _ := cfg["proxy-groups"].([]any)
	if len(groups) != 2 {
		t.Fatalf("proxy-groups = %v", cfg["proxy-groups"])
	}
	auto, _ := groups[1].(map[string]any)
	if auto["type"] != "load-balance" || auto["strategy"] != "consistent-hashing" {
		t.Fatalf("auto group = %v", auto)
	}
	rules, _ := cfg["rules"].([]any)
	if len(rules) != 1 || rules[0] != "MATCH,ARTEX-SELECT" {
		t.Fatalf("rules = %v", rules)
	}
}
