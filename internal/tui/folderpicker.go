// internal/tui/folderpicker.go
package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
)

type pickerMode int

const (
	modeBrowse pickerMode = iota
	modeTyping
)

// folderPickerState is the pure, tty-free core of the folder picker: it
// consumes bubbletea's msg.String() key form and returns the next state.
// Selection ("done"+result) and cancellation are terminal; the bubbletea
// Update method (below) is a two-line adapter that quits the program once
// either is set.
type folderPickerState struct {
	folders   []string
	cursor    int
	mode      pickerMode
	inputVal  string
	result    string
	cancelled bool
	done      bool
}

// handleKey applies one key (bubbletea's msg.String() form: "up", "down",
// "enter", "q", "n", "esc", "backspace", or a single printed rune) to
// state. Browse mode: j/k or arrows move the cursor, enter selects the
// highlighted folder, n switches to typing a brand-new path (behavior
// inventory row #37), q/esc/ctrl+c cancels (row #39: cancel never fails
// the caller). Typing mode: printable runes append, backspace deletes,
// enter confirms the typed path, esc returns to browse.
func (s folderPickerState) handleKey(key string) folderPickerState {
	if s.mode == modeTyping {
		switch key {
		case "esc":
			s.mode = modeBrowse
			s.inputVal = ""
		case "enter":
			if path := strings.TrimSpace(s.inputVal); path != "" {
				s.result = path
				s.done = true
			}
		case "backspace":
			if s.inputVal != "" {
				s.inputVal = s.inputVal[:len(s.inputVal)-1]
			}
		default:
			if len([]rune(key)) == 1 {
				s.inputVal += key
			}
		}
		return s
	}
	switch key {
	case "q", "esc", "ctrl+c":
		s.cancelled = true
	case "up", "k":
		if s.cursor > 0 {
			s.cursor--
		}
	case "down", "j":
		if s.cursor < len(s.folders)-1 {
			s.cursor++
		}
	case "n":
		s.mode = modeTyping
	case "enter":
		if len(s.folders) > 0 {
			s.result = s.folders[s.cursor]
			s.done = true
		}
	}
	return s
}

type folderPickerModel struct {
	state folderPickerState
}

func newFolderPickerModel(folders []string) folderPickerModel {
	return folderPickerModel{state: folderPickerState{folders: folders}}
}

func (m folderPickerModel) Init() tea.Cmd { return nil }

func (m folderPickerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if keyMsg, ok := msg.(tea.KeyPressMsg); ok {
		m.state = m.state.handleKey(keyMsg.String())
		if m.state.done || m.state.cancelled {
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m folderPickerModel) View() tea.View {
	var b strings.Builder
	b.WriteString(headingStyle.Render("Choose destination folder ([n] type a new path, q to cancel):"))
	b.WriteString("\n\n")
	if m.state.mode == modeTyping {
		fmt.Fprintf(&b, "New folder: %s\n", m.state.inputVal)
	} else {
		for i, f := range m.state.folders {
			if i == m.state.cursor {
				fmt.Fprintf(&b, "%s %s\n", cursorStyle.Render(">"), f)
			} else {
				fmt.Fprintf(&b, "  %s\n", f)
			}
		}
	}
	return tea.NewView(b.String())
}
