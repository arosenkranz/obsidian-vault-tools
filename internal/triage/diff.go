// internal/triage/diff.go
package triage

import (
	"strings"

	"github.com/sergi/go-diff/diffmatchpatch"
)

// DiffLine is one line of a unified, line-level diff: Op is '+' (added),
// '-' (removed), or ' ' (context); Text is the line content without a
// trailing newline.
type DiffLine struct {
	Op   byte
	Text string
}

// Diff renders a unified, line-level diff between old and new note
// content (the note before this proposal's frontmatter/move and after).
// Presentation-free — internal/tui colorizes for the terminal,
// internal/web escapes into an HTML diff card (design spec: phase 3 needs
// a diff view for triage approval in both TUI and web; row #151).
func Diff(old, new string) []DiffLine {
	dmp := diffmatchpatch.New()
	a, b, lines := dmp.DiffLinesToChars(old, new)
	diffs := dmp.DiffMain(a, b, false)
	diffs = dmp.DiffCharsToLines(diffs, lines)

	var out []DiffLine
	for _, d := range diffs {
		text := strings.TrimSuffix(d.Text, "\n")
		if text == "" {
			continue
		}
		var op byte = ' '
		switch d.Type {
		case diffmatchpatch.DiffInsert:
			op = '+'
		case diffmatchpatch.DiffDelete:
			op = '-'
		}
		for _, line := range strings.Split(text, "\n") {
			out = append(out, DiffLine{Op: op, Text: line})
		}
	}
	return out
}
