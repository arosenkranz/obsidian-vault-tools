// internal/triage/validate.go
package triage

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/arosenkranz/obsidian-vault-tools/internal/vault"
)

var (
	// ErrBodyPatchRejected is the headline v2 safety fix (row #5): v1's
	// prompt forbade a non-null body_patch, but apply_proposal honored one
	// anyway and the approval display never showed it. v2 rejects it in
	// code, unconditionally.
	ErrBodyPatchRejected = errors.New("triage: body_patch must be null (row #5 enforcement)")
	// ErrLinksToAddRejected is the companion half of row #5.
	ErrLinksToAddRejected = errors.New("triage: links_to_add must be empty (row #5 enforcement)")
	ErrMissingTo          = errors.New("triage: proposal missing 'to'")
	// ErrTargetNotParaRoot is the PARA-root gate (row #97): the target's
	// first vault-relative path component must be a configured PARA root
	// or the inbox, layered on top of vault.ContainPath's pure filesystem
	// containment.
	ErrTargetNotParaRoot = errors.New("triage: target is not inside a configured PARA root or the inbox")
	// ErrFrontmatterPatchInvalid is row #152's fix (and its residual
	// hardening): frontmatter_patch is fully LLM-controlled JSON written
	// to disk via apply.go's renderPatchValue/mergeFrontmatter with zero
	// newline sanitization. renderPatchValue stringifies ANY value via
	// fmt.Sprint — including a nested map[string]any or nested []any —
	// so a key, or the value's *rendered* form once renderPatchValue has
	// run on it, containing "\n"/"\r" can prematurely close the
	// frontmatter fence, turning everything after an injected "---"
	// into unguarded note body: the same outcome row #5's body_patch/
	// links_to_add rejection exists to prevent, via a second channel.
	// A key or rendered value that, once trimmed, is exactly "---" or
	// "..." is rejected too: both are valid YAML/frontmatter fence
	// markers that corrupt the block as a bare standalone line even
	// without an embedded newline.
	ErrFrontmatterPatchInvalid = errors.New("triage: frontmatter_patch contains a newline or a bare fence marker (row #152 enforcement)")
)

// Validate rejects a proposal that violates a hard safety gate — enforced
// in code, never trusted from the LLM's own output (design spec §Safety
// item 1, §LLM subsystem "Prompt injection posture"). Every gate here is
// re-run by Apply against fresh disk content at write time; Validate
// itself never touches the filesystem for writes, only resolves p.To
// through vault.ContainPath for the containment check.
func Validate(cfg Config, p Proposal) error {
	if p.BodyPatch != nil {
		return ErrBodyPatchRejected
	}
	if len(p.LinksToAdd) > 0 {
		return ErrLinksToAddRejected
	}
	if err := validateFrontmatterPatch(p.FrontmatterPatch); err != nil {
		return err
	}
	if strings.TrimSpace(p.To) == "" {
		return ErrMissingTo
	}
	targetAbs, err := vault.ContainPath(cfg.VaultDir, p.To)
	if err != nil {
		return err
	}
	// Resolve cfg.VaultDir the same way vault.ContainPath resolves its own
	// root (row #97 fix): ContainPath returns targetAbs built from the
	// *symlink-resolved* root, so computing Rel against the raw,
	// unresolved cfg.VaultDir here would produce a bogus "../.."-style
	// path on any vault dir that passes through a symlink (e.g. macOS
	// /var -> /private/var, common for t.TempDir() and real vault
	// locations like iCloud Drive / CloudStorage paths).
	vaultDirReal, err := filepath.EvalSymlinks(cfg.VaultDir)
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(vaultDirReal, targetAbs)
	if err != nil {
		return err
	}
	first := strings.SplitN(filepath.ToSlash(rel), "/", 2)[0]
	allowed := append(append([]string{}, cfg.ParaRoots()...), cfg.Inbox)
	for _, a := range allowed {
		if first == a {
			return nil
		}
	}
	return fmt.Errorf("%w: %s", ErrTargetNotParaRoot, p.To)
}

// validateFrontmatterPatch is row #152's fix, hardened against the
// residual bypass a security review found in the original version: that
// version re-derived apply.go's supported frontmatter_patch shapes
// (bare string, []any of strings) instead of checking what actually
// gets written to disk, so a nested map[string]any or nested []any
// value — never a bare string itself, but still stringified whole by
// renderPatchValue via fmt.Sprint, embedded newlines and all — sailed
// through unchecked.
//
// The fix: for every key/value pair, render the value with the exact
// same renderPatchValue apply.go's mergeFrontmatter calls at write
// time, then run the newline/bare-fence check against that rendered
// string (and the key). Because this checks the actual bytes that will
// land on disk rather than a re-derived approximation of the input
// shape, it covers every case — top-level string, string list, nested
// map, nested list, mixed list — uniformly, and structurally cannot
// drift out of sync with renderPatchValue again.
func validateFrontmatterPatch(patch map[string]any) error {
	for k, v := range patch {
		if err := checkFrontmatterPatchString(k, k); err != nil {
			return err
		}
		if err := checkFrontmatterPatchString(k, renderPatchValue(v)); err != nil {
			return err
		}
	}
	return nil
}

// checkFrontmatterPatchString rejects a newline/carriage return anywhere
// in s, or s trimmed down to exactly "---" or "...".
func checkFrontmatterPatchString(key, s string) error {
	if strings.ContainsAny(s, "\n\r") {
		return fmt.Errorf("%w: key %q", ErrFrontmatterPatchInvalid, key)
	}
	if trimmed := strings.TrimSpace(s); trimmed == "---" || trimmed == "..." {
		return fmt.Errorf("%w: key %q", ErrFrontmatterPatchInvalid, key)
	}
	return nil
}
