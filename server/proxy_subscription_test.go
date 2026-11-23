package server

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestLooksLikeClashConfig(t *testing.T) {
	if !looksLikeClashConfig("mixed-port: 7890\nproxies:\n  - name: x\n") {
		t.Fatal("expected clash config")
	}
	if looksLikeClashConfig("vless://x") {
		t.Fatal("base64 uri list is not clash config")
	}
}

func TestSubscriptionCandidateURLs(t *testing.T) {
	cands := subscriptionCandidateURLs("https://example.com/sub?OwO=abc")
	if len(cands) < 2 {
		t.Fatalf("expected fallback candidates, got %v", cands)
	}
	found := false
	for _, c := range cands {
		if strings.Contains(c, "flag=meta") {
			found = true
		}
	}
	if !found {
		t.Fatalf("missing flag=meta candidate: %v", cands)
	}
}

func TestDetectSubscriptionProtocols(t *testing.T) {
	raw := base64.StdEncoding.EncodeToString([]byte("vless://a\nhysteria2://b\n"))
	got := detectSubscriptionProtocols(raw)
	if got["vless"] != 1 || got["hysteria2"] != 1 {
		t.Fatalf("unexpected counts: %#v", got)
	}
}

func TestBuildMihomoConfigOverridesPort(t *testing.T) {
	raw := "mixed-port: 7890\nallow-lan: true\nport: 7891\nproxies:\n  - {name: n1, type: vless, server: x, port: 443}\n"
	out, err := buildMihomoConfig(raw, 17890)
	if err != nil {
		t.Fatal(err)
	}
	var cfg map[string]any
	if err := yaml.Unmarshal(out, &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg["mixed-port"] != 17890 || cfg["allow-lan"] != false {
		t.Fatalf("bad overrides: %#v", cfg)
	}
	if _, ok := cfg["port"]; ok {
		t.Fatal("legacy port must be removed")
	}
}

func TestFetchClashSubscriptionAutoFormat(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("flag") == "meta" {
			_, _ = w.Write([]byte("mixed-port: 7890\nproxies:\n  - {name: n1, type: vless, server: x, port: 443}\n"))
			return
		}
		_, _ = w.Write([]byte(base64.StdEncoding.EncodeToString([]byte("vless://x"))))
	}))
	defer srv.Close()
	s := &Server{}
	body, err := s.fetchClashSubscription(t.Context(), srv.Client(), srv.URL+"/sub")
	if err != nil {
		t.Fatal(err)
	}
	if !looksLikeClashConfig(body) {
		t.Fatalf("not clash yaml: %q", body)
	}
}
