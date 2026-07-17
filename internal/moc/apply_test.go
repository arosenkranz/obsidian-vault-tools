// internal/moc/apply_test.go
package moc

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/arosenkranz/obsidian-vault-tools/internal/vault"
)

func writeMOC(t *testing.T, dir, name, content string) (path, hash string) {
	t.Helper()
	path = filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	_, hash, err := vault.ReadNote(path)
	if err != nil {
		t.Fatal(err)
	}
	return path, hash
}

// CONTRACT(#116): a valid, changed proposal writes via WriteNoteAtomic.
func TestApplyWritesValidProposal(t *testing.T) {
	dir := t.TempDir()
	original := "---\ntype: moc\n---\n# MOC Test\n\n- [[Foo]] — https://example.com/foo\n"
	proposed := "---\ntype: moc\n---\n# MOC Test\n\n### Group\n- [[Foo]] — https://example.com/foo\n"
	path, hash := writeMOC(t, dir, "MOC Test.md", original)

	if err := Apply(path, original, proposed, hash); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != proposed {
		t.Errorf("got:\n%s\nwant:\n%s", got, proposed)
	}
}

// CONTRACT(#115): a proposal identical to the original is a no-op —
// Apply itself never writes, even if called directly (defense-in-depth
// backstop; the primary "unchanged" short-circuit lives in cmd/ov's
// cleanup loop, row #157, before Apply is ever called for this case).
func TestApplyNoOpWhenIdentical(t *testing.T) {
	dir := t.TempDir()
	content := "---\ntype: moc\n---\n# MOC Test\n\n- [[Foo]] — https://example.com/foo\n"
	path, hash := writeMOC(t, dir, "MOC Test.md", content)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	if err := Apply(path, content, content, hash); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Error("Apply must not touch the file when proposed == original")
	}
}

// BUG(fixed)(#107-109): Apply refuses an invalid proposal even if a
// caller forgot to call Validate first — the gate lives inside Apply
// too (defense in depth, mirroring triage.Apply's posture). No write
// occurs.
func TestApplyRefusesInvalidProposalWithoutExternalValidate(t *testing.T) {
	dir := t.TempDir()
	original := "---\ntype: moc\n---\n# MOC Test\n\n- [[Foo]] — https://example.com/foo\n"
	invalid := "---\ntype: moc\nextra: field\n---\n# MOC Test\n\n- [[Foo]] — https://example.com/foo\n"
	path, hash := writeMOC(t, dir, "MOC Test.md", original)

	err := Apply(path, original, invalid, hash)
	if !errors.Is(err, ErrFrontmatterChanged) {
		t.Fatalf("err = %v, want ErrFrontmatterChanged", err)
	}
	got, rerr := os.ReadFile(path)
	if rerr != nil {
		t.Fatal(rerr)
	}
	if string(got) != original {
		t.Error("file must be untouched after a rejected Apply")
	}
}

// CONTRACT(#106): a stale expectedHash (disk changed since it was
// computed) is refused, never silently clobbered.
func TestApplyRefusesStaleHash(t *testing.T) {
	dir := t.TempDir()
	original := "---\ntype: moc\n---\n# MOC Test\n\n- [[Foo]] — https://example.com/foo\n"
	proposed := "---\ntype: moc\n---\n# MOC Test\n\n### Group\n- [[Foo]] — https://example.com/foo\n"
	path, staleHash := writeMOC(t, dir, "MOC Test.md", original)
	// Simulate a concurrent edit after the hash was captured.
	if err := os.WriteFile(path, []byte(original+"\nEdited concurrently.\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := Apply(path, original, proposed, staleHash)
	if !errors.Is(err, vault.ErrChangedOnDisk) {
		t.Fatalf("err = %v, want vault.ErrChangedOnDisk", err)
	}
}
