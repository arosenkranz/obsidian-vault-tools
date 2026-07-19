// internal/render/markdown_test.go
package render

import (
	"strings"
	"testing"
)

// CONTRACT: frontmatter is stripped before conversion — it must never
// leak into the rendered HTML body.
func TestRenderMarkdownBodyStripsFrontmatter(t *testing.T) {
	md := "---\ntype: note\nsecret: do-not-leak\n---\n# Title\n\nSome *text*.\n"
	got, err := RenderMarkdownBody(md)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got, "secret") || strings.Contains(got, "do-not-leak") {
		t.Errorf("frontmatter leaked into rendered body: %s", got)
	}
}

// CONTRACT: goldmark converts headings and emphasis (design spec
// §Architecture: "ov render | port (goldmark)").
func TestRenderMarkdownBodyConvertsBasics(t *testing.T) {
	got, err := RenderMarkdownBody("# Title\n\nSome *text* and **bold**.\n")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "<h1") {
		t.Errorf("expected an <h1>, got: %s", got)
	}
	if !strings.Contains(got, "<em>text</em>") || !strings.Contains(got, "<strong>bold</strong>") {
		t.Errorf("expected emphasis/strong conversion, got: %s", got)
	}
}

// CONTRACT: a note with no frontmatter at all still converts normally.
func TestRenderMarkdownBodyNoFrontmatter(t *testing.T) {
	got, err := RenderMarkdownBody("# Just a title\n\nbody text\n")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "<h1") || !strings.Contains(got, "body text") {
		t.Errorf("got: %s", got)
	}
}
