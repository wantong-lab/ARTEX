package server

import "testing"

func TestValidateSubscriptionURL(t *testing.T) {
	for _, good := range []string{"https://example.com/clash.yaml", "http://127.0.0.1:8080/sub?token=x"} {
		if err := validateSubscriptionURL(good); err != nil {
			t.Fatalf("good %q rejected: %v", good, err)
		}
	}
	for _, bad := range []string{"", "file:///etc/passwd", "ftp://example.com/x", "example.com/sub"} {
		if err := validateSubscriptionURL(bad); err == nil {
			t.Fatalf("bad %q accepted", bad)
		}
	}
}
