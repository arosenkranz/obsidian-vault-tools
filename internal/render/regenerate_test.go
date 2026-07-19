// internal/render/regenerate_test.go
package render

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func mustReadFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// CONTRACT(#117-119): a full regenerate cycle — markdown converts,
// splices between the markers, stamps a fresh RENDER_TIMESTAMP, and
// writes atomically.
func TestRegenerateSplicesAndWrites(t *testing.T) {
	vault := t.TempDir()
	mdPath := filepath.Join(vault, "guide.md")
	htmlPath := filepath.Join(vault, "guide.html")
	mustWriteFile(t, mdPath, "# Guide\n\nHello *world*.\n")
	mustWriteFile(t, htmlPath, "<!-- RENDER_SOURCE: guide.md -->\n<!-- RENDER_BODY_START -->stale<!-- RENDER_BODY_END -->\n")

	p := Pair{HTMLPath: htmlPath, MDPath: mdPath, HTMLRel: "guide.html", MDRel: "guide.md"}
	if err := Regenerate(p, time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}

	got := mustReadFile(t, htmlPath)
	if strings.Contains(got, "stale") {
		t.Errorf("stale body survived regenerate: %s", got)
	}
	if !strings.Contains(got, "<em>world</em>") {
		t.Errorf("expected converted markdown body, got: %s", got)
	}
	if !strings.Contains(got, "<!-- RENDER_TIMESTAMP: 2026-07-15 -->") {
		t.Errorf("expected a fresh RENDER_TIMESTAMP, got: %s", got)
	}
}

// CONTRACT: a missing Markdown source is a reported, non-fatal-to-the-
// batch error.
func TestRegenerateMissingSourceErrors(t *testing.T) {
	vault := t.TempDir()
	htmlPath := filepath.Join(vault, "guide.html")
	mustWriteFile(t, htmlPath, "<!-- RENDER_SOURCE: missing.md -->\n<!-- RENDER_BODY_START --><!-- RENDER_BODY_END -->")

	p := Pair{HTMLPath: htmlPath, MDPath: filepath.Join(vault, "missing.md"), HTMLRel: "guide.html", MDRel: "missing.md"}
	err := Regenerate(p, time.Now())
	if !errors.Is(err, ErrSourceMissing) {
		t.Errorf("err = %v, want ErrSourceMissing", err)
	}
}

// CONTRACT(#118): missing RENDER_BODY markers -> ErrNoMarkers, "skip
// with warning" (the cmd layer decides how to report/continue).
func TestRegenerateMissingMarkersErrors(t *testing.T) {
	vault := t.TempDir()
	mdPath := filepath.Join(vault, "guide.md")
	htmlPath := filepath.Join(vault, "guide.html")
	mustWriteFile(t, mdPath, "# Guide\n")
	mustWriteFile(t, htmlPath, "<!-- RENDER_SOURCE: guide.md -->\n<html>no markers here</html>")

	p := Pair{HTMLPath: htmlPath, MDPath: mdPath, HTMLRel: "guide.html", MDRel: "guide.md"}
	err := Regenerate(p, time.Now())
	if !errors.Is(err, ErrNoMarkers) {
		t.Errorf("err = %v, want ErrNoMarkers", err)
	}
}

// CONTRACT(#119): a second regenerate updates the existing
// RENDER_TIMESTAMP in place instead of duplicating it.
func TestRegenerateUpdatesExistingTimestamp(t *testing.T) {
	vault := t.TempDir()
	mdPath := filepath.Join(vault, "guide.md")
	htmlPath := filepath.Join(vault, "guide.html")
	mustWriteFile(t, mdPath, "# Guide\n")
	mustWriteFile(t, htmlPath, "<!-- RENDER_SOURCE: guide.md -->\n<!-- RENDER_TIMESTAMP: 2020-01-01 -->\n<!-- RENDER_BODY_START --><!-- RENDER_BODY_END -->")

	p := Pair{HTMLPath: htmlPath, MDPath: mdPath, HTMLRel: "guide.html", MDRel: "guide.md"}
	if err := Regenerate(p, time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	got := mustReadFile(t, htmlPath)
	if strings.Count(got, "RENDER_TIMESTAMP") != 1 {
		t.Errorf("expected exactly one RENDER_TIMESTAMP, got: %s", got)
	}
	if !strings.Contains(got, "2026-07-15") || strings.Contains(got, "2020-01-01") {
		t.Errorf("timestamp not updated: %s", got)
	}
}
