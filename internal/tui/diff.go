// internal/tui/diff.go
package tui

import (
	"strings"

	lipgloss "charm.land/lipgloss/v2"
	"github.com/arosenkranz/obsidian-vault-tools/internal/triage"
)

var (
	diffAddStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))  // green
	diffDelStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("196")) // red
	diffCtxStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("240")) // dim
)

// RenderDiff colorizes triage.DiffLines for terminal display: green "+ "
// lines, red "- " lines, dim unmarked context lines. Presentation-only —
// internal/triage owns the diff computation (design spec: shared diff
// data, two presentations, row #151).
func RenderDiff(lines []triage.DiffLine) string {
	if len(lines) == 0 {
		return ""
	}
	var b strings.Builder
	for i, l := range lines {
		if i > 0 {
			b.WriteByte('\n')
		}
		switch l.Op {
		case '+':
			b.WriteString(diffAddStyle.Render("+ " + l.Text))
		case '-':
			b.WriteString(diffDelStyle.Render("- " + l.Text))
		default:
			b.WriteString(diffCtxStyle.Render("  " + l.Text))
		}
	}
	return b.String()
}
