// internal/moc/apply.go
package moc

import "github.com/arosenkranz/obsidian-vault-tools/internal/vault"

// Apply writes proposed to mocPath via vault.WriteNoteAtomic, conditional
// on expectedHash (the caller re-reads and hashes the file immediately
// before calling Apply, row #106's discipline — same pattern as
// syncMOCLink and triage.Apply). Apply calls Validate(original, proposed)
// itself as its first step — defense in depth: it refuses an unsafe
// proposal even if a caller forgot to call Validate first (design
// spec's "never trust the LLM's own output for a gate" posture,
// mirroring triage.Apply exactly, including the CRITICAL-bug-class
// lesson from phase 3 Task 5, row #152). A proposal identical to
// original is a no-op — nothing is written (row #115); the caller
// (cmd/ov's cleanup loop) independently short-circuits this same case
// BEFORE showing the diff/confirm prompt (row #157), so this branch is
// an unreachable-via-normal-flow backstop, not the primary path.
func Apply(mocPath, original, proposed, expectedHash string) error {
	if err := Validate(original, proposed); err != nil {
		return err
	}
	if proposed == original {
		return nil
	}
	return vault.WriteNoteAtomic(mocPath, []byte(proposed), expectedHash)
}
