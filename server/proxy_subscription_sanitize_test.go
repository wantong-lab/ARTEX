package server

import (
	"testing"

	"gopkg.in/yaml.v3"
)

func TestBuildMihomoConfigSanitizesDangerousFields(t *testing.T) {
	raw := "mixed-port: 7890\ntun:\n  enable: true\nexternal-controller: 127.0.0.1:9090\nsecret: x\nproxy-providers:\n  p: {url: http://example.com}\nrule-providers:\n  r: {url: http://example.com}\nscript: x\nprofile: x\ninterface-name: eth0\nproxies:\n  - {name: n1, type: vless, server: x, port: 443}\n"
	out, err := buildMihomoConfig(raw, 17890)
	if err != nil {
		t.Fatal(err)
	}
	var cfg map[string]any
	if err := yaml.Unmarshal(out, &cfg); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"tun", "external-controller", "secret", "proxy-providers", "rule-providers", "script", "profile", "interface-name"} {
		if _, ok := cfg[k]; ok {
			t.Fatalf("dangerous field %s not removed", k)
		}
	}
	if cfg["mixed-port"] != 17890 {
		t.Fatalf("mixed-port = %v", cfg["mixed-port"])
	}
}
