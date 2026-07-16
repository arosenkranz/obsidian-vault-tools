package capture

import (
	"context"
	"net"
	"strings"
	"testing"
)

// CONTRACT(#31): first <title> tag, case-insensitive/dotall, entities
// unescaped, whitespace collapsed.
func TestExtractTitle(t *testing.T) {
	cases := []struct {
		name    string
		html    string
		want    string
		wantErr bool
	}{
		{"simple", "<html><head><TITLE>Hello &amp; World</TITLE></head></html>", "Hello & World", false},
		{"whitespace", "<title>\n  Multi\n  Line  \n</title>", "Multi Line", false},
		{"missing", "<html><body>no title here</body></html>", "", true},
		{"empty", "<title></title>", "", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := ExtractTitle(c.html)
			if c.wantErr {
				if err == nil {
					t.Fatal("expected an error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != c.want {
				t.Errorf("ExtractTitle = %q, want %q", got, c.want)
			}
		})
	}
}

// CONTRACT(#29): bot-challenge interstitials are rejected, not returned as
// the title.
func TestExtractTitleRejectsChallenge(t *testing.T) {
	html := `<title>Just a moment...</title><script src="https://challenges.cloudflare.com/x.js"></script>`
	if _, err := ExtractTitle(html); err == nil {
		t.Fatal("expected a challenge-interstitial rejection")
	}
}

// DECIDE(#131, new in v2): rejectUnsafeIP refuses loopback, private,
// link-local, and CGNAT (100.64.0.0/10) addresses; public addresses pass.
func TestRejectUnsafeIP(t *testing.T) {
	cases := []struct {
		ip     string
		reject bool
	}{
		{"127.0.0.1", true},
		{"10.0.0.1", true},
		{"172.16.0.1", true},
		{"192.168.1.1", true},
		{"169.254.1.1", true},
		{"100.64.0.1", true},
		{"100.100.100.100", true},
		{"100.127.255.254", true},
		{"100.128.0.1", false},
		{"100.63.255.255", false},
		{"::1", true},
		{"fe80::1", true},
		{"8.8.8.8", false},
		{"1.1.1.1", false},
	}
	for _, c := range cases {
		ip := net.ParseIP(c.ip)
		if ip == nil {
			t.Fatalf("bad test IP %q", c.ip)
		}
		err := rejectUnsafeIP(ip)
		if c.reject && err == nil {
			t.Errorf("rejectUnsafeIP(%s): expected rejection", c.ip)
		}
		if !c.reject && err != nil {
			t.Errorf("rejectUnsafeIP(%s): expected acceptance, got %v", c.ip, err)
		}
	}
}

// DECIDE(#131): the real HTTP fetcher refuses a loopback target through the
// same DialContext guard, not just in a standalone helper.
func TestHTTPTitleFetcherRefusesLoopback(t *testing.T) {
	f := NewHTTPTitleFetcher()
	_, err := f.FetchTitle(context.Background(), "http://127.0.0.1:1/")
	if err == nil {
		t.Fatal("expected the SSRF guard to refuse a loopback target")
	}
	if !strings.Contains(err.Error(), "refus") {
		t.Errorf("error = %v, want a refusal message from the SSRF guard", err)
	}
}

// CONTRACT(#30): fetch failure is never fatal — errors return for the
// caller to fall back on.
func TestHTTPTitleFetcherBadHost(t *testing.T) {
	f := NewHTTPTitleFetcher()
	if _, err := f.FetchTitle(context.Background(), "http://this-host-does-not-resolve.invalid/"); err == nil {
		t.Fatal("expected a DNS resolution error")
	}
}
