package server

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestGatewayToClashYAML(t *testing.T) {
	raw := "socks5://user-region-HK-sid-rand@127.0.0.1:1080"
	out, err := gatewayToClashYAML(raw, 3)
	if err != nil {
		t.Fatal(err)
	}
	var cfg map[string]any
	if err := yaml.Unmarshal([]byte(out), &cfg); err != nil {
		t.Fatal(err)
	}
	nodes, _ := cfg["proxies"].([]any)
	if len(nodes) != 3 {
		t.Fatalf("nodes = %d", len(nodes))
	}
	first, _ := nodes[0].(map[string]any)
	if first["type"] != "socks5" || first["server"] != "127.0.0.1" {
		t.Fatalf("node = %v", first)
	}
	name, _ := first["name"].(string)
	if !strings.HasPrefix(name, "gw-HK-") {
		t.Fatalf("name = %q", name)
	}
}
