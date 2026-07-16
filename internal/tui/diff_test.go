// internal/tui/diff_test.go
package tui

import (
	"strings"
	"testing"

	"github.com/arosenkranz/obsidian-vault-tools/internal/triage"
)

// CONTRACT(#151): each diff line is rendered with a leading marker
// matching its Op, so a plain-text terminal (color stripped) still shows
// unambiguous +/- markers.
func TestRenderDiffMarkers(t *testing.T) {
	lines := []triage.DiffLine{
		{Op: ' ', Text: "unchanged"},
		{Op: '+', Text: "added"},
		{Op: '-', Text: "removed"},
	}
	got := RenderDiff(lines)
	for _, want := range []string{"unchanged", "+ added", "- removed"} {
		if !strings.Contains(got, want) {
			t.Errorf("RenderDiff() missing %q; got:\n%s", want, got)
		}
	}
}

func TestRenderDiffEmpty(t *testing.T) {
	if got := RenderDiff(nil); got != "" {
		t.Errorf("RenderDiff(nil) = %q, want empty", got)
	}
}
