package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// CONTRACT(#56): inbox lists notes sorted by name. DECIDE(#123): records go to
// stdout, chrome to stderr.
func TestInboxLists(t *testing.T) {
	vault := newVaultFixture(t)
	addNote(t, vault, "00-Inbox/2026-01-01 Old Note.md", "x", 30)
	addNote(t, vault, "00-Inbox/2026-07-14 Fresh.md", "x", 1)
	out, errs, err := runCmd(t, "inbox")
	if err != nil {
		t.Fatalf("err: %v\nstderr: %s", err, errs)
	}
	if !strings.Contains(errs, "Inbox contents") {
		t.Errorf("header must be on stderr, got %q", errs)
	}
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("want 2 stdout records, got %d: %q", len(lines), out)
	}
	if lines[0] != "2026-01-01 Old Note\t30" {
		t.Errorf("line 0 = %q, want name + age", lines[0])
	}
	if lines[1] != "2026-07-14 Fresh\t1" {
		t.Errorf("line 1 = %q, want name + age", lines[1])
	}
}

// CONTRACT(#19): notes older than 7 days marked ⚠; age exactly 7 is not a
// warning (strictly age > 7).
func TestAgeMarker(t *testing.T) {
	if got := ageMarker(8); got != "⚠" {
		t.Errorf("ageMarker(8) = %q, want ⚠", got)
	}
	if got := ageMarker(7); got != "•" {
		t.Errorf("ageMarker(7) = %q, want •", got)
	}
	if got := ageMarker(0); got != "•" {
		t.Errorf("ageMarker(0) = %q, want •", got)
	}
}

func TestInboxEmpty(t *testing.T) {
	newVaultFixture(t)
	out, errs, err := runCmd(t, "inbox")
	if err != nil {
		t.Fatal(err)
	}
	if out != "" {
		t.Errorf("empty inbox must produce no stdout records, got %q", out)
	}
	if !strings.Contains(errs, "Inbox is empty") {
		t.Errorf("stderr = %q, want empty-state notice", errs)
	}
}

func TestInboxMissingDir(t *testing.T) {
	vault := newVaultFixture(t)
	os.RemoveAll(filepath.Join(vault, "00-Inbox"))
	out, errs, err := runCmd(t, "inbox")
	if err != nil {
		t.Fatalf("missing inbox must be non-fatal: %v", err)
	}
	if out != "" {
		t.Errorf("stdout = %q, want empty", out)
	}
	if !strings.Contains(errs, "not found") {
		t.Errorf("stderr = %q, want not-found notice", errs)
	}
}
