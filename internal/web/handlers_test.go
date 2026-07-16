// internal/web/handlers_test.go
package web

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestHandleInboxEmpty(t *testing.T) {
	_, cfg := newTestVault(t)
	srv := New(cfg, stubFetcher{}, &fakeLLMRunner{}, nil)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Host = cfg.Bind
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Inbox is empty") {
		t.Errorf("body = %q", rec.Body.String())
	}
}

// DECIDE(#138): the web inbox list renders through vault.ListInbox, the
// same query the CLI's `ov2 inbox` uses.
func TestHandleInboxListsNotes(t *testing.T) {
	vaultDir, cfg := newTestVault(t)
	if err := os.WriteFile(filepath.Join(vaultDir, "00-Inbox", "2026-07-15 0800 Foo.md"), []byte("# Foo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	srv := New(cfg, stubFetcher{}, &fakeLLMRunner{}, nil)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Host = cfg.Bind
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if !strings.Contains(rec.Body.String(), "2026-07-15 0800 Foo") {
		t.Errorf("body = %q", rec.Body.String())
	}
}

// Temp-vault integration test (design spec §Testing strategy tier 3): the
// web capture handler writes a real note.
func TestHandleCaptureSubmitWritesNote(t *testing.T) {
	vaultDir, cfg := newTestVault(t)
	srv := New(cfg, stubFetcher{}, &fakeLLMRunner{}, func() time.Time { return time.Date(2026, 7, 15, 8, 30, 0, 0, time.UTC) })
	form := url.Values{"title": {"Web Capture"}, "body": {"hello from web"}}
	req := httptest.NewRequest(http.MethodPost, "/capture", strings.NewReader(form.Encode()))
	req.Host = cfg.Bind
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "Captured:") {
		t.Errorf("body = %q", rec.Body.String())
	}
	if _, err := os.Stat(filepath.Join(vaultDir, "00-Inbox", "2026-07-15 0830 Web Capture.md")); err != nil {
		t.Errorf("note not written: %v", err)
	}
}

// CONTRACT(#135): the opt-in checkbox triggers a title fetch.
func TestHandleCaptureSubmitFetchTitleOptIn(t *testing.T) {
	vaultDir, cfg := newTestVault(t)
	srv := New(cfg, stubFetcher{title: "Fetched Title"}, &fakeLLMRunner{}, func() time.Time { return time.Date(2026, 7, 15, 8, 30, 0, 0, time.UTC) })
	form := url.Values{"body": {"https://example.com/article"}, "fetch_title": {"on"}}
	req := httptest.NewRequest(http.MethodPost, "/capture", strings.NewReader(form.Encode()))
	req.Host = cfg.Bind
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if _, err := os.Stat(filepath.Join(vaultDir, "00-Inbox", "2026-07-15 0830 Fetched Title.md")); err != nil {
		t.Fatalf("note with fetched title not written: %v\nbody: %s", err, rec.Body.String())
	}
}

// DECIDE(#135): without the opt-in checkbox, title-fetch never happens even
// for a bare-URL body.
func TestHandleCaptureSubmitNeverAutoFetches(t *testing.T) {
	vaultDir, cfg := newTestVault(t)
	srv := New(cfg, stubFetcher{title: "Should Not Be Used"}, &fakeLLMRunner{}, func() time.Time { return time.Date(2026, 7, 15, 8, 30, 0, 0, time.UTC) })
	form := url.Values{"body": {"https://example.com/article"}}
	req := httptest.NewRequest(http.MethodPost, "/capture", strings.NewReader(form.Encode()))
	req.Host = cfg.Bind
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if _, err := os.Stat(filepath.Join(vaultDir, "00-Inbox", "2026-07-15 0830 Should Not Be Used.md")); err == nil {
		t.Fatal("title fetch happened without the opt-in checkbox")
	}
	if _, err := os.Stat(filepath.Join(vaultDir, "00-Inbox", "2026-07-15 0830 https example.com article.md")); err != nil {
		t.Errorf("expected the slugified-URL fallback title, note missing: %v", err)
	}
}

// DECIDE(#134, new in v2): Host-header validation.
func TestHygieneRejectsWrongHost(t *testing.T) {
	_, cfg := newTestVault(t)
	srv := New(cfg, stubFetcher{}, &fakeLLMRunner{}, nil)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Host = "evil.example.com"
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

// DECIDE(#134): a cross-origin POST is rejected even with HX-Request set.
func TestHygieneRejectsCrossOriginPost(t *testing.T) {
	_, cfg := newTestVault(t)
	srv := New(cfg, stubFetcher{}, &fakeLLMRunner{}, nil)
	form := url.Values{"title": {"x"}, "body": {"y"}}
	req := httptest.NewRequest(http.MethodPost, "/capture", strings.NewReader(form.Encode()))
	req.Host = cfg.Bind
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	req.Header.Set("Origin", "http://evil.example.com")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", rec.Code)
	}
}

// DECIDE(#134): a POST without the HX-Request header is rejected (kills
// drive-by form CSRF that CORS alone would not stop).
func TestHygieneRejectsMissingHXRequestHeader(t *testing.T) {
	_, cfg := newTestVault(t)
	srv := New(cfg, stubFetcher{}, &fakeLLMRunner{}, nil)
	form := url.Values{"title": {"x"}, "body": {"y"}}
	req := httptest.NewRequest(http.MethodPost, "/capture", strings.NewReader(form.Encode()))
	req.Host = cfg.Bind
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", rec.Code)
	}
}
