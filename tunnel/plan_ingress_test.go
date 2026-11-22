package tunnel

import (
	"strings"
	"testing"
)

func TestPlanIngress(t *testing.T) {
	p, err := PlanIngress(&PlanOpts{
		TaskID: 7, ViaSessionID: 3, ListenHost: "0.0.0.0", ListenPort: 39444,
		CallbackHost: "platform.example", PlatformChiselPort: 39443,
		C2CallbackHost: "vps.example", C2ListenerID: 9,
		TargetHost: "127.0.0.1", TargetPort: 54321, DataDir: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if p.Kind != KindIngress || p.ListenPort != 39444 || p.PlatformChiselPort != 39443 || p.C2ListenerID != 9 {
		t.Fatalf("bad plan: %+v", p)
	}
	if p.PlatformAddr != "platform.example:39443" {
		t.Fatalf("platform addr=%q", p.PlatformAddr)
	}
	joinedServer := strings.Join(p.ServerArgs, " ")
	if strings.Contains(joinedServer, "--reverse") {
		t.Fatalf("ingress server must not be reverse: %v", p.ServerArgs)
	}
	if !strings.Contains(joinedServer, "-p 39443") {
		t.Fatalf("server port missing: %v", p.ServerArgs)
	}
	remote := p.ClientArgs[len(p.ClientArgs)-1]
	if remote != "0.0.0.0:39444:127.0.0.1:54321" {
		t.Fatalf("client remote=%q", remote)
	}
	if strings.Contains(strings.Join(p.ClientArgs, " "), "R:") {
		t.Fatalf("ingress client must use local forward: %v", p.ClientArgs)
	}
}

func TestPlanIngressRequiresCallback(t *testing.T) {
	if _, err := PlanIngress(&PlanOpts{ListenPort: 39444, PlatformChiselPort: 39443, TargetPort: 80}); err == nil {
		t.Fatal("missing callback host should fail")
	}
}
