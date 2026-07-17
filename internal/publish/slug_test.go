// internal/publish/slug_test.go
package publish

import "testing"

// CONTRACT(#73): published slug is lowercase + spaces->hyphens + strip
// everything but ASCII [a-z0-9-] — a distinct, documented rule from
// vault.Slugify's case-preserving Unicode-aware note-filename policy
// (row #23).
func TestSlug(t *testing.T) {
	cases := []struct{ in, want string }{
		{"My Great Note", "my-great-note"},
		{"already-lower-case", "already-lower-case"},
		{"Weird!!! Chars??? Here###", "weird-chars-here"},
		{"  Leading and trailing  ", "--leading-and-trailing--"},
		{"Tabs\tand\nnewlines", "tabsandnewlines"},
		{"Café Résumé", "caf-rsum"}, // ASCII-only strip, row #73 DECIDE
		{"", ""},
		{"123 Numbers 456", "123-numbers-456"},
	}
	for _, c := range cases {
		if got := Slug(c.in); got != c.want {
			t.Errorf("Slug(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
