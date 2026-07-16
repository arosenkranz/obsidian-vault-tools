package vault

import (
	"reflect"
	"testing"
)

// DECIDE(#32): v2 counts parsed wikilinks; two links on one line count twice
// (v1 grep -c counted the LINE once). Targets strip alias and heading anchor.
func TestParseWikilinks(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []string
	}{
		{"plain", "see [[Music]]", []string{"Music"}},
		{"alias", "[[Music|my tunes]]", []string{"Music"}},
		{"heading", "[[Music#Jazz]]", []string{"Music"}},
		{"alias and heading", "[[Music#Jazz|jazz]]", []string{"Music"}},
		{"two on one line", "[[A]] and [[B]]", []string{"A", "B"}},
		{"embed counts", "![[Diagram]]", []string{"Diagram"}},
		{"path target", "[[folder/Note]]", []string{"folder/Note"}},
		{"heading only skipped", "[[#Section]]", nil},
		{"empty skipped", "[[]]", nil},
		{"whitespace trimmed", "[[  Spaced  ]]", []string{"Spaced"}},
		{"none", "no links here", nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ParseWikilinks(c.in); !reflect.DeepEqual(got, c.want) {
				t.Errorf("ParseWikilinks(%q) = %v, want %v", c.in, got, c.want)
			}
		})
	}
}

// DECIDE(#126): orphan matching keys links and notes by NFC + case-folded
// basename, mirroring Obsidian's case-insensitive, basename link resolution.
func TestLinkKey(t *testing.T) {
	for in, want := range map[string]string{
		"Music":        "music",
		"folder/Music": "music",
		"MUSIC":        "music",
		"  Music  ":    "music",
	} {
		if got := linkKey(in); got != want {
			t.Errorf("linkKey(%q) = %q, want %q", in, got, want)
		}
	}
	// NFC/NFD: composed and decomposed é key identically.
	if linkKey("Café") != linkKey("Cafe\u0301") {
		t.Errorf("NFC and NFD forms must key identically")
	}
}
