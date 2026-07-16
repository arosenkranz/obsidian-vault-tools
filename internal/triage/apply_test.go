// internal/triage/apply_test.go
package triage

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/arosenkranz/obsidian-vault-tools/internal/vault"
)

func writeInboxNote(t *testing.T, cfg Config, name, content string) vault.Note {
	t.Helper()
	p := filepath.Join(cfg.VaultDir, cfg.Inbox, name)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return vault.Note{Path: p, Rel: cfg.Inbox + "/" + name, Name: name[:len(name)-3]}
}

var fixedNow = time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC)

// CONTRACT(#98, #100): a real apply moves the note, upgrades frontmatter
// (type: inbox dropped, patch applied except created/modified which stay
// script-owned), and writes the new heading/body unchanged (body_patch is
// nil, enforced by Validate inside Apply).
func TestApplyMovesAndMergesFrontmatter(t *testing.T) {
	cfg := testConfig(t)
	note := writeInboxNote(t, cfg, "2026-05-14 0830 thought.md",
		"---\ntype: inbox\ncreated: 2026-05-14\nsource: cli\n---\n# thought\n\nSome idea.\n")
	p := Proposal{
		To:               "02-Areas/Local LLM Notes.md",
		NewTitle:         "Local LLM Notes",
		FrontmatterPatch: map[string]any{"type": "learning", "status": "active"},
		Confidence:       "high",
		Rationale:        "fits",
	}
	res, err := Apply(cfg, note, p, fixedNow, false)
	if err != nil {
		t.Fatal(err)
	}
	if res.Target != "02-Areas/Local LLM Notes.md" {
		t.Errorf("Target = %q", res.Target)
	}
	if _, err := os.Stat(note.Path); !os.IsNotExist(err) {
		t.Error("source note should be removed after a real apply")
	}
	got, err := os.ReadFile(filepath.Join(cfg.VaultDir, "02-Areas", "Local LLM Notes.md"))
	if err != nil {
		t.Fatal(err)
	}
	content := string(got)
	if stringsContains(content, "type: inbox") {
		t.Error("type: inbox should have been dropped")
	}
	if !stringsContains(content, "type: learning") || !stringsContains(content, "status: active") {
		t.Errorf("frontmatter patch not applied:\n%s", content)
	}
	if !stringsContains(content, "created: 2026-05-14") {
		t.Errorf("created should be preserved from the original note:\n%s", content)
	}
	if !stringsContains(content, "modified: 2026-07-15") {
		t.Errorf("modified should be today (fixedNow):\n%s", content)
	}
	if !stringsContains(content, "Some idea.") {
		t.Errorf("body should be unchanged:\n%s", content)
	}
}

// CONTRACT: --dry-run performs every resolution/merge step and returns
// the would-be Result (including rendered Content for diff display)
// without touching the filesystem.
func TestApplyDryRunWritesNothing(t *testing.T) {
	cfg := testConfig(t)
	note := writeInboxNote(t, cfg, "x.md", "---\ntype: inbox\ncreated: 2026-05-14\n---\nbody\n")
	p := Proposal{To: "02-Areas/X.md", NewTitle: "X", FrontmatterPatch: map[string]any{"type": "note"}}
	res, err := Apply(cfg, note, p, fixedNow, true)
	if err != nil {
		t.Fatal(err)
	}
	if res.Target != "02-Areas/X.md" {
		t.Errorf("Target = %q", res.Target)
	}
	if res.Content == "" || !stringsContains(res.Content, "type: note") {
		t.Errorf("dry-run Content not populated correctly: %q", res.Content)
	}
	if _, err := os.Stat(note.Path); err != nil {
		t.Error("dry-run must not remove the source note")
	}
	if _, err := os.Stat(filepath.Join(cfg.VaultDir, "02-Areas", "X.md")); !os.IsNotExist(err) {
		t.Error("dry-run must not write the target note")
	}
}

// BUG(fixed)(#5): Apply refuses a body_patch even if a caller forgot to
// call Validate first — the gate lives inside Apply too (defense in
// depth). Neither the source nor a target file is touched.
func TestApplyRefusesBodyPatchWithoutExternalValidate(t *testing.T) {
	cfg := testConfig(t)
	note := writeInboxNote(t, cfg, "x.md", "---\ntype: inbox\n---\noriginal body\n")
	patch := "INJECTED CONTENT"
	p := Proposal{To: "02-Areas/X.md", NewTitle: "X", BodyPatch: &patch}
	_, err := Apply(cfg, note, p, fixedNow, false)
	if !errors.Is(err, ErrBodyPatchRejected) {
		t.Fatalf("err = %v, want ErrBodyPatchRejected", err)
	}
	if _, statErr := os.Stat(note.Path); statErr != nil {
		t.Error("source note must remain untouched on rejection")
	}
	if _, statErr := os.Stat(filepath.Join(cfg.VaultDir, "02-Areas", "X.md")); !os.IsNotExist(statErr) {
		t.Error("target must never be written when body_patch is rejected")
	}
	if content, _ := os.ReadFile(note.Path); stringsContains(string(content), "INJECTED CONTENT") {
		t.Error("injected content must never reach disk")
	}
}

// CONTRACT(#99): an existing target file is refused, never overwritten —
// mechanically enforced by WriteNoteAtomic's ErrExists (write_test.go
// already covers the primitive; this proves Apply routes through it).
func TestApplyRefusesExistingTarget(t *testing.T) {
	cfg := testConfig(t)
	note := writeInboxNote(t, cfg, "x.md", "---\ntype: inbox\n---\nbody\n")
	if err := os.WriteFile(filepath.Join(cfg.VaultDir, "02-Areas", "X.md"), []byte("already here"), 0o644); err != nil {
		t.Fatal(err)
	}
	p := Proposal{To: "02-Areas/X.md", NewTitle: "X"}
	_, err := Apply(cfg, note, p, fixedNow, false)
	if !errors.Is(err, vault.ErrExists) {
		t.Fatalf("err = %v, want vault.ErrExists", err)
	}
	if _, statErr := os.Stat(note.Path); statErr != nil {
		t.Error("source note must remain when the target already exists")
	}
}

// CONTRACT(#97): Apply itself also enforces the PARA-root gate (not just
// Validate) — a proposal that somehow bypassed an external Validate call
// is still refused.
func TestApplyRefusesNonParaRootTarget(t *testing.T) {
	cfg := testConfig(t)
	note := writeInboxNote(t, cfg, "x.md", "---\ntype: inbox\n---\nbody\n")
	p := Proposal{To: "99-Meta/evil.md", NewTitle: "evil"}
	_, err := Apply(cfg, note, p, fixedNow, false)
	if !errors.Is(err, ErrTargetNotParaRoot) {
		t.Fatalf("err = %v, want ErrTargetNotParaRoot", err)
	}
}

// CONTRACT(#98): a "to" with no filename (directory-style, trailing "/")
// derives its filename from the slugified new_title.
func TestApplyDirectoryTargetDerivesFilename(t *testing.T) {
	cfg := testConfig(t)
	note := writeInboxNote(t, cfg, "x.md", "---\ntype: inbox\n---\nbody\n")
	p := Proposal{To: "02-Areas/", NewTitle: "My New Title"}
	res, err := Apply(cfg, note, p, fixedNow, false)
	if err != nil {
		t.Fatal(err)
	}
	if res.Target != "02-Areas/My New Title.md" {
		t.Errorf("Target = %q", res.Target)
	}
}

// CONTRACT(#94-96): when the title changes and the note was linked from a
// MOC at capture time (moc: frontmatter), Apply syncs that MOC's
// [[old]] -> [[new]] entry, best-effort.
func TestApplySyncsMOCLinkOnRename(t *testing.T) {
	cfg := testConfig(t)
	mocPath := filepath.Join(cfg.VaultDir, cfg.Resources, "MOC Music.md")
	if err := os.WriteFile(mocPath, []byte("# MOC Music\n\n- [[Old Thought]] — a tune\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	note := writeInboxNote(t, cfg, "Old Thought.md", "---\ntype: inbox\nmoc: [[MOC Music]]\n---\nbody\n")
	p := Proposal{To: "02-Areas/", NewTitle: "New Thought"}
	res, err := Apply(cfg, note, p, fixedNow, false)
	if err != nil {
		t.Fatal(err)
	}
	if !res.MOCSynced {
		t.Error("expected MOCSynced=true")
	}
	mocContent, err := os.ReadFile(mocPath)
	if err != nil {
		t.Fatal(err)
	}
	if !stringsContains(string(mocContent), "[[New Thought]]") || stringsContains(string(mocContent), "[[Old Thought]]") {
		t.Errorf("MOC not synced: %s", mocContent)
	}
}

// CONTRACT(#94): a missing MOC file is a warning, never an abort — the
// completed move is not rolled back.
func TestApplyMOCSyncMissingMOCWarnsNeverAborts(t *testing.T) {
	cfg := testConfig(t)
	note := writeInboxNote(t, cfg, "Old Thought.md", "---\ntype: inbox\nmoc: [[MOC Nonexistent]]\n---\nbody\n")
	p := Proposal{To: "02-Areas/", NewTitle: "New Thought"}
	res, err := Apply(cfg, note, p, fixedNow, false)
	if err != nil {
		t.Fatal(err)
	}
	if res.MOCWarning == "" {
		t.Error("expected a MOCWarning")
	}
	if _, statErr := os.Stat(filepath.Join(cfg.VaultDir, "02-Areas", "New Thought.md")); statErr != nil {
		t.Error("the move must have completed despite the MOC sync failure")
	}
}

// A title that does not change never touches any MOC (row #94: "runs only
// when the title changed").
func TestApplyNoMOCSyncWhenTitleUnchanged(t *testing.T) {
	cfg := testConfig(t)
	mocPath := filepath.Join(cfg.VaultDir, cfg.Resources, "MOC Music.md")
	original := "# MOC Music\n\n- [[Same Title]] — x\n"
	if err := os.WriteFile(mocPath, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	note := writeInboxNote(t, cfg, "Same Title.md", "---\ntype: inbox\nmoc: [[MOC Music]]\n---\nbody\n")
	p := Proposal{To: "02-Areas/", NewTitle: "Same Title"}
	res, err := Apply(cfg, note, p, fixedNow, false)
	if err != nil {
		t.Fatal(err)
	}
	if res.MOCSynced {
		t.Error("expected no MOC sync when the title did not change")
	}
	got, _ := os.ReadFile(mocPath)
	if string(got) != original {
		t.Errorf("MOC was modified despite an unchanged title: %s", got)
	}
}
