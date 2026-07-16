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
