package vault

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCreateNew(t *testing.T) {
	p := filepath.Join(t.TempDir(), "note.md")
	if err := WriteNoteAtomic(p, []byte("hello\n"), ""); err != nil {
		t.Fatal(err)
	}
	content, hash, err := ReadNote(p)
	if err != nil || content != "hello\n" || len(hash) != 64 {
		t.Fatalf("content=%q hash=%q err=%v", content, hash, err)
	}
}

func TestCreateNewRefusesExisting(t *testing.T) {
	p := filepath.Join(t.TempDir(), "note.md")
	os.WriteFile(p, []byte("old"), 0o644)
	err := WriteNoteAtomic(p, []byte("new"), "")
	if !errors.Is(err, ErrExists) {
		t.Fatalf("want ErrExists, got %v", err)
	}
	got, _ := os.ReadFile(p)
	if string(got) != "old" {
		t.Error("original clobbered")
	}
}

func TestConditionalReplace(t *testing.T) {
	p := filepath.Join(t.TempDir(), "note.md")
	WriteNoteAtomic(p, []byte("v1"), "")
	_, hash, _ := ReadNote(p)
	if err := WriteNoteAtomic(p, []byte("v2"), hash); err != nil {
		t.Fatal(err)
	}
	content, _, _ := ReadNote(p)
	if content != "v2" {
		t.Errorf("content = %q", content)
	}
}

// CONTRACT(new in v2, review F1): stale hash = visible refusal, never clobber.
func TestConditionalRefusesStale(t *testing.T) {
	p := filepath.Join(t.TempDir(), "note.md")
	WriteNoteAtomic(p, []byte("v1"), "")
	_, staleHash, _ := ReadNote(p)
	// Simulate Obsidian Sync pulling a newer version between read and write:
	os.WriteFile(p, []byte("synced-change"), 0o644)
	err := WriteNoteAtomic(p, []byte("v2"), staleHash)
	if !errors.Is(err, ErrChangedOnDisk) {
		t.Fatalf("want ErrChangedOnDisk, got %v", err)
	}
	content, _, _ := ReadNote(p)
	if content != "synced-change" {
		t.Error("concurrent edit was clobbered")
	}
}

// Temp files are dot-prefixed (Obsidian ignores dotfiles), live in the target
// dir (same-filesystem rename), and never survive success or failure.
func TestNoTempLeftovers(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "note.md")
	WriteNoteAtomic(p, []byte("x"), "")
	WriteNoteAtomic(p, []byte("y"), "not-a-real-hash") // fails
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".ov-tmp-") {
			t.Errorf("temp leftover: %s", e.Name())
		}
		if e.Name() != "note.md" {
			t.Errorf("unexpected entry: %s", e.Name())
		}
	}
}

func TestConditionalOnMissingFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "gone.md")
	err := WriteNoteAtomic(p, []byte("x"), "deadbeef")
	if err == nil {
		t.Fatal("conditional write to missing file must error")
	}
}
