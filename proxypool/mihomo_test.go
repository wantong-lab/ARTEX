package proxypool

import (
	"strings"
	"testing"
)

func TestParseMihomoConfig(t *testing.T) {
	data := `proxies:
  - name: h1
    type: http
    server: 1.2.3.4
    port: 8080
    username: u
    password: p
  - name: s1
    type: socks5
    server: 5.6.7.8
    port: 1080
  - name: v1
    type: vmess
    server: 9.9.9.9
    port: 443
  - {name: inline, type: http, server: 10.0.0.1, port: 3128}
proxy-groups:
  - name: auto
`
	imp, err := ParseMihomoConfig(data)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(imp.Proxies) != 3 || len(imp.Skipped) != 1 {
		t.Fatalf("proxies=%d skipped=%d", len(imp.Proxies), len(imp.Skipped))
	}
	u, err := imp.Proxies[0].URL()
	if err != nil || !strings.Contains(u, "u:p@1.2.3.4:8080") {
		t.Fatalf("url=%q err=%v", u, err)
	}
	if _, err := ParseProxyURL(u); err != nil {
		t.Fatalf("generated url not parseable: %v", err)
	}
	if !strings.Contains(imp.Skipped[0].Reason, "mihomo") {
		t.Fatalf("skip reason=%q", imp.Skipped[0].Reason)
	}
	if _, err := ParseMihomoConfig(`rules:
  - x
`); err == nil {
		t.Fatal("config without proxies should error")
	}
}
