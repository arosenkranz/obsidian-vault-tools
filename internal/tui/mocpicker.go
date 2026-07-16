// internal/tui/mocpicker.go
package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/arosenkranz/obsidian-vault-tools/internal/vault"
)

// mocPickerState is the pure, tty-free core of the MOC picker. q/esc
// cancels (behavior inventory row #39: MOC selection is always optional,
// and cancelling never fails the calling capture) — the new cursor-list
// paradigm replaces v1 fzf's specific Enter/ESC affordances (row #38
// DECIDE already covers "v2 ships its own TUI picker") with Enter=select,
// q/esc=skip.
type mocPickerState struct {
	mocs      []vault.MOC
	cursor    int
	result    string
	cancelled bool
	done      bool
}

func (s mocPickerState) handleKey(key string) mocPickerState {
	switch key {
	case "q", "esc", "ctrl+c":
		s.cancelled = true
	case "up", "k":
		if s.cursor > 0 {
			s.cursor--
		}
	case "down", "j":
		if s.cursor < len(s.mocs)-1 {
			s.cursor++
		}
	case "enter":
		if len(s.mocs) > 0 {
			s.result = s.mocs[s.cursor].Name
			s.done = true
		} else {
			s.cancelled = true
		}
	}
	return s
}

type mocPickerModel struct{ state mocPickerState }

func newMOCPickerModel(mocs []vault.MOC) mocPickerModel {
	return mocPickerModel{state: mocPickerState{mocs: mocs}}
}

func (m mocPickerModel) Init() tea.Cmd { return nil }

func (m mocPickerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if keyMsg, ok := msg.(tea.KeyPressMsg); ok {
		m.state = m.state.handleKey(keyMsg.String())
		if m.state.done || m.state.cancelled {
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m mocPickerModel) View() tea.View {
	var b strings.Builder
	b.WriteString(headingStyle.Render("Choose MOC to link (q to cancel/skip):"))
	b.WriteString("\n\n")
	for i, moc := range m.state.mocs {
		line := fmt.Sprintf("%s (%d items)", moc.Name, moc.ItemCount)
		if i == m.state.cursor {
			fmt.Fprintf(&b, "%s %s\n", cursorStyle.Render(">"), line)
		} else {
			fmt.Fprintf(&b, "  %s\n", line)
		}
	}
	return tea.NewView(b.String())
}
