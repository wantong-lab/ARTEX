package proxypool

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

// fakeStore 是 Store 的内存实现。
type fakeStore struct {
	mu      sync.Mutex
	entries map[int64]Entry
	fails   map[int64]int
	lastErr map[int64]string
}

func newFakeStore() *fakeStore {
	return &fakeStore{entries: map[int64]Entry{}, fails: map[int64]int{}, lastErr: map[int64]string{}}
}

func (f *fakeStore) add(id int64, rawURL string) {
	f.mu.Lock()
	f.entries[id] = Entry{ID: id, URL: rawURL, Status: "active"}
	f.mu.Unlock()
}

func (f *fakeStore) List(context.Context) ([]Entry, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []Entry
	for _, e := range f.entries {
		out = append(out, e)
	}
	return out, nil
}

func (f *fakeStore) Get(_ context.Context, id int64) (Entry, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	e, ok := f.entries[id]
	return e, ok, nil
}

func (f *fakeStore) RecordFailure(_ context.Context, id int64, msg string) error {
	f.mu.Lock()
	f.fails[id]++
	f.lastErr[id] = msg
	f.mu.Unlock()
	return nil
}

func (f *fakeStore) UpdateProbe(_ context.Context, id int64, exitIP string, latencyMs int, errMsg string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	e := f.entries[id]
	if errMsg != "" {
		e.Status = "failed"
		f.fails[id]++
		f.lastErr[id] = errMsg
	} else {
		e.Status = "active"
	}
	f.entries[id] = e
	return nil
}

// fakeEgressProxy 是「HTTP 代理」：无论请求哪个目标，都返回自己的出口 IP。
func fakeEgressProxy(t *testing.T, exitIP string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"ip": exitIP})
	}))
	t.Cleanup(srv.Close)
	return srv
}

func directEgress(t *testing.T, exitIP string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(exitIP))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestParseProxyURL(t *testing.T) {
	ok := []string{"http://127.0.0.1:8080", "socks5h://user:pass@10.0.0.2:1080"}
	for _, raw := range ok {
		if _, err := ParseProxyURL(raw); err != nil {
			t.Fatalf("%s should parse: %v", raw, err)
		}
	}
	bad := []string{"", "ftp://1.2.3.4:21", "http://1.2.3.4", "socks5://1.2.3.4:abc"}
	for _, raw := range bad {
		if _, err := ParseProxyURL(raw); err == nil {
			t.Fatalf("%s should be rejected", raw)
		}
	}
}

func TestProbeAndReadExitIP(t *testing.T) {
	store := newFakeStore()
	proxy := fakeEgressProxy(t, "203.0.113.9")
	store.add(1, proxy.URL)
	m := NewManager(store)
	m.SetProbeURL("http://example.invalid/ip")
	res, err := m.Probe(context.Background(), 1)
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	if !res.OK || res.ExitIP != "203.0.113.9" {
		t.Fatalf("probe result=%+v", res)
	}
	// 坏代理：探测必须失败并写回 failed
	store.add(2, "http://127.0.0.1:1")
	res2, err := m.Probe(context.Background(), 2)
	if err != nil {
		t.Fatalf("probe bad: %v", err)
	}
	if res2.OK || res2.Error == "" {
		t.Fatalf("bad proxy should be judged unusable: %+v", res2)
	}
	if e, _, _ := store.Get(context.Background(), 2); e.Status != "failed" {
		t.Fatalf("bad proxy status=%q", e.Status)
	}
}

func TestSwitchChangesExitAndFallback(t *testing.T) {
	store := newFakeStore()
	p1 := fakeEgressProxy(t, "198.51.100.1")
	p2 := fakeEgressProxy(t, "198.51.100.2")
	store.add(1, p1.URL)
	store.add(2, p2.URL)
	m := NewManager(store)
	m.SetProbeURL("http://example.invalid/ip")

	m.SetActive(1)
	ip1, _, err := m.Egress(context.Background())
	if err != nil || ip1 != "198.51.100.1" {
		t.Fatalf("egress via 1: ip=%q err=%v", ip1, err)
	}
	m.SetActive(2)
	ip2, _, err := m.Egress(context.Background())
	if err != nil || ip2 != "198.51.100.2" {
		t.Fatalf("egress via 2: ip=%q err=%v", ip2, err)
	}

	// 活动代理坏死 → 自动回退全局代理
	global := fakeEgressProxy(t, "198.51.100.3")
	if err := m.SetGlobal(global.URL); err != nil {
		t.Fatalf("set global: %v", err)
	}
	store.add(3, "http://127.0.0.1:1")
	m.SetActive(3)
	ip3, _, err := m.Egress(context.Background())
	if err != nil || ip3 != "198.51.100.3" {
		t.Fatalf("fallback via global: ip=%q err=%v", ip3, err)
	}
	store.mu.Lock()
	fails := store.fails[3]
	store.mu.Unlock()
	if fails == 0 {
		t.Fatal("active failure must be recorded for failover bookkeeping")
	}

	// 无全局代理时回退直连
	_ = m.SetGlobal("")
	direct := directEgress(t, "192.0.2.10")
	m.SetProbeURL(direct.URL)
	ip4, _, err := m.Egress(context.Background())
	if err != nil || ip4 != "192.0.2.10" {
		t.Fatalf("fallback direct: ip=%q err=%v", ip4, err)
	}
}

func TestSetGlobalValidates(t *testing.T) {
	m := NewManager(newFakeStore())
	if err := m.SetGlobal("ftp://1.2.3.4:21"); err == nil {
		t.Fatal("ftp proxy must be rejected")
	}
	if err := m.SetGlobal("http://127.0.0.1:8080"); err != nil {
		t.Fatalf("valid proxy rejected: %v", err)
	}
	if m.Global() == "" {
		t.Fatal("global not stored")
	}
}

var _ = fmt.Sprintf
