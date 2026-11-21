package agent

import "testing"

func TestServiceViewFromInsert(t *testing.T) {
	it := assetInputItem{
		URL:          "https://honey.example.com:8443/admin",
		PageTitle:    "Welcome to nginx!",
		Technologies: []string{"nginx", "fake-header"},
	}
	v := serviceViewFromInsert(it)
	if v.Port != 8443 {
		t.Fatalf("port=%d, want 8443 (from URL)", v.Port)
	}
	if v.Title != "Welcome to nginx!" {
		t.Fatalf("title=%q", v.Title)
	}
	if v.Banner != "nginx fake-header" {
		t.Fatalf("banner=%q", v.Banner)
	}

	// 非 HTTP 服务没有 URL：回退到显式 port
	it2 := assetInputItem{Port: 2222, Technologies: []string{"ssh"}}
	if got := serviceViewFromInsert(it2); got.Port != 2222 {
		t.Fatalf("fallback port=%d, want 2222", got.Port)
	}
}
