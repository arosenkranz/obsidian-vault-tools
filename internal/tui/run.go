// internal/tui/run.go
package tui

import (
	"errors"
	"os"

	tea "charm.land/bubbletea/v2"
	lipgloss "charm.land/lipgloss/v2"

	"github.com/arosenkranz/obsidian-vault-tools/internal/vault"
)

// ErrCancelled is returned by RunFolderPicker/RunMOCPicker when the user
// quits without choosing.
var ErrCancelled = errors.New("cancelled")

var (
	headingStyle = lipgloss.NewStyle().Bold(true)
	cursorStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("212"))
)

// RunFolderPicker launches an interactive folder picker over folders
// (vault-relative paths, e.g. from vault.ListAllFolders). Renders to
// os.Stderr, never stdout. Bubbletea v2 always opens the controlling tty
// for its own input regardless of the process's stdin redirection, which
// is what keeps a piped capture body safe from the picker (design spec
// §CLI/TUI tty discipline).
func RunFolderPicker(folders []string) (string, error) {
	p := tea.NewProgram(newFolderPickerModel(folders), tea.WithOutput(os.Stderr))
	final, err := p.Run()
	if err != nil {
		return "", err
	}
	m := final.(folderPickerModel)
	if m.state.cancelled {
		return "", ErrCancelled
	}
	return m.state.result, nil
}

// RunMOCPicker launches an interactive MOC picker. See RunFolderPicker for
// the tty/output contract.
func RunMOCPicker(mocs []vault.MOC) (string, error) {
	p := tea.NewProgram(newMOCPickerModel(mocs), tea.WithOutput(os.Stderr))
	final, err := p.Run()
	if err != nil {
		return "", err
	}
	m := final.(mocPickerModel)
	if m.state.cancelled {
		return "", ErrCancelled
	}
	return m.state.result, nil
}
