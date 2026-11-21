package c2

import "testing"

func TestValidateBindHost(t *testing.T) {
	cases := []struct {
		host  string
		allow bool
		ok    bool
	}{
		{"", false, true},
		{"127.0.0.1", false, true},
		{"::1", false, true},
		{"localhost", false, true},
		{"0.0.0.0", false, false},
		{"::", false, false},
		{"10.0.0.5", false, false},
		{"0.0.0.0", true, true},
		{"10.0.0.5", true, true},
	}
	for _, c := range cases {
		err := ValidateBindHost(c.host, c.allow)
		if (err == nil) != c.ok {
			t.Fatalf("ValidateBindHost(%q, %v) err=%v, want ok=%v", c.host, c.allow, err, c.ok)
		}
	}
}
