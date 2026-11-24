package server

import (
	"context"
	"testing"
)

func TestResolveEgressFailClosed(t *testing.T) {
	m := &Manager{egressMode: egressPool, failClosed: true}
	res, err := m.ResolveEgress(context.Background(), EgressTarget)
	if err == nil || !res.Blocked {
		t.Fatalf("expected blocked, got %#v err=%v", res, err)
	}
	m.failClosed = false
	res, err = m.ResolveEgress(context.Background(), EgressTarget)
	if err != nil || res.Blocked || res.ProxyURL != "" {
		t.Fatalf("expected direct fallback, got %#v err=%v", res, err)
	}
	m.poolActiveProxy = "socks5://127.0.0.1:1080"
	res, err = m.ResolveEgress(context.Background(), EgressTarget)
	if err != nil || res.ProxyURL != "socks5://127.0.0.1:1080" {
		t.Fatalf("expected active pool, got %#v err=%v", res, err)
	}
}

func TestResolveEgressControlBypassesPool(t *testing.T) {
	m := &Manager{egressMode: egressPool, failClosed: true}
	res, err := m.ResolveEgress(context.Background(), EgressControl)
	if err != nil || res.ProxyURL != "" || res.Blocked {
		t.Fatalf("control should be direct, got %#v err=%v", res, err)
	}
	m.globalProxy = "http://127.0.0.1:8080"
	res, err = m.ResolveEgress(context.Background(), EgressControl)
	if err != nil || res.ProxyURL != "http://127.0.0.1:8080" {
		t.Fatalf("control should use fixed, got %#v err=%v", res, err)
	}
}

func TestResolveEgressFixedAndDirect(t *testing.T) {
	m := &Manager{egressMode: egressDirect, globalProxy: "http://127.0.0.1:8080"}
	if res, _ := m.ResolveEgress(context.Background(), EgressTarget); res.ProxyURL != "" {
		t.Fatalf("direct got %#v", res)
	}
	m.egressMode = egressFixed
	if res, _ := m.ResolveEgress(context.Background(), EgressTarget); res.ProxyURL != "http://127.0.0.1:8080" {
		t.Fatalf("fixed got %#v", res)
	}
}
