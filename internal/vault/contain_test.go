// internal/vault/contain_test.go
package vault

import (
	"os"
	"path/filepath"
	"testing"
)

// BUG(fixed)(#6,#130): v1 used a string-prefix check that accepts a sibling
// "vault-evil" directory sharing the vault's name as a prefix; v1 bash
// manual triage had no containment check at all.
func TestContainPathRejectsSibling(t *testing.T) {
	parent := t.TempDir()
	vaultDir := filepath.Join(parent, "vault")
	evil := filepath.Join(parent, "vault-evil")
	if err := os.MkdirAll(vaultDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(evil, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := ContainPath(vaultDir, evil); err == nil {
		t.Fatal("expected rejection of a sibling directory sharing a name prefix")
	}
}

func TestContainPathRejectsTraversal(t *testing.T) {
	vaultDir := t.TempDir()
	if _, err := ContainPath(vaultDir, "../../etc/passwd"); err == nil {
		t.Fatal("expected rejection of a traversal path")
	}
}

func TestContainPathRejectsSymlinkedEscape(t *testing.T) {
	parent := t.TempDir()
	vaultDir := filepath.Join(parent, "vault")
	outside := filepath.Join(parent, "outside")
	if err := os.MkdirAll(vaultDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(vaultDir, "escape")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	if _, err := ContainPath(vaultDir, filepath.Join(link, "note.md")); err == nil {
		t.Fatal("expected rejection through a symlink pointing outside the vault")
	}
}

func TestContainPathAllowsSymlinkedSubdirInside(t *testing.T) {
	vaultDir := mustEvalSymlinks(t, t.TempDir())
	real := filepath.Join(vaultDir, "03-Resources")
	if err := os.MkdirAll(real, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(vaultDir, "res-link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	got, err := ContainPath(vaultDir, filepath.Join(link, "note.md"))
	if err != nil {
		t.Fatalf("expected an in-vault symlink target to be allowed: %v", err)
	}
	want := filepath.Join(real, "note.md")
	if got != want {
		t.Errorf("ContainPath = %q, want %q", got, want)
	}
}

// CONTRACT(#37): a not-yet-created nested path is allowed (the folder
// picker creates it on demand).
func TestContainPathAllowsNotYetExisting(t *testing.T) {
	vaultDir := mustEvalSymlinks(t, t.TempDir())
	got, err := ContainPath(vaultDir, "02-Areas/Brand New Folder")
	if err != nil {
		t.Fatalf("expected a new in-vault path to be allowed: %v", err)
	}
	want := filepath.Join(vaultDir, "02-Areas", "Brand New Folder")
	if got != want {
		t.Errorf("ContainPath = %q, want %q", got, want)
	}
}

// CONTRACT(#37): EnsureFolder creates the folder (after ~-trim) and returns
// its absolute path.
func TestEnsureFolderCreatesOnDemand(t *testing.T) {
	vaultDir := mustEvalSymlinks(t, t.TempDir())
	got, err := EnsureFolder(vaultDir, "/02-Areas/New Folder/")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(vaultDir, "02-Areas", "New Folder")
	if got != want {
		t.Errorf("EnsureFolder = %q, want %q", got, want)
	}
	info, err := os.Stat(want)
	if err != nil || !info.IsDir() {
		t.Fatalf("folder not created: %v", err)
	}
}

func TestEnsureFolderRejectsEscape(t *testing.T) {
	vaultDir := t.TempDir()
	if _, err := EnsureFolder(vaultDir, "../outside"); err == nil {
		t.Fatal("expected rejection of an escaping folder")
	}
}

// mustEvalSymlinks resolves symlinks in a t.TempDir() path (on macOS, /var
// is itself a symlink to /private/var) so tests can build "want" paths with
// filepath.Join and compare directly against ContainPath/EnsureFolder's
// symlink-resolved output.
func mustEvalSymlinks(t *testing.T, p string) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(p)
	if err != nil {
		t.Fatal(err)
	}
	return resolved
}
