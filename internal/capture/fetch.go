// internal/capture/fetch.go
package capture

import (
	"context"
	"errors"
	"fmt"
	"html"
	"io"
	"net"
	"net/http"
	"regexp"
	"strings"
	"time"
)

// TitleFetcher fetches the <title> of a URL. Errors are never fatal to a
// capture — callers fall back to a slugified URL (behavior inventory row
// #30). Injected so tests never hit the network.
type TitleFetcher interface {
	FetchTitle(ctx context.Context, url string) (string, error)
}

const fetchUserAgent = "Mozilla/5.0 (compatible; ov-capture/1.0)"

var (
	titleRe = regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`)
	wsRe    = regexp.MustCompile(`\s+`)
)

// challengeMarkers mirrors v1's bot-challenge interstitial detection
// (behavior inventory row #29): reject rather than return a fake title.
var challengeMarkers = []string{
	"challenges.cloudflare.com",
	"cf-browser-verification",
	"cf_chl_opt",
	"just a moment",
}

// HTTPTitleFetcher is the real network implementation: 5s timeout, a fixed
// UA (row #28), and a post-DNS IP check refusing loopback/private/
// link-local/CGNAT targets (row #131 — new in v2, design spec §Safety item
// 4). The check dials the validated IP directly, never the hostname,
// closing the DNS-rebinding TOCTOU window a check-then-connect-by-name
// approach would leave open.
type HTTPTitleFetcher struct {
	client *http.Client
}

func NewHTTPTitleFetcher() *HTTPTitleFetcher {
	dialer := &net.Dialer{Timeout: 5 * time.Second}
	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, err
			}
			ips, err := net.DefaultResolver.LookupIP(ctx, "ip", host)
			if err != nil {
				return nil, err
			}
			var lastErr error
			for _, ip := range ips {
				if rejErr := rejectUnsafeIP(ip); rejErr != nil {
					lastErr = rejErr
					continue
				}
				conn, dialErr := dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
				if dialErr == nil {
					return conn, nil
				}
				lastErr = dialErr
			}
			if lastErr == nil {
				lastErr = errors.New("no resolvable address")
			}
			return nil, lastErr
		},
	}
	return &HTTPTitleFetcher{client: &http.Client{
		Timeout:   5 * time.Second,
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return errors.New("too many redirects")
			}
			return nil // each hop re-resolves and re-checks via DialContext
		},
	}}
}

// rejectUnsafeIP refuses loopback, private (RFC1918/RFC4193), link-local,
// unspecified, and CGNAT (100.64.0.0/10) addresses. Behavior inventory row
// #131.
func rejectUnsafeIP(ip net.IP) error {
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() {
		return fmt.Errorf("refusing unsafe address %s", ip)
	}
	if ip4 := ip.To4(); ip4 != nil && ip4[0] == 100 && ip4[1]&0xc0 == 64 {
		return fmt.Errorf("refusing CGNAT address %s", ip) // 100.64.0.0/10
	}
	return nil
}

func (f *HTTPTitleFetcher) FetchTitle(ctx context.Context, rawURL string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", fetchUserAgent)
	resp, err := f.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20)) // 2MiB cap
	if err != nil {
		return "", err
	}
	return ExtractTitle(string(body))
}

// ExtractTitle mirrors v1's fetch_url_title HTML parsing (row #31): first
// <title> tag, case-insensitive/dotall, HTML entities unescaped, whitespace
// collapsed. Bot-challenge interstitials are rejected outright (row #29).
func ExtractTitle(htmlBody string) (string, error) {
	lower := strings.ToLower(htmlBody)
	for _, marker := range challengeMarkers {
		if strings.Contains(lower, marker) {
			return "", errors.New("bot-challenge interstitial detected")
		}
	}
	m := titleRe.FindStringSubmatch(htmlBody)
	if m == nil {
		return "", errors.New("no <title> found")
	}
	text := html.UnescapeString(m[1])
	text = strings.TrimSpace(wsRe.ReplaceAllString(text, " "))
	if text == "" {
		return "", errors.New("empty title")
	}
	return text, nil
}
