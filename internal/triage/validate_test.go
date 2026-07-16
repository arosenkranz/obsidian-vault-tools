// internal/triage/validate_test.go
package triage

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func testConfig(t *testing.T) Config {
	t.Helper()
	// EvalSymlinks the temp dir up front (macOS resolves /var to
	// /private/var) so it matches vault.ContainPath's own symlink-resolved
	// return value — same convention as vault package's own
	// mustEvalSymlinks test helper (internal/vault/contain_test.go).
	vaultDir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range []string{"00-Inbox", "01-Projects", "02-Areas", "03-Resources", "04-Archive", "99-Meta"} {
		if err := os.MkdirAll(filepath.Join(vaultDir, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return Config{
		VaultDir:  vaultDir,
		Inbox:     "00-Inbox",
		Projects:  "01-Projects",
		Areas:     "02-Areas",
		Resources: "03-Resources",
		Archive:   "04-Archive",
	}
}

func validProposal() Proposal {
	return Proposal{
		From:             "00-Inbox/2026-05-14 0830 thought.md",
		To:               "02-Areas/Local LLM Notes.md",
		NewTitle:         "Local LLM Notes",
		FrontmatterPatch: map[string]any{"type": "learning"},
		BodyPatch:        nil,
		LinksToAdd:       nil,
		Rationale:        "fits areas/learning",
		Confidence:       "high",
	}
}

// CONTRACT(#97): a well-formed proposal targeting a configured PARA root
// passes.
func TestValidateAccepts(t *testing.T) {
	cfg := testConfig(t)
	if err := Validate(cfg, validProposal()); err != nil {
		t.Fatalf("Validate() = %v, want nil", err)
	}
}

// BUG(fixed)(#5): the headline safety fix — a non-null body_patch is
// REJECTED, never silently applied. v1's prompt forbade this but
// apply_proposal honored it anyway and the approval display never showed
// it.
func TestValidateRejectsBodyPatch(t *testing.T) {
	cfg := testConfig(t)
	p := validProposal()
	patch := "rewritten body content"
	p.BodyPatch = &patch
	err := Validate(cfg, p)
	if !errors.Is(err, ErrBodyPatchRejected) {
		t.Errorf("Validate() = %v, want ErrBodyPatchRejected", err)
	}
}

// BUG(fixed)(#5): a non-empty links_to_add is REJECTED.
func TestValidateRejectsLinksToAdd(t *testing.T) {
	cfg := testConfig(t)
	p := validProposal()
	p.LinksToAdd = []string{"[[MOC Local LLM]]"}
	err := Validate(cfg, p)
	if !errors.Is(err, ErrLinksToAddRejected) {
		t.Errorf("Validate() = %v, want ErrLinksToAddRejected", err)
	}
}

// CONTRACT(#97): the PARA-root gate is a SEMANTIC check layered on top of
// vault.ContainPath's pure filesystem containment — a path can stay
// inside the vault (99-Meta is a real, contained folder) yet still fail
// this check because it isn't a configured PARA root or the inbox.
func TestValidateRejectsNonParaRootTarget(t *testing.T) {
	cfg := testConfig(t)
	p := validProposal()
	p.To = "99-Meta/evil.md"
	err := Validate(cfg, p)
	if !errors.Is(err, ErrTargetNotParaRoot) {
		t.Errorf("Validate() = %v, want ErrTargetNotParaRoot", err)
	}
}

// BUG(fixed)(#6, #130): a target escaping the vault entirely (traversal)
// is rejected via vault.ContainPath, surfaced through Validate.
func TestValidateRejectsEscapingTarget(t *testing.T) {
	cfg := testConfig(t)
	p := validProposal()
	p.To = "../../etc/passwd"
	if err := Validate(cfg, p); err == nil {
		t.Fatal("expected rejection of an escaping target")
	}
}

// CONTRACT(#97): the inbox itself is an allowed target first-component
// (a proposal can leave a note in the inbox, e.g. after only a
// frontmatter-only tidy — mirrors v1's PARA_ROOTS + [inbox_name]).
func TestValidateAllowsInboxTarget(t *testing.T) {
	cfg := testConfig(t)
	p := validProposal()
	p.To = "00-Inbox/Still Undecided.md"
	if err := Validate(cfg, p); err != nil {
		t.Errorf("Validate() = %v, want nil (inbox is an allowed root)", err)
	}
}

func TestValidateRejectsMissingTo(t *testing.T) {
	cfg := testConfig(t)
	p := validProposal()
	p.To = ""
	if err := Validate(cfg, p); !errors.Is(err, ErrMissingTo) {
		t.Errorf("Validate() = %v, want ErrMissingTo", err)
	}
}

// BUG(fixed)(#97): Validate computed filepath.Rel(cfg.VaultDir, targetAbs)
// against the RAW, unresolved cfg.VaultDir, while targetAbs (returned by
// vault.ContainPath) is built from ContainPath's own *symlink-resolved*
// root. On any vault dir that passes through a symlink — e.g. macOS
// /var -> /private/var, which t.TempDir() hits naturally, and real vault
// locations like iCloud Drive / CloudStorage — this produced a bogus
// "../../private/var/..."-style relative path, so Validate spuriously
// rejected a perfectly valid PARA-root target with ErrTargetNotParaRoot.
// Unlike testConfig (which pre-resolves t.TempDir() via EvalSymlinks to
// route around this exact bug), this test deliberately leaves VaultDir
// UNRESOLVED to reproduce and prove the fix.
func TestValidateHandlesSymlinkedVaultDir(t *testing.T) {
	vaultDir := t.TempDir() // deliberately NOT EvalSymlinks'd
	for _, d := range []string{"00-Inbox", "01-Projects", "02-Areas", "03-Resources", "04-Archive", "99-Meta"} {
		if err := os.MkdirAll(filepath.Join(vaultDir, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	cfg := Config{
		VaultDir:  vaultDir,
		Inbox:     "00-Inbox",
		Projects:  "01-Projects",
		Areas:     "02-Areas",
		Resources: "03-Resources",
		Archive:   "04-Archive",
	}
	p := validProposal()
	if err := Validate(cfg, p); err != nil {
		t.Errorf("Validate() = %v, want nil (a symlinked VaultDir must not cause a spurious ErrTargetNotParaRoot)", err)
	}
}

// BUG(fixed)(row #152): frontmatter_patch is fully LLM-controlled JSON
// written straight to disk via vault.Frontmatter.Set with zero newline
// sanitization. A value like "ok\n---\n# Injected\nattacker body"
// prematurely closes the frontmatter fence when rendered, turning
// everything after the injected "---" into note BODY — a second,
// unguarded channel to the exact outcome row #5's body_patch/
// links_to_add rejection exists to prevent.
func TestValidateRejectsFrontmatterPatchNewlineInValue(t *testing.T) {
	cfg := testConfig(t)
	p := validProposal()
	p.FrontmatterPatch = map[string]any{
		"status": "ok\n---\n# Injected\nattacker body",
	}
	err := Validate(cfg, p)
	if !errors.Is(err, ErrFrontmatterPatchInvalid) {
		t.Fatalf("Validate() = %v, want ErrFrontmatterPatchInvalid", err)
	}
}

// BUG(fixed)(row #152): the injection channel is not limited to values —
// a key containing a newline is just as capable of corrupting the
// rendered frontmatter block.
func TestValidateRejectsFrontmatterPatchNewlineInKey(t *testing.T) {
	cfg := testConfig(t)
	p := validProposal()
	p.FrontmatterPatch = map[string]any{
		"status\n---\ninjected": "value",
	}
	err := Validate(cfg, p)
	if !errors.Is(err, ErrFrontmatterPatchInvalid) {
		t.Fatalf("Validate() = %v, want ErrFrontmatterPatchInvalid", err)
	}
}

// BUG(fixed)(row #152): a value that is exactly "---" (no embedded
// newline at all) is still rejected — rendered as a bare standalone
// frontmatter line, it is itself a valid YAML/frontmatter fence marker
// that corrupts the block.
func TestValidateRejectsFrontmatterPatchBareFenceValue(t *testing.T) {
	cfg := testConfig(t)
	p := validProposal()
	p.FrontmatterPatch = map[string]any{
		"status": "---",
	}
	err := Validate(cfg, p)
	if !errors.Is(err, ErrFrontmatterPatchInvalid) {
		t.Fatalf("Validate() = %v, want ErrFrontmatterPatchInvalid", err)
	}
}

// CONTRACT(row #152): the fix must have no false positives — a
// well-formed multi-key patch (the kind Propose's own decoded Proposal
// would normally carry) still passes cleanly.
func TestValidateAcceptsNormalFrontmatterPatch(t *testing.T) {
	cfg := testConfig(t)
	p := validProposal()
	p.FrontmatterPatch = map[string]any{
		"type": "note",
		"tags": []any{"a", "b"},
	}
	if err := Validate(cfg, p); err != nil {
		t.Errorf("Validate() = %v, want nil for a well-formed frontmatter_patch", err)
	}
}
