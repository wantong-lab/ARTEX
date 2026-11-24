package proxypool

import (
	"strings"
	"testing"
)

func TestParseShareLinks(t *testing.T) {
	raw := "vless://11111111-1111-1111-1111-111111111111@example.com:443?security=tls&type=ws&path=%2Fws#vless-a\n" +
		"ss://YWVzLTI1Ni1nY206cGFzcw==@example.org:8388#ss-b"
	nodes, issues, err := ParseShareLinks(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(issues) != 0 {
		t.Fatalf("issues = %v", issues)
	}
	if len(nodes) != 2 {
		t.Fatalf("nodes = %d", len(nodes))
	}
	if nodes[0]["type"] != "vless" || nodes[1]["type"] != "ss" {
		t.Fatalf("types = %v %v", nodes[0]["type"], nodes[1]["type"])
	}
	if nodes[0]["network"] != "ws" || nodes[0]["tls"] != true {
		t.Fatalf("vless opts = %v", nodes[0])
	}
}

func TestParseShareLinksBase64(t *testing.T) {
	raw := "dmxlc3M6Ly8xMTExMTExMS0xMTExLTExMTEtMTExMS0xMTExMTExMTExMTFAZXhhbXBsZS5jb206NDQzP3NlY3VyaXR5PXRscyNvbmU="
	nodes, _, err := ParseShareLinks(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 1 || !strings.Contains(nodes[0]["name"].(string), "one") {
		t.Fatalf("nodes = %v", nodes)
	}
}
