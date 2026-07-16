// internal/moc/diff.go
package moc

import "github.com/arosenkranz/obsidian-vault-tools/internal/triage"

// Diff reuses internal/triage.Diff directly — a presentation-free,
// line-level unified diff already shared by tui and web (design spec
// row #151). Importing internal/triage from internal/moc introduces no
// cycle: internal/triage never imports internal/moc, and neither
// package depends on the other's mutation types (Proposal, Validate,
// Apply are all independently named and typed per package). A thin
// wrapper rather than a bare re-export keeps internal/moc's public
// surface self-contained per the design spec's package listing (design
// spec §Architecture: "Diff(old, new string) []triage.DiffLine").
func Diff(old, new string) []triage.DiffLine {
	return triage.Diff(old, new)
}
