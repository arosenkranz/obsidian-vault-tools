// internal/vault/slugify_test.go
package vault

import (
	"strings"
	"testing"
)

// Mined from vault.sh slugify_title (60) and triage_llm.py slugify_title (80).
// DECIDE(kept): the 60/80 budget split is intentional — python comment says
// "filed notes get a slightly larger budget than inbox".
func TestSlugify(t *testing.T) {
	cases := []struct {
		name   string
		in     string
		maxLen int
		want   string
	}{
		{"empty", "", 60, "Untitled"},
		{"whitespace only", "   ", 60, "Untitled"},
		// CONTRACT: leading markdown heading markers stripped
		{"heading", "## My Note Title", 60, "My Note Title"},
		// CONTRACT: forbidden set includes @&# beyond OS-forbidden chars
		{"forbidden", `a/b\c:d*e?f"g<h>i|j`, 60, "a b c d e f g h i j"},
		{"at-amp-hash", "email@host & #tag", 60, "email host tag"},
		// CONTRACT: whitespace runs collapse, ends trimmed
		{"collapse", "  too   many\tspaces  ", 60, "too many spaces"},
		// CONTRACT: case preserved — never lowercased
		{"case", "My COOL Note", 60, "My COOL Note"},
		// CONTRACT: heading strip may leave nothing
		{"only heading markers", "###", 60, "Untitled"},
		// CONTRACT: word-boundary truncation, no trailing space, <= maxLen
		{"truncate 60", strings.Repeat("word ", 20), 60, strings.TrimSpace(strings.Repeat("word ", 11)) + " word"},
		// NFC normalization (design spec §filename policy): NFD é -> NFC é
		{"nfd to nfc", "Caf\u0065\u0301 Notes", 60, "Caf\u00e9 Notes"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := Slugify(c.in, c.maxLen)
			if got != c.want {
				t.Errorf("Slugify(%q, %d) = %q, want %q", c.in, c.maxLen, got, c.want)
			}
			if len([]rune(got)) > c.maxLen {
				t.Errorf("result exceeds maxLen: %d > %d", len([]rune(got)), c.maxLen)
			}
		})
	}
}

func TestSlugifyBudgets(t *testing.T) {
	long := strings.Repeat("abcde ", 20) // 120 chars
	s60 := Slugify(long, 60)
	s80 := Slugify(long, 80)
	if len([]rune(s60)) > 60 || len([]rune(s80)) > 80 {
		t.Fatalf("budget violated: %d, %d", len([]rune(s60)), len([]rune(s80)))
	}
	if strings.HasSuffix(s60, " ") || strings.HasSuffix(s80, " ") {
		t.Error("no trailing whitespace after truncation")
	}
}
