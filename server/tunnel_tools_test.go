package server

import (
	"context"
	"encoding/json"
	"sync"
	"testing"

	"github.com/Autumn-27/artex/agent"
	"github.com/Autumn-27/artex/db"
	"github.com/Autumn-27/artex/tunnel"
)

func TestPickUpstream(t *testing.T) {
	socksParams := func(port int, user, pass string) json.RawMessage {
		b, _ := json.Marshal(tunnel.Plan{SocksPort: port, AuthUser: user, AuthPass: pass})
		return b
	}
	global := "http://10.0.0.1:8080"
	noTunnel := []*db.TunnelRecord{{ID: 1, Kind: "socks", State: db.TunnelError, DeployParams: socksParams(1080, "", "")}}
	if got := pickUpstream(noTunnel, global); got != global {
		t.Fatalf("no alive tunnel should fall back to global, got %q", got)
	}
	two := []*db.TunnelRecord{
		{ID: 1, Kind: "socks", State: db.TunnelAlive, DeployParams: socksParams(1080, "", "")},
		{ID: 2, Kind: "socks", State: db.TunnelAlive, DeployParams: socksParams(1081, "", "")},
		{ID: 3, Kind: "portfwd", State: db.TunnelAlive, DeployParams: socksParams(0, "", "")},
	}
	if got := pickUpstream(two, global); got != "socks5://127.0.0.1:1081" {
		t.Fatalf("newest alive socks wins, got %q", got)
	}
	suo5 := []*db.TunnelRecord{{ID: 4, Adapter: tunnel.AdapterSuo5, Kind: "socks", State: db.TunnelAlive, DeployParams: socksParams(1082, "artex", "pw")}}
	if got := pickUpstream(suo5, global); got != "socks5://artex:pw@127.0.0.1:1082" {
		t.Fatalf("suo5 upstream must embed auth, got %q", got)
	}
	if got := pickUpstream(nil, ""); got != "" {
		t.Fatalf("direct fallback must be empty, got %q", got)
	}
}

type fakeTunnelLister struct {
	mu   sync.Mutex
	recs []*db.TunnelRecord
}

func (f *fakeTunnelLister) ListByTask(context.Context, int64) ([]*db.TunnelRecord, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]*db.TunnelRecord, len(f.recs))
	copy(out, f.recs)
	return out, nil
}

func (f *fakeTunnelLister) set(recs ...*db.TunnelRecord) {
	f.mu.Lock()
	f.recs = recs
	f.mu.Unlock()
}

func TestTaskProxyManagerHotSwitch(t *testing.T) {
	lister := &fakeTunnelLister{}
	m := newTaskProxyManager(t.TempDir(), lister, func() string { return "" }, 24100, 24150)
	defer m.CloseAll()
	ctx := context.Background()
	if m.HasTunnels(ctx, 7) {
		t.Fatal("empty lister should report no tunnels")
	}
	addr, ca := m.ForTask(ctx, 7)
	if addr == "" {
		t.Fatal("instance should be created")
	}
	_ = ca
	// 隧道上线 → 刷新上游为 socks5
	plan, _ := json.Marshal(tunnel.Plan{SocksPort: 1080})
	lister.set(&db.TunnelRecord{ID: 1, TaskID: 7, Kind: "socks", State: db.TunnelAlive, DeployParams: plan})
	if !m.HasTunnels(ctx, 7) {
		t.Fatal("lister with record should report tunnels")
	}
	m.Refresh(ctx, 7)
	inst := m.proxies[7]
	if inst == nil || inst.upstream != "socks5://127.0.0.1:1080" {
		t.Fatalf("upstream after refresh=%+v", inst)
	}
	// 隧道死亡 → 回退直连
	lister.set(&db.TunnelRecord{ID: 1, TaskID: 7, Kind: "socks", State: db.TunnelError, DeployParams: plan})
	m.Refresh(ctx, 7)
	if inst.upstream != "" {
		t.Fatalf("dead tunnel should fall back to direct, got %q", inst.upstream)
	}
	m.CloseTask(7)
	if _, ok := m.proxies[7]; ok {
		t.Fatal("CloseTask must remove instance")
	}
}

func TestTunnelPlanRoundtrip(t *testing.T) {
	plan, err := tunnel.PlanSocks(&tunnel.PlanOpts{
		TaskID: 3, ViaSessionID: 9, CallbackHost: "10.1.1.1",
		PortMin: 22000, PortMax: 22100, UsedPorts: map[int]bool{},
		DataDir: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	b, err := json.Marshal(plan)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var back tunnel.Plan
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if back.Kind != plan.Kind || back.TaskID != plan.TaskID || back.SocksPort != plan.SocksPort || back.AuthPass != plan.AuthPass {
		t.Fatalf("roundtrip mismatch: %+v vs %+v", back, plan)
	}
}

func TestTunnelToolSchemaMatchesCatalog(t *testing.T) {
	rt := (&tunnelRuntime{}).tools()
	if len(rt) != 3 {
		t.Fatalf("want 3 tunnel tools, got %d", len(rt))
	}
	for _, tl := range rt {
		seed, ok := agent.TunnelToolMeta(tl.Name())
		if !ok {
			t.Fatalf("%s not in catalog", tl.Name())
		}
		var got, want map[string]any
		gb, _ := json.Marshal(tl.InputSchema())
		wb, _ := json.Marshal(seed.Schema)
		_ = json.Unmarshal(gb, &got)
		_ = json.Unmarshal(wb, &want)
		if !sameStringSet(requiredNames(got), requiredNames(want)) {
			t.Fatalf("%s required mismatch", tl.Name())
		}
		if !sameStringSet(propertyNames(got), propertyNames(want)) {
			t.Fatalf("%s properties mismatch", tl.Name())
		}
	}
}
