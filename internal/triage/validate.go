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
	// ErrFrontmatterPatchInvalid is row #152's fix: frontmatter_patch is
	// fully LLM-controlled JSON written to disk via vault.Frontmatter.Set
	// with zero newline sanitization. A key or string value containing
	// "\n"/"\r" (or a []any list element containing one) can prematurely
	// close the frontmatter fence when rendered, turning everything after
	// an injected "---" into unguarded note body — the same outcome row
	// #5's body_patch/links_to_add rejection exists to prevent, via a
	// second, previously unchecked channel. A key or value that, once
	// trimmed, is exactly "---" or "..." is rejected too: both are valid
	// YAML/frontmatter fence markers that corrupt the block as a bare
	// standalone line even without an embedded newline.
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

// validateFrontmatterPatch is row #152's fix: frontmatter_patch is fully
// LLM-controlled JSON (map[string]any) that apply.go's mergeFrontmatter/
// renderPatchValue write straight to disk via vault.Frontmatter.Set with
// zero newline sanitization. Any key or string value (including a string
// element inside a []any list value — mirroring renderPatchValue's list
// handling) containing "\n" or "\r" can prematurely close the
// frontmatter fence when rendered, turning everything after an injected
// "---" into unguarded note body. A key or value that, once trimmed, is
// exactly "---" or "..." is rejected too: both are valid YAML/
// frontmatter fence markers that corrupt the block as a bare standalone
// line even without an embedded newline.
func validateFrontmatterPatch(patch map[string]any) error {
	for k, v := range patch {
		if err := checkFrontmatterPatchString(k, k); err != nil {
			return err
		}
		if err := checkFrontmatterPatchValue(k, v); err != nil {
			return err
		}
	}
	return nil
}

// checkFrontmatterPatchValue inspects a single frontmatter_patch value:
// a bare string is checked directly, and a []any list (renderPatchValue's
// other supported shape) has each string element checked in turn.
func checkFrontmatterPatchValue(key string, v any) error {
	if s, ok := v.(string); ok {
		return checkFrontmatterPatchString(key, s)
	}
	if list, ok := v.([]any); ok {
		for _, e := range list {
			if s, ok := e.(string); ok {
				if err := checkFrontmatterPatchString(key, s); err != nil {
					return err
				}
			}
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
