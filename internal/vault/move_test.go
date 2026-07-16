// internal/vault/move_test.go
package vault

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestMoveNote(t *testing.T) {
	vaultDir := t.TempDir()
	src := filepath.Join(vaultDir, "00-Inbox", "note.md")
	if err := os.MkdirAll(filepath.Dir(src), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(src, []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	destDir := filepath.Join(vaultDir, "02-Areas")
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := MoveNote(src, destDir)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(destDir, "note.md")
	if got != want {
		t.Errorf("MoveNote = %q, want %q", got, want)
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Error("source should no longer exist")
	}
}

// BUG(fixed)(#139): a plain `mv` would silently overwrite; v2 refuses.
func TestMoveNoteRefusesExisting(t *testing.T) {
	vaultDir := t.TempDir()
	src := filepath.Join(vaultDir, "00-Inbox", "note.md")
	if err := os.MkdirAll(filepath.Dir(src), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(src, []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}
	destDir := filepath.Join(vaultDir, "02-Areas")
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(destDir, "note.md"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := MoveNote(src, destDir); !errors.Is(err, ErrExists) {
		t.Fatalf("expected ErrExists, got %v", err)
	}
	got, err := os.ReadFile(filepath.Join(destDir, "note.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "old" {
		t.Error("existing destination must not be overwritten")
	}
}
