package server

import "testing"

func TestManagerEffectiveProxyModes(t *testing.T) {
	m := &Manager{}
	m.egressMode = egressDirect
	m.globalProxy = "socks5://127.0.0.1:1080"
	if got := m.EffectiveProxy(); got != "" {
		t.Fatalf("direct got %q", got)
	}
	m.egressMode = egressFixed
	if got := m.EffectiveProxy(); got != "socks5://127.0.0.1:1080" {
		t.Fatalf("fixed got %q", got)
	}
	m.egressMode = egressPool
	m.poolActiveProxy = "http://127.0.0.1:18080"
	if got := m.EffectiveProxy(); got != "http://127.0.0.1:18080" {
		t.Fatalf("pool active got %q", got)
	}
	m.poolActiveProxy = ""
	m.poolFallbackProxy = "http://127.0.0.1:19090"
	if got := m.EffectiveProxy(); got != "http://127.0.0.1:19090" {
		t.Fatalf("pool fallback got %q", got)
	}
	m.poolFallbackProxy = ""
	if got := m.EffectiveProxy(); got != "" {
		t.Fatalf("pool empty got %q", got)
	}
	if inferEgressMode("") != egressDirect {
		t.Fatal("empty global should infer direct")
	}
	if inferEgressMode("http://1.2.3.4:8080") != egressFixed {
		t.Fatal("non-empty global should infer fixed")
	}
}
