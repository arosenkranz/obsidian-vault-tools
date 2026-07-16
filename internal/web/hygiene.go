// internal/web/hygiene.go
package web

import (
	"net"
	"net/http"
	"net/url"
)

// hygieneMiddleware validates the Host header against the configured bind
// (kills DNS rebinding) and, for state-changing requests, requires a
// same-origin/absent Origin AND the HX-Request header (kills drive-by form
// CSRF — CORS alone does not stop cross-origin form POSTs to 127.0.0.1).
// Distinct from auth: any local process can still reach the API (accepted
// for v1, design spec §Web layer). Behavior inventory row #134.
func hygieneMiddleware(bind string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Host != bind && !isLoopbackHost(r.Host) {
				http.Error(w, "invalid host header", http.StatusBadRequest)
				return
			}
			if r.Method == http.MethodPost {
				if origin := r.Header.Get("Origin"); origin != "" && !sameOrigin(origin, r.Host) {
					http.Error(w, "cross-origin request rejected", http.StatusForbidden)
					return
				}
				if r.Header.Get("HX-Request") != "true" {
					http.Error(w, "missing HX-Request header", http.StatusForbidden)
					return
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

func isLoopbackHost(host string) bool {
	h, _, err := net.SplitHostPort(host)
	if err != nil {
		h = host
	}
	if h == "localhost" {
		return true
	}
	ip := net.ParseIP(h)
	return ip != nil && ip.IsLoopback()
}

func sameOrigin(origin, host string) bool {
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	return u.Host == host
}
