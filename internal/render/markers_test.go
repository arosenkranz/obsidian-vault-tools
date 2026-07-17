// internal/render/markers_test.go
package render

import (
	"strings"
	"testing"
)

// CONTRACT(#118): the body between START/END markers is replaced;
// everything outside is preserved byte-for-byte.
func TestSpliceBodyReplacesBetweenMarkers(t *testing.T) {
	html := "<html><head></head><body>\n<!-- RENDER_BODY_START -->old body<!-- RENDER_BODY_END -->\n</body></html>"
	got, err := spliceBody(html, "<p>new</p>")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "<!-- RENDER_BODY_START -->\n<p>new</p>\n<!-- RENDER_BODY_END -->") {
		t.Errorf("body not spliced correctly:\n%s", got)
	}
	if strings.Contains(got, "old body") {
		t.Errorf("old body survived splice:\n%s", got)
	}
	if !strings.HasPrefix(got, "<html><head></head><body>\n") || !strings.HasSuffix(got, "\n</body></html>") {
		t.Errorf("content outside markers was not preserved:\n%s", got)
	}
}

// CONTRACT(#118): a missing START or END marker returns ErrNoMarkers
// ("skip with warning" — the caller decides how to report it).
func TestSpliceBodyMissingMarkersErrors(t *testing.T) {
	for name, html := range map[string]string{
		"no markers at all": "<html><body>plain</body></html>",
		"start only":        "<!-- RENDER_BODY_START -->only start",
		"end only":          "only end<!-- RENDER_BODY_END -->",
		"end before start":  "<!-- RENDER_BODY_END --><!-- RENDER_BODY_START -->",
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := spliceBody(html, "x"); err == nil {
				t.Error("expected ErrNoMarkers, got nil")
			}
		})
	}
}

// BUG(fixed)(#120): a generated HTML body containing sequences that
// look like a regex-replacement backreference — Python's "\1"/"\g<name>"
// AND Go's own "$1"/"${name}" replacement-string syntax — survives the
// splice completely unchanged. This is the dedicated regression test
// for row #120: spliceBody must never pass newBody through ANY
// regexp.*Replace* API as the replacement argument.
func TestSpliceBodyPreservesBackreferenceLookingContent(t *testing.T) {
	html := "<!-- RENDER_BODY_START -->old<!-- RENDER_BODY_END -->"
	body := `Prices: \1 dollars, a group ref \g<name>, and Go-style $1 and ${name} and $$ too.`
	got, err := spliceBody(html, body)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, body) {
		t.Errorf("spliceBody corrupted backreference-looking content:\ngot:  %q\nwant substring: %q", got, body)
	}
}

// CONTRACT(#119): a first render (no existing RENDER_TIMESTAMP comment)
// inserts one immediately after RENDER_SOURCE.
func TestSpliceTimestampInsertsAfterSourceOnFirstRender(t *testing.T) {
	html := "<!-- RENDER_SOURCE: guide.md -->\n<!-- RENDER_BODY_START --><!-- RENDER_BODY_END -->"
	got := spliceTimestamp(html, "<!-- RENDER_TIMESTAMP: 2026-07-15 -->")
	want := "<!-- RENDER_SOURCE: guide.md -->\n<!-- RENDER_TIMESTAMP: 2026-07-15 -->\n<!-- RENDER_BODY_START --><!-- RENDER_BODY_END -->"
	if got != want {
		t.Errorf("spliceTimestamp =\n%q\nwant\n%q", got, want)
	}
}

// CONTRACT(#119): a re-render (existing RENDER_TIMESTAMP comment)
// updates it in place rather than duplicating it.
func TestSpliceTimestampUpdatesExisting(t *testing.T) {
	html := "<!-- RENDER_SOURCE: guide.md -->\n<!-- RENDER_TIMESTAMP: 2020-01-01 -->\nbody"
	got := spliceTimestamp(html, "<!-- RENDER_TIMESTAMP: 2026-07-15 -->")
	want := "<!-- RENDER_SOURCE: guide.md -->\n<!-- RENDER_TIMESTAMP: 2026-07-15 -->\nbody"
	if got != want {
		t.Errorf("spliceTimestamp =\n%q\nwant\n%q", got, want)
	}
	if strings.Count(got, "RENDER_TIMESTAMP") != 1 {
		t.Errorf("expected exactly one RENDER_TIMESTAMP comment, got:\n%s", got)
	}
}
