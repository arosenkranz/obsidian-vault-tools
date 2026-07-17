// cmd/ov/new_test.go
package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// CONTRACT(#59): each type resolves to its fixed destination folder and
// its own template, with {{title}} substituted.
func TestRunNewEachType(t *testing.T) {
	cases := []struct {
		noteType, wantDir, wantHeading string
	}{
		{"project", "01-Projects", "# Project Alpha"},
		{"meeting", "02-Areas/Work", "# Standup"},
		{"learning", "02-Areas/Learning", "# Go Generics"},
		{"general", "00-Inbox", "# Quick Thought"},
	}
	for _, c := range cases {
		t.Run(c.noteType, func(t *testing.T) {
			vaultDir := newVaultFixture(t)
			cfg, err := resolveConfig(vaultDir)
			if err != nil {
				t.Fatal(err)
			}
			title := strings.TrimPrefix(c.wantHeading, "# ")
			rel, err := runNew(cfg, c.noteType, title)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.HasPrefix(rel, c.wantDir+"/") {
				t.Errorf("rel = %q, want prefix %q", rel, c.wantDir)
			}
			got, err := os.ReadFile(filepath.Join(vaultDir, filepath.FromSlash(rel)))
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(got), c.wantHeading) {
				t.Errorf("content = %q, want heading %q", got, c.wantHeading)
			}
		})
	}
}

// CONTRACT(#59): an empty title is an error.
func TestRunNewEmptyTitleErrors(t *testing.T) {
	vaultDir := newVaultFixture(t)
	cfg, err := resolveConfig(vaultDir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runNew(cfg, "project", ""); err == nil {
		t.Fatal("expected an error for an empty title")
	}
}

// An unknown type is an error.
func TestRunNewUnknownTypeErrors(t *testing.T) {
	vaultDir := newVaultFixture(t)
	cfg, err := resolveConfig(vaultDir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runNew(cfg, "bogus", "Title"); err == nil {
		t.Fatal("expected an error for an unknown note type")
	}
}

// BUG(fixed)(#58): the filename comes from vault.Slugify — one rule
// everywhere, closing the third disagreeing slug rule for real.
func TestRunNewFilenameUsesVaultSlugify(t *testing.T) {
	vaultDir := newVaultFixture(t)
	cfg, err := resolveConfig(vaultDir)
	if err != nil {
		t.Fatal(err)
	}
	rel, err := runNew(cfg, "general", "  Weird @@@ Title!!!  ")
	if err != nil {
		t.Fatal(err)
	}
	base := filepath.Base(rel)
	if strings.ContainsAny(base, "@") {
		t.Errorf("filename = %q, forbidden chars survived", base)
	}
}

// CONTRACT: mirrors row #99's family — an existing target refuses,
// never overwrites.
func TestRunNewRefusesExistingTarget(t *testing.T) {
	vaultDir := newVaultFixture(t)
	cfg, err := resolveConfig(vaultDir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runNew(cfg, "general", "Duplicate"); err != nil {
		t.Fatal(err)
	}
	if _, err := runNew(cfg, "general", "Duplicate"); err == nil {
		t.Fatal("expected a refusal on the second create with the same title")
	}
}

// DECIDE (row #7/#154, cited not re-litigated): no auto-open side
// effect exists to test — runNew's only observable effect is the
// created file plus its printed path.
func TestNewCmdPrintsOnlyPath(t *testing.T) {
	vaultDir := newVaultFixture(t)
	stdout, _, err := runCmd(t, "new", "general", "Solo Thought", "--vault", vaultDir)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(stdout) != "00-Inbox/Solo Thought.md" {
		t.Errorf("stdout = %q", stdout)
	}
}
