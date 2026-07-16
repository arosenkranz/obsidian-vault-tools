// internal/tui/confirm.go
package tui

import "github.com/charmbracelet/huh"

// Confirm asks a yes/no question on the controlling tty (used by triage
// delete — behavior inventory row #132: v2 requires explicit confirmation
// on every triage path, unlike v1 bash's confirmless delete).
func Confirm(title string) (bool, error) {
	var ok bool
	err := huh.NewConfirm().
		Title(title).
		Affirmative("Delete").
		Negative("Cancel").
		Value(&ok).
		Run()
	return ok, err
}
