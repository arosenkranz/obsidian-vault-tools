// internal/capture/filename_test.go
package capture

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// CONTRACT(#50): a collision probes " (2)", " (3)", ... until free.
func TestNextAvailablePath(t *testing.T) {
	dir := t.TempDir()
	path, name, err := NextAvailablePath(dir, "2026-07-15 0830 Foo", ".md")
	if err != nil {
		t.Fatal(err)
	}
	if name != "2026-07-15 0830 Foo.md" {
		t.Errorf("name = %q", name)
	}
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	path2, name2, err := NextAvailablePath(dir, "2026-07-15 0830 Foo", ".md")
	if err != nil {
		t.Fatal(err)
	}
	if name2 != "2026-07-15 0830 Foo (2).md" {
		t.Errorf("name2 = %q", name2)
	}
	if err := os.WriteFile(path2, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, name3, err := NextAvailablePath(dir, "2026-07-15 0830 Foo", ".md")
	if err != nil {
		t.Fatal(err)
	}
	if name3 != "2026-07-15 0830 Foo (3).md" {
		t.Errorf("name3 = %q", name3)
	}
}

// DECIDE(#51): uniqueness is case-insensitive against a real directory
// listing, filesystem-independent.
func TestNextAvailablePathCaseInsensitive(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "2026-07-15 0830 Foo.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, name, err := NextAvailablePath(dir, "2026-07-15 0830 foo", ".md")
	if err != nil {
		t.Fatal(err)
	}
	if name != "2026-07-15 0830 foo (2).md" {
		t.Errorf("name = %q, want a case-insensitive collision suffix", name)
	}
}

// DECIDE(#137): the collision probe is bounded and errors instead of
// looping forever.
func TestNextAvailablePathBoundedProbe(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "Stem.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	for n := 2; n <= maxCollisionAttempts; n++ {
		name := fmt.Sprintf("Stem (%d).md", n)
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := NextAvailablePath(dir, "Stem", ".md"); err == nil {
		t.Fatal("expected an error once the probe bound is exhausted")
	}
}

// BUG(fixed)(#51, design spec §filename policy "table-tested with case-
// variant and NFD inputs"): an on-disk NFD-form name must still collide
// with an NFC-normalized candidate (found in Task 3 review: lowerNames
// originally NFC-normalized only the candidate, not the on-disk entries).
func TestNextAvailablePathNFDOnDisk(t *testing.T) {
	dir := t.TempDir()
	nfd := "2026-07-15 0830 Cafe\u0301.md" // "Café" as NFD: e + combining acute
	if err := os.WriteFile(filepath.Join(dir, nfd), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, name, err := NextAvailablePath(dir, "2026-07-15 0830 Caf\u00e9", ".md") // NFC form
	if err != nil {
		t.Fatal(err)
	}
	if name != "2026-07-15 0830 Caf\u00e9 (2).md" {
		t.Errorf("name = %q, want an NFD-aware collision suffix", name)
	}
}
