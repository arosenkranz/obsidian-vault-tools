// internal/triage/apply.go
package triage

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/arosenkranz/obsidian-vault-tools/internal/vault"
)

// Result reports what Apply did (or, under dryRun, would do).
type Result struct {
	// Target is the vault-relative path the note was (or would be)
	// written to.
	Target string
	// Content is the full rendered note content Apply wrote (or would
	// write) at Target — populated in both real and dry-run modes so
	// callers can render a diff against the note's original content
	// (design spec: diff-confirm before any apply, row #151).
	Content string
	// MOCSynced reports whether a MOC entry's wikilink title was renamed
	// (rows #94-96). Never set under dryRun (no filesystem write occurs).
	MOCSynced bool
	// MOCWarning is set when a title change should have synced a MOC
	// entry but couldn't (missing MOC, missing entry, or a write
	// failure) — never fatal (row #94).
	MOCWarning string
}

// Apply merges the proposal's frontmatter_patch into the note per
// AGENTS.md §7 field rules (row #100), resolves and writes the target
// (rows #97-99), removes the source, and best-effort syncs the source
// MOC's wikilink title (rows #94-96). Apply calls Validate itself as its
// first step — defense in depth: it refuses unsafe input even if a
// caller forgot to call Validate first (design spec §Safety item 1).
// Content is always read fresh from note.Path (never a snapshot Propose
// captured earlier) so a concurrent Obsidian Sync edit is reflected,
// per design spec's conditional-write principle. dryRun performs every
// resolution/merge step and returns the would-be Result without touching
// the filesystem — the CLI's --dry-run and both frontends' diff-confirm
// screens call Apply(..., dryRun=true) to preview exactly what the real
// apply (dryRun=false) would do, using the identical code path.
func Apply(cfg Config, note vault.Note, p Proposal, now time.Time, dryRun bool) (Result, error) {
	if err := Validate(cfg, p); err != nil {
		return Result{}, err
	}

	content, _, err := vault.ReadNote(note.Path)
	if err != nil {
		return Result{}, err
	}
	fm, body := vault.ParseNote(content)
	oldTitle := strings.TrimSuffix(filepath.Base(note.Path), ".md")

	targetAbs, err := vault.ContainPath(cfg.VaultDir, p.To)
	if err != nil {
		return Result{}, err
	}

	newTitle := strings.TrimSpace(p.NewTitle)
	if newTitle == "" {
		newTitle = oldTitle
	} else {
		newTitle = vault.Slugify(newTitle, 80)
	}

	// Directory-style target (trailing "/" or an existing directory) gets
	// a filename derived from the slugified new_title (row #98).
	isDirTarget := strings.HasSuffix(p.To, "/")
	if !isDirTarget {
		if info, statErr := os.Stat(targetAbs); statErr == nil && info.IsDir() {
			isDirTarget = true
		}
	}
	if isDirTarget {
		targetAbs = filepath.Join(targetAbs, newTitle+".md")
	} else if filepath.Ext(targetAbs) == "" {
		targetAbs += ".md"
	}

	rel, err := filepath.Rel(cfg.VaultDir, targetAbs)
	if err != nil {
		return Result{}, err
	}
	relSlash := filepath.ToSlash(rel)

	newFM := mergeFrontmatter(fm, p.FrontmatterPatch, now)
	// body_patch is guaranteed nil by Validate above — body stays as-is.
	newContent := newFM.Render() + strings.TrimPrefix(body, "\n")

	res := Result{Target: relSlash, Content: newContent}

	if dryRun {
		return res, nil
	}

	if err := os.MkdirAll(filepath.Dir(targetAbs), 0o755); err != nil {
		return Result{}, err
	}
	if err := vault.WriteNoteAtomic(targetAbs, []byte(newContent), ""); err != nil {
		return Result{}, err
	}
	if err := os.Remove(note.Path); err != nil {
		return Result{}, fmt.Errorf("note written to %s but the source could not be removed: %w", relSlash, err)
	}

	synced, warning := syncMOCLink(cfg, fm, oldTitle, newTitle)
	res.MOCSynced = synced
	res.MOCWarning = warning

	return res, nil
}

// mergeFrontmatter applies AGENTS.md §7's frontmatter merge rule (row
// #100): type: inbox is dropped; the patch is applied to every key except
// created/modified (script-owned); created is preserved if already
// present, else set to now; modified is always set to now. Patch keys are
// applied in sorted order so multiple newly-introduced keys append to the
// frontmatter block deterministically (Go map iteration order is
// randomized; the frontmatter_patch field itself decodes from JSON into
// an unordered map[string]any).
func mergeFrontmatter(fm *vault.Frontmatter, patch map[string]any, now time.Time) *vault.Frontmatter {
	if fm == nil {
		fm = vault.NewFrontmatter()
	}
	if v, ok := fm.Get("type"); ok && v == "inbox" {
		fm.Delete("type")
	}
	keys := make([]string, 0, len(patch))
	for k := range patch {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if k == "created" || k == "modified" {
			continue
		}
		fm.Set(k, renderPatchValue(patch[k]))
	}
	if _, ok := fm.Get("created"); !ok {
		fm.Set("created", now.Format("2006-01-02"))
	}
	fm.Set("modified", now.Format("2006-01-02"))
	return fm
}

// renderPatchValue renders one frontmatter_patch JSON value into the raw
// scalar/flow-list text Frontmatter.Set expects, mirroring
// triage_llm.py's _fm_line list handling.
func renderPatchValue(v any) string {
	if list, ok := v.([]any); ok {
		strs := make([]string, 0, len(list))
		for _, e := range list {
			strs = append(strs, fmt.Sprint(e))
		}
		return "[" + strings.Join(strs, ", ") + "]"
	}
	return fmt.Sprint(v)
}
