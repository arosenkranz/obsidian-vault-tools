// internal/capture/note_test.go
package capture

import "testing"

// Golden: exact captured-note bytes under an injected clock (design spec
// §Testing strategy tier 2).
func TestBuildNoteGolden(t *testing.T) {
	p := Params{
		Title:   "Test Capture",
		Body:    "first line content\nsecond line",
		Tags:    []string{"a", "b"},
		Source:  "cli",
		Created: "2026-07-15",
	}
	want := "---\ntype: inbox\ncreated: 2026-07-15\nmodified: 2026-07-15\nsource: cli\ntags: [a, b]\n---\n\n# Test Capture\n\nfirst line content\nsecond line\n"
	if got := BuildNote(p); got != want {
		t.Errorf("BuildNote =\n%q\nwant\n%q", got, want)
	}
}

// CONTRACT(#54): a MOC-linked capture gets a footer.
func TestBuildNoteWithMOC(t *testing.T) {
	p := Params{Title: "Linked", Body: "body text", Source: "cli", MOCName: "MOC Music", Created: "2026-07-15"}
	want := "---\ntype: inbox\ncreated: 2026-07-15\nmodified: 2026-07-15\nsource: cli\nmoc: [[MOC Music]]\n---\n\n# Linked\n\nbody text\n\n---\n*Added to [[MOC Music]] on 2026-07-15*\n"
	if got := BuildNote(p); got != want {
		t.Errorf("BuildNote =\n%q\nwant\n%q", got, want)
	}
}

// CONTRACT(#53): the body's first line is dropped when it duplicates the
// title, with or without a leading heading marker; leading blank lines are
// also dropped.
func TestBodyWithoutTitleEchoDropsMatchingFirstLine(t *testing.T) {
	cases := []struct{ body, title, want string }{
		{"Test Capture\nmore content", "Test Capture", "more content"},
		{"# Test Capture\nmore content", "Test Capture", "more content"},
		{"\n\nTest Capture\nmore content", "Test Capture", "more content"},
		{"Something Else\nmore content", "Test Capture", "Something Else\nmore content"},
	}
	for _, c := range cases {
		if got := bodyWithoutTitleEcho(c.body, c.title); got != c.want {
			t.Errorf("bodyWithoutTitleEcho(%q, %q) = %q, want %q", c.body, c.title, got, c.want)
		}
	}
}
