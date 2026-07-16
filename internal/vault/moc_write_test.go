// internal/vault/moc_write_test.go
package vault

import (
	"os"
	"path/filepath"
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
