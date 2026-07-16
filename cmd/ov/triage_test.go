// cmd/ov/triage_test.go
package main

import (
	"bufio"
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTriageSkip(t *testing.T) {
	vaultDir := newVaultFixture(t)
	addNote(t, vaultDir, "00-Inbox/2026-07-15 0800 Foo.md", "# Foo\n", 0)
	cfg, err := resolveConfig("")
	if err != nil {
		t.Fatal(err)
	}
	var errBuf bytes.Buffer
	deps := triageDeps{
		pickFolder: func([]string) (string, error) { t.Fatal("pickFolder should not be called"); return "", nil },
		confirm:    func(string) (bool, error) { t.Fatal("confirm should not be called"); return false, nil },
	}
	if err := runTriage(cfg, bufio.NewReader(strings.NewReader("s\n")), &errBuf, deps); err != nil {
		t.Fatal(err)
	}
	if _, statErr := os.Stat(filepath.Join(vaultDir, "00-Inbox", "2026-07-15 0800 Foo.md")); statErr != nil {
		t.Errorf("skipped note should remain: %v", statErr)
	}
	if !strings.Contains(errBuf.String(), "Skipped") {
		t.Errorf("stderr = %q", errBuf.String())
	}
}

// CONTRACT(#57): Enter (empty choice) triggers the folder picker + move.
func TestTriageMove(t *testing.T) {
	vaultDir := newVaultFixture(t)
	addNote(t, vaultDir, "00-Inbox/2026-07-15 0800 Foo.md", "# Foo\n", 0)
	cfg, _ := resolveConfig("")
	var errBuf bytes.Buffer
	deps := triageDeps{pickFolder: func(folders []string) (string, error) { return "02-Areas", nil }}
	if err := runTriage(cfg, bufio.NewReader(strings.NewReader("\n")), &errBuf, deps); err != nil {
		t.Fatal(err)
	}
	if _, statErr := os.Stat(filepath.Join(vaultDir, "02-Areas", "2026-07-15 0800 Foo.md")); statErr != nil {
		t.Errorf("note should be moved: %v", statErr)
	}
}

// DECIDE(#132): delete requires explicit confirmation.
func TestTriageDeleteConfirmed(t *testing.T) {
	vaultDir := newVaultFixture(t)
	addNote(t, vaultDir, "00-Inbox/2026-07-15 0800 Foo.md", "# Foo\n", 0)
	cfg, _ := resolveConfig("")
	var errBuf bytes.Buffer
	deps := triageDeps{confirm: func(string) (bool, error) { return true, nil }}
	if err := runTriage(cfg, bufio.NewReader(strings.NewReader("d\n")), &errBuf, deps); err != nil {
		t.Fatal(err)
	}
	if _, statErr := os.Stat(filepath.Join(vaultDir, "00-Inbox", "2026-07-15 0800 Foo.md")); !os.IsNotExist(statErr) {
		t.Error("note should be deleted")
	}
}

// DECIDE(#132): declining the confirmation leaves the note untouched.
func TestTriageDeleteDeclined(t *testing.T) {
	vaultDir := newVaultFixture(t)
	addNote(t, vaultDir, "00-Inbox/2026-07-15 0800 Foo.md", "# Foo\n", 0)
	cfg, _ := resolveConfig("")
	var errBuf bytes.Buffer
	deps := triageDeps{confirm: func(string) (bool, error) { return false, nil }}
	if err := runTriage(cfg, bufio.NewReader(strings.NewReader("d\n")), &errBuf, deps); err != nil {
		t.Fatal(err)
	}
	if _, statErr := os.Stat(filepath.Join(vaultDir, "00-Inbox", "2026-07-15 0800 Foo.md")); statErr != nil {
		t.Errorf("declined delete must leave the note: %v", statErr)
	}
}

// CONTRACT(#102): q exits immediately without processing remaining notes.
func TestTriageQuit(t *testing.T) {
	vaultDir := newVaultFixture(t)
	addNote(t, vaultDir, "00-Inbox/2026-07-15 0800 A.md", "# A\n", 0)
	addNote(t, vaultDir, "00-Inbox/2026-07-15 0801 B.md", "# B\n", 0)
	cfg, _ := resolveConfig("")
	var errBuf bytes.Buffer
	deps := triageDeps{pickFolder: func([]string) (string, error) { t.Fatal("should not reach the second note"); return "", nil }}
	if err := runTriage(cfg, bufio.NewReader(strings.NewReader("q\n")), &errBuf, deps); err != nil {
		t.Fatal(err)
	}
	if _, statErr := os.Stat(filepath.Join(vaultDir, "00-Inbox", "2026-07-15 0800 A.md")); statErr != nil {
		t.Errorf("A should remain untouched after quit")
	}
}

// CONTRACT(#102): EOF (Ctrl-D) also quits cleanly — with a real note
// pending, so the read loop genuinely reaches ReadString and hits EOF
// rather than short-circuiting on an empty inbox (found in Task 6 review:
// the original empty-fixture version never exercised the EOF branch).
func TestTriageEOFQuits(t *testing.T) {
	vaultDir := newVaultFixture(t)
	addNote(t, vaultDir, "00-Inbox/2026-07-15 0800 Foo.md", "# Foo\n", 0)
	cfg, _ := resolveConfig("")
	var errBuf bytes.Buffer
	if err := runTriage(cfg, bufio.NewReader(strings.NewReader("")), &errBuf, triageDeps{}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(errBuf.String(), "interrupted") {
		t.Errorf("stderr = %q, want the EOF/interrupted message", errBuf.String())
	}
	if _, statErr := os.Stat(filepath.Join(vaultDir, "00-Inbox", "2026-07-15 0800 Foo.md")); statErr != nil {
		t.Errorf("note must remain untouched after an EOF quit: %v", statErr)
	}
}

// BUG(fixed)(#130): a typed destination cannot escape the vault.
func TestTriageMoveRejectsEscape(t *testing.T) {
	vaultDir := newVaultFixture(t)
	addNote(t, vaultDir, "00-Inbox/2026-07-15 0800 Foo.md", "# Foo\n", 0)
	cfg, _ := resolveConfig("")
	var errBuf bytes.Buffer
	deps := triageDeps{pickFolder: func([]string) (string, error) { return "../../../etc", nil }}
	if err := runTriage(cfg, bufio.NewReader(strings.NewReader("\n")), &errBuf, deps); err != nil {
		t.Fatal(err)
	}
	if _, statErr := os.Stat(filepath.Join(vaultDir, "00-Inbox", "2026-07-15 0800 Foo.md")); statErr != nil {
		t.Errorf("note must remain in inbox after a rejected escape: %v", statErr)
	}
	if !strings.Contains(errBuf.String(), "escapes vault") {
		t.Errorf("stderr = %q, want an escape rejection message", errBuf.String())
	}
}

// BUG(fixed)(#139): a picker result naming an occupied destination refuses
// rather than overwriting.
func TestTriageMoveRefusesExistingDestination(t *testing.T) {
	vaultDir := newVaultFixture(t)
	addNote(t, vaultDir, "00-Inbox/2026-07-15 0800 Foo.md", "new\n", 0)
	addNote(t, vaultDir, "02-Areas/2026-07-15 0800 Foo.md", "old\n", 0)
	cfg, _ := resolveConfig("")
	var errBuf bytes.Buffer
	deps := triageDeps{pickFolder: func([]string) (string, error) { return "02-Areas", nil }}
	if err := runTriage(cfg, bufio.NewReader(strings.NewReader("\n")), &errBuf, deps); err != nil {
		t.Fatal(err)
	}
	got, rerr := os.ReadFile(filepath.Join(vaultDir, "02-Areas", "2026-07-15 0800 Foo.md"))
	if rerr != nil {
		t.Fatal(rerr)
	}
	if !strings.Contains(string(got), "old") {
		t.Error("existing destination note must not be overwritten")
	}
	if _, statErr := os.Stat(filepath.Join(vaultDir, "00-Inbox", "2026-07-15 0800 Foo.md")); statErr != nil {
		t.Error("source note must remain after a refused move")
	}
}
