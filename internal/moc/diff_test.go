// internal/moc/diff_test.go
package moc

import "testing"

// CONTRACT(#151 reuse, #111): Diff reuses triage.Diff directly rather
// than reimplementing a second diff engine.
func TestDiffReusesTriageDiff(t *testing.T) {
	old := "line1\nline2\n"
	new := "line1\nline2-changed\n"
	lines := Diff(old, new)
	var sawAdd, sawDel bool
	for _, l := range lines {
		if l.Op == '+' && l.Text == "line2-changed" {
			sawAdd = true
		}
		if l.Op == '-' && l.Text == "line2" {
			sawDel = true
		}
	}
	if !sawAdd || !sawDel {
		t.Errorf("Diff(%q, %q) = %+v, missing expected +/- lines", old, new, lines)
	}
}

// Identical input produces only context lines, no +/- lines — matches
// triage.Diff's own contract (internal/triage/diff_test.go's
// TestDiffIdenticalInputNoChanges): DiffMain still emits an Equal diff
// spanning the unchanged text, so the result is non-empty context, not
// an empty slice (row #111 — the "(no changes proposed)" message is
// cmd/ov's presentation over an all-context Diff, not Diff's own job).
func TestDiffEmptyWhenIdentical(t *testing.T) {
	for _, l := range Diff("same\n", "same\n") {
		if l.Op != ' ' {
			t.Errorf("unexpected change on identical input: %+v", l)
		}
	}
}
