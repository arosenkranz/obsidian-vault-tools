// internal/vault/moc_write_test.go
package vault

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// CONTRACT(#42): inserting into an existing "## 🔗 Recent Additions"
// heading places the entry immediately below the heading.
func TestAppendMOCEntryExistingHeading(t *testing.T) {
	content := "# MOC Music\n\n## 🔗 Recent Additions\n- [[Old Entry]] — old\n"
	got := AppendMOCEntry(content, "New Song", "a great tune")
	want := "# MOC Music\n\n## 🔗 Recent Additions\n\n- [[New Song]] — a great tune\n- [[Old Entry]] — old\n"
	if got != want {
		t.Errorf("AppendMOCEntry =\n%q\nwant\n%q", got, want)
	}
}

// CONTRACT(#8,#41): no matching heading -> a new "## 🔗 Recent Additions"
// section is created at EOF (the v2 simplification of v1's emoji-heading
// preference chain).
func TestAppendMOCEntryCreatesHeading(t *testing.T) {
	content := "# MOC Music\n\nSome description.\n"
	got := AppendMOCEntry(content, "New Song", "a great tune")
	want := "# MOC Music\n\nSome description.\n\n## 🔗 Recent Additions\n- [[New Song]] — a great tune\n"
	if got != want {
		t.Errorf("AppendMOCEntry =\n%q\nwant\n%q", got, want)
	}
}

// CONTRACT(#40): entry format is "- [[Title]] — snippet".
func TestAppendMOCEntryFormat(t *testing.T) {
	got := AppendMOCEntry("# X\n", "My Title", "my snippet")
	if want := "- [[My Title]] — my snippet"; !containsLine(got, want) {
		t.Errorf("AppendMOCEntry =\n%q\nwant a line %q", got, want)
	}
}

func containsLine(s, line string) bool {
	for _, l := range splitLines(s) {
		if l == line {
			return true
		}
	}
	return false
}

func splitLines(s string) []string {
	var out []string
	start := 0
	for i, r := range s {
		if r == '\n' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	return out
}

// CONTRACT(#33): accepts "Music" or "MOC Music"; prefers Resources, then
// vault-wide.
func TestFindMOCByName(t *testing.T) {
	vaultDir := t.TempDir()
	resources := filepath.Join(vaultDir, "03-Resources")
	if err := os.MkdirAll(resources, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(resources, "MOC Music.md"), []byte("# MOC Music\n\n> tunes\n\n[[Jazz]]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"Music", "MOC Music"} {
		got, err := FindMOCByName(vaultDir, "03-Resources", name)
		if err != nil {
			t.Fatalf("FindMOCByName(%q): %v", name, err)
		}
		if got.Name != "MOC Music" {
			t.Errorf("Name = %q", got.Name)
		}
	}
}

func TestFindMOCByNameNotFound(t *testing.T) {
	vaultDir := t.TempDir()
	if _, err := FindMOCByName(vaultDir, "03-Resources", "Nope"); err == nil {
		t.Fatal("expected an error for an unknown MOC")
	}
}

// BUG(fixed)(#140): a name containing a path separator is rejected
// outright rather than reaching filepath.Join, closing a traversal read
// primitive.
func TestFindMOCByNameRejectsPathSeparator(t *testing.T) {
	vaultDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(vaultDir, "03-Resources"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := FindMOCByName(vaultDir, "03-Resources", "../../../etc/passwd"); err == nil {
		t.Fatal("expected rejection of a name containing a path separator")
	}
}

// CONTRACT(#33): a vault-wide match outside Resources is still found.
func TestFindMOCByNameVaultWideFallback(t *testing.T) {
	vaultDir := t.TempDir()
	other := filepath.Join(vaultDir, "04-Archive")
	if err := os.MkdirAll(other, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(other, "MOC Old.md"), []byte("# MOC Old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := FindMOCByName(vaultDir, "03-Resources", "Old")
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "MOC Old" {
		t.Errorf("Name = %q", got.Name)
	}
}

// CONTRACT(#96): RenameMOCLink replaces every [[old]] with [[new]] in the
// MOC body only; frontmatter untouched (ported from triage_llm.py
// update_moc_entry_title, tests/test_triage_llm.py:45-104).
func TestRenameMOCLinkReplacesInBody(t *testing.T) {
	content := "---\ntype: moc\n---\n# MOC Music\n\n- [[Old Song]] — a tune\n- [[Other]] — unrelated\n"
	got, changed := RenameMOCLink(content, "Old Song", "New Song")
	if !changed {
		t.Fatal("expected a change")
	}
	want := "---\ntype: moc\n---\n# MOC Music\n\n- [[New Song]] — a tune\n- [[Other]] — unrelated\n"
	if got != want {
		t.Errorf("RenameMOCLink =\n%q\nwant\n%q", got, want)
	}
}

// CONTRACT(#96): the frontmatter block is never touched even if it happens
// to contain the literal wikilink text.
func TestRenameMOCLinkNeverTouchesFrontmatter(t *testing.T) {
	content := "---\nmoc: [[Old Song]]\n---\nbody has [[Old Song]] here\n"
	got, changed := RenameMOCLink(content, "Old Song", "New Song")
	if !changed {
		t.Fatal("expected a change")
	}
	if !strings.Contains(got, "moc: [[Old Song]]") {
		t.Errorf("frontmatter was modified: %q", got)
	}
	if !strings.Contains(got, "body has [[New Song]] here") {
		t.Errorf("body was not renamed: %q", got)
	}
}

// CONTRACT(#96): old==new is a no-op (mirrors v1's early return).
func TestRenameMOCLinkNoOpSameTitle(t *testing.T) {
	content := "# MOC Music\n\n- [[Same]] — x\n"
	got, changed := RenameMOCLink(content, "Same", "Same")
	if changed || got != content {
		t.Errorf("expected no-op, got changed=%v content=%q", changed, got)
	}
}

// CONTRACT(#96): no matching entry -> no-op, changed=false (the note may
// not actually be linked from this MOC — caller reports a warning, never
// an error, per row #94).
func TestRenameMOCLinkNoMatch(t *testing.T) {
	content := "# MOC Music\n\n- [[Unrelated]] — x\n"
	got, changed := RenameMOCLink(content, "Missing", "New")
	if changed || got != content {
		t.Errorf("expected no-op, got changed=%v content=%q", changed, got)
	}
}

// A note with no frontmatter block still renames correctly.
func TestRenameMOCLinkNoFrontmatter(t *testing.T) {
	content := "# MOC Music\n\n- [[Old]] — x\n"
	got, changed := RenameMOCLink(content, "Old", "New")
	if !changed {
		t.Fatal("expected a change")
	}
	if got != "# MOC Music\n\n- [[New]] — x\n" {
		t.Errorf("got %q", got)
	}
}
