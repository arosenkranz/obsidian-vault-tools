// internal/web/bind_test.go
package web

import "testing"

// DECIDE(#133, new in v2): loopback and Tailscale addresses are allowed by
// default; anything else needs the override.
func TestAllowBind(t *testing.T) {
	cases := []struct {
		host     string
		override bool
		allow    bool
	}{
		{"127.0.0.1", false, true},
		{"localhost", false, true},
		{"::1", false, true},
		{"100.64.0.5", false, true}, // Tailscale CGNAT range
		{"100.100.100.100", false, true},
		{"100.127.255.254", false, true},
		{"100.128.0.1", false, false}, // just outside the /10
		{"", false, false},            // wildcard bind, not localhost
		{"0.0.0.0", false, false},
		{"192.168.1.5", false, false},
		{"8.8.8.8", false, false},
		{"0.0.0.0", true, true}, // override always wins
		{"8.8.8.8", true, true},
	}
	for _, c := range cases {
		err := AllowBind(c.host, c.override)
		if c.allow && err != nil {
			t.Errorf("AllowBind(%q, %v): expected allow, got %v", c.host, c.override, err)
		}
		if !c.allow && err == nil {
			t.Errorf("AllowBind(%q, %v): expected refusal", c.host, c.override)
		}
	}
}
