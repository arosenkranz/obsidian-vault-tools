// internal/render/pair_test.go
package render

import (
	"os"
	"path/filepath"
	"testing"
)

func mustWriteFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// CONTRACT(#117): only HTML files carrying a RENDER_SOURCE comment are
// discovered; unmarked HTML files are invisible to render.
func TestFindPairedFilesDiscoversMarkedHTMLOnly(t *testing.T) {
	vault := t.TempDir()
	mustWriteFile(t, filepath.Join(vault, "guide.md"), "# Guide\n")
	mustWriteFile(t, filepath.Join(vault, "guide.html"),
		"<!-- RENDER_SOURCE: guide.md -->\n<!-- RENDER_BODY_START --><!-- RENDER_BODY_END -->")
	mustWriteFile(t, filepath.Join(vault, "plain.html"), "<html><body>no marker</body></html>")

	pairs, err := FindPairedFiles(vault)
	if err != nil {
		t.Fatal(err)
	}
	if len(pairs) != 1 {
		t.Fatalf("got %d pairs, want 1: %+v", len(pairs), pairs)
	}
	if pairs[0].HTMLRel != "guide.html" || pairs[0].MDRel != "guide.md" {
		t.Errorf("pair = %+v", pairs[0])
	}
}

// BUG(fixed): a RENDER_SOURCE value that would traverse outside the
// vault is skipped, not fatal — same containment posture as row #153,
// applied to this READ path.
func TestFindPairedFilesSkipsTraversalUnsafeSource(t *testing.T) {
	vault := t.TempDir()
	mustWriteFile(t, filepath.Join(vault, "evil.html"), "<!-- RENDER_SOURCE: ../../etc/passwd -->")

	pairs, err := FindPairedFiles(vault)
	if err != nil {
		t.Fatal(err)
	}
	if len(pairs) != 0 {
		t.Errorf("expected traversal-unsafe RENDER_SOURCE to be skipped, got %+v", pairs)
	}
}

// CONTRACT: results are sorted by HTML vault-relative path (deterministic,
// same discipline as row #156's `mocs cleanup --all` ordering).
func TestFindPairedFilesSortedByHTMLRel(t *testing.T) {
	vault := t.TempDir()
	mustWriteFile(t, filepath.Join(vault, "b.md"), "# B\n")
	mustWriteFile(t, filepath.Join(vault, "b.html"), "<!-- RENDER_SOURCE: b.md -->")
	mustWriteFile(t, filepath.Join(vault, "a.md"), "# A\n")
	mustWriteFile(t, filepath.Join(vault, "a.html"), "<!-- RENDER_SOURCE: a.md -->")

	pairs, err := FindPairedFiles(vault)
	if err != nil {
		t.Fatal(err)
	}
	if len(pairs) != 2 || pairs[0].HTMLRel != "a.html" || pairs[1].HTMLRel != "b.html" {
		t.Errorf("pairs not sorted: %+v", pairs)
	}
}

// A missing MD source is a discovery-time non-error — Regenerate is
// where a missing source becomes an error; FindPairedFiles only
// resolves the intended path.
func TestFindPairedFilesToleratesMissingSource(t *testing.T) {
	vault := t.TempDir()
	mustWriteFile(t, filepath.Join(vault, "orphaned.html"), "<!-- RENDER_SOURCE: missing.md -->")

	pairs, err := FindPairedFiles(vault)
	if err != nil {
		t.Fatal(err)
	}
	if len(pairs) != 1 || pairs[0].MDRel != "missing.md" {
		t.Errorf("pairs = %+v", pairs)
	}
}
