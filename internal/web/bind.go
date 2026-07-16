// internal/web/bind.go
package web

import (
	"fmt"
	"net"
)

// AllowBind reports whether host is safe to listen on without an explicit
// override: loopback and "localhost" are always allowed; a Tailscale
// address (100.64.0.0/10 — the CGNAT range Tailscale assigns from) is
// allowed because it is not reachable from an open network; everything
// else (an empty host meaning a wildcard bind, 0.0.0.0, a LAN IP, ...)
// requires override=true. Behavior inventory row #133 — new in v2, design
// spec §Web layer "Bind guard".
func AllowBind(host string, override bool) error {
	if override {
		return nil
	}
	if host == "localhost" {
		return nil
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return fmt.Errorf("refusing to bind %q: not a recognized loopback or Tailscale address; pass --allow-nonlocal-bind to bind anyway", host)
	}
	if ip.IsLoopback() {
		return nil
	}
	if ip4 := ip.To4(); ip4 != nil && ip4[0] == 100 && ip4[1]&0xc0 == 64 {
		return nil // 100.64.0.0/10
	}
	return fmt.Errorf("refusing to bind %s: not loopback or a Tailscale address; pass --allow-nonlocal-bind to bind anyway", host)
}
