// internal/capture/capture_test.go
package capture

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type stubFetcher struct {
	title string
	err   error
}

func (f stubFetcher) FetchTitle(ctx context.Context, url string) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	return f.title, nil
}

// Temp-vault integration test (design spec §Testing strategy tier 3):
// capture -> file content, MOC entry appended.
func TestCaptureWritesNoteAndUpdatesMOC(t *testing.T) {
	vaultDir := t.TempDir()
	inbox := filepath.Join(vaultDir, "00-Inbox")
	resources := filepath.Join(vaultDir, "03-Resources")
	if err := os.MkdirAll(resources, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(resources, "MOC Music.md"), []byte("# MOC Music\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := CaptureConfig{VaultDir: vaultDir, Inbox: "00-Inbox", Resources: "03-Resources"}
	req := Request{Body: "some idea", Title: "New Idea", Source: "cli", MOCName: "Music"}
	now := time.Date(2026, 7, 15, 8, 30, 0, 0, time.UTC)
	result, err := Capture(context.Background(), cfg, req, stubFetcher{}, now)
	if err != nil {
		t.Fatal(err)
	}
	if result.Rel != "00-Inbox/2026-07-15 0830 New Idea.md" {
		t.Errorf("Rel = %q", result.Rel)
	}
	if result.MOCLinked != "MOC Music" {
		t.Errorf("MOCLinked = %q", result.MOCLinked)
	}
	if _, err := os.Stat(filepath.Join(inbox, "2026-07-15 0830 New Idea.md")); err != nil {
		t.Errorf("note missing: %v", err)
	}
	moc, err := os.ReadFile(filepath.Join(resources, "MOC Music.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(moc), "[[New Idea]]") {
		t.Errorf("MOC not updated: %s", moc)
	}
}

// CONTRACT(#46): a bare-URL first line uses the fetched title.
func TestCaptureBareURLUsesFetchedTitle(t *testing.T) {
	vaultDir := t.TempDir()
	cfg := CaptureConfig{VaultDir: vaultDir, Inbox: "00-Inbox"}
	req := Request{Body: "https://example.com/article", FetchTitle: true}
	now := time.Date(2026, 7, 15, 8, 30, 0, 0, time.UTC)
	result, err := Capture(context.Background(), cfg, req, stubFetcher{title: "A Great Article"}, now)
	if err != nil {
		t.Fatal(err)
	}
	if result.Title != "A Great Article" {
		t.Errorf("Title = %q", result.Title)
	}
}

// CONTRACT(#30): a failed fetch falls back to the slugified URL.
func TestCaptureBareURLFetchFailureFallsBack(t *testing.T) {
	vaultDir := t.TempDir()
	cfg := CaptureConfig{VaultDir: vaultDir, Inbox: "00-Inbox"}
	req := Request{Body: "https://example.com/article", FetchTitle: true}
	now := time.Date(2026, 7, 15, 8, 30, 0, 0, time.UTC)
	result, err := Capture(context.Background(), cfg, req, stubFetcher{err: errors.New("network down")}, now)
	if err != nil {
		t.Fatal(err)
	}
	if result.Title != "https example.com article" {
		t.Errorf("Title = %q, want slugified URL fallback", result.Title)
	}
}

// BUG(fixed)(#128): the note is written via vault.WriteNoteAtomic, not a
// raw redirect — no leftover temp file after a successful capture.
func TestCaptureUsesAtomicWrite(t *testing.T) {
	vaultDir := t.TempDir()
	cfg := CaptureConfig{VaultDir: vaultDir, Inbox: "00-Inbox"}
	req := Request{Body: "content", Title: "Atomic Test"}
	if _, err := Capture(context.Background(), cfg, req, stubFetcher{}, time.Now()); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(filepath.Join(vaultDir, "00-Inbox"))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".ov-tmp-") {
			t.Errorf("leftover temp file: %s", e.Name())
		}
	}
}

// CONTRACT(#47): --moc requires exact resolution; a miss aborts the
// capture (and the inbox note is never written — the MOC is resolved
// before any write happens).
func TestCaptureUnknownMOCFails(t *testing.T) {
	vaultDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(vaultDir, "03-Resources"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := CaptureConfig{VaultDir: vaultDir, Inbox: "00-Inbox", Resources: "03-Resources"}
	req := Request{Body: "x", Title: "X", MOCName: "Nonexistent"}
	if _, err := Capture(context.Background(), cfg, req, stubFetcher{}, time.Now()); err == nil {
		t.Fatal("expected an error for an unresolvable MOC")
	}
	if _, err := os.Stat(filepath.Join(vaultDir, "00-Inbox")); err == nil {
		t.Error("the inbox note must not be written when the MOC is unresolvable")
	}
}

// CONTRACT(#45): whitespace-only body is refused.
func TestCaptureEmptyBodyRefused(t *testing.T) {
	vaultDir := t.TempDir()
	cfg := CaptureConfig{VaultDir: vaultDir, Inbox: "00-Inbox"}
	if _, err := Capture(context.Background(), cfg, Request{Body: "   \n  "}, stubFetcher{}, time.Now()); err == nil {
		t.Fatal("expected empty-body refusal")
	}
}
