package tunnel

import (
	"strings"
	"testing"
)

func TestPlanIngressReverse(t *testing.T) {
	p, err := PlanIngressReverse(&PlanOpts{
		TaskID: 7, ViaSessionID: 3,
		ListenHost: "0.0.0.0", ListenPort: 39444,
		CallbackHost: "vps.example.com", PlatformChiselPort: 39443,
		C2CallbackHost: "vps.example.com", C2ListenerID: 9,
		TargetHost: "127.0.0.1", TargetPort: 19660,
		NodeLabel: "vps-hk", DataDir: "/tmp/tunnels",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !p.Reverse {
		t.Fatal("reverse flag not set")
	}
	if p.RemoteAuthFile == "" {
		t.Fatal("remote authfile missing")
	}
	server := strings.Join(p.ServerArgs, " ")
	if !strings.Contains(server, "server") || !strings.Contains(server, "--reverse") || !strings.Contains(server, "--authfile") {
		t.Fatalf("bad server args: %v", p.ServerArgs)
	}
	client := strings.Join(p.ClientArgs, " ")
	if !strings.Contains(client, "client") || !strings.Contains(client, "R:0.0.0.0:39444:127.0.0.1:19660") {
		t.Fatalf("bad client args: %v", p.ClientArgs)
	}
	if p.PlatformAddr != "vps.example.com:39443" {
		t.Fatalf("platform addr=%s", p.PlatformAddr)
	}
	if p.VerifyAddr != "" {
		t.Fatalf("unexpected verify addr: %s", p.VerifyAddr)
	}
}
