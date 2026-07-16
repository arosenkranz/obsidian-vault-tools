// internal/triage/diff_test.go
package triage

import "testing"

// CONTRACT(#151, new in v2): line-level diff, one DiffLine per line,
// tagged '+'/'-'/' '.
func TestDiffAddedAndRemovedLines(t *testing.T) {
	old := "line one\nline two\nline three\n"
	updated := "line one\nline TWO changed\nline three\nline four\n"
	lines := Diff(old, updated)

	var got []string
	for _, l := range lines {
		got = append(got, string(l.Op)+l.Text)
	}
	// Every original line must be accounted for as context or removed, and
	// every new line as context or added — assert presence rather than
	// exact ordering/grouping, which is an implementation detail of the
	// underlying line-diff algorithm.
	mustContain := []string{" line one", "-line two", "+line TWO changed", " line three", "+line four"}
	for _, want := range mustContain {
		found := false
		for _, g := range got {
			if g == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Diff() missing expected line %q; got %v", want, got)
		}
	}
}

// Identical input produces only context lines, no +/- lines.
func TestDiffIdenticalInputNoChanges(t *testing.T) {
	content := "same\ncontent\n"
	lines := Diff(content, content)
	for _, l := range lines {
		if l.Op != ' ' {
			t.Errorf("unexpected change on identical input: %+v", l)
		}
	}
}
