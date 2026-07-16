// internal/triage/mocsync.go
package triage

import (
	"fmt"
	"strings"

	"github.com/arosenkranz/obsidian-vault-tools/internal/vault"
)

// extractMOCName pulls a bare MOC name (e.g. "MOC Music") out of a note's
// moc: frontmatter field, however it happens to have been parsed. Mirrors
// triage_llm.py extract_moc_name_from_frontmatter (row #4's documented
// quirk): `moc: [[MOC Music]]` round-trips through Frontmatter.GetList's
// bracket-list heuristic as a one-item list whose element is the string
// "[MOC Music]" (the outer brackets are consumed as the list delimiter,
// the inner pair survives). This unwraps that quirk as well as a plain
// scalar value.
func extractMOCName(fm *vault.Frontmatter) string {
	if list, ok := fm.GetList("moc"); ok {
		if len(list) == 0 {
			return ""
		}
		return strings.Trim(strings.TrimSpace(list[0]), "[]")
	}
	if raw, ok := fm.Get("moc"); ok {
		return strings.Trim(strings.TrimSpace(raw), "[]")
	}
	return ""
}

// syncMOCLink is best-effort: if note was linked from a MOC at capture
// time and triage just renamed it, update that MOC's entry so the link
// keeps resolving instead of silently breaking. Never returns an error —
// any failure is reported as a warning string, and the caller (Apply) has
// already completed the file move by the time this runs, which must never
// be rolled back for a MOC-sync failure (row #94).
func syncMOCLink(cfg Config, fm *vault.Frontmatter, oldTitle, newTitle string) (synced bool, warning string) {
	if oldTitle == newTitle {
		return false, ""
	}
	mocName := extractMOCName(fm)
	if mocName == "" {
		return false, ""
	}
	moc, err := vault.FindMOCByName(cfg.VaultDir, cfg.Resources, mocName)
	if err != nil {
		return false, fmt.Sprintf("note was linked from %q but that MOC file wasn't found — link not updated, check manually", mocName)
	}
	content, hash, err := vault.ReadNote(moc.Path)
	if err != nil {
		return false, fmt.Sprintf("failed to read MOC %q: %v", mocName, err)
	}
	newContent, changed := vault.RenameMOCLink(content, oldTitle, newTitle)
	if !changed {
		return false, fmt.Sprintf("note was linked from %q but no matching [[%s]] entry was found there — check manually", mocName, oldTitle)
	}
	if err := vault.WriteNoteAtomic(moc.Path, []byte(newContent), hash); err != nil {
		return false, fmt.Sprintf("failed to update MOC %q link after rename: %v", mocName, err)
	}
	return true, ""
}
