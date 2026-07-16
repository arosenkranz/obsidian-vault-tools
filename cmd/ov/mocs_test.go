package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// CONTRACT(#63): mocs list shows name + description; count added from the
// resolved row #32 item count. DECIDE(#123): record to stdout, chrome to stderr.
func TestMocsList(t *testing.T) {
	vault := newVaultFixture(t)
	addNote(t, vault, "03-Resources/MOC Music.md",
		"# MOC Music\n\n> All my music notes\n\n## Key Notes\n- [[Jazz]]\n", 1)
	out, errs, err := runCmd(t, "mocs", "list")
	if err != nil {
		t.Fatalf("%v\n%s", err, errs)
	}
	if !strings.Contains(out, "MOC Music\t1\t> All my music notes\n") {
		t.Errorf("stdout = %q, want name\\tcount\\tdescription", out)
	}
	if !strings.Contains(errs, "Maps of Content") {
		t.Errorf("chrome must be on stderr: %q", errs)
	}
}

func TestMocsListEmpty(t *testing.T) {
	newVaultFixture(t)
	out, errs, err := runCmd(t, "mocs", "list")
	if err != nil {
		t.Fatal(err)
	}
	if out != "" {
		t.Errorf("no MOCs → no stdout, got %q", out)
	}
	if !strings.Contains(errs, "No MOCs found") {
		t.Errorf("stderr = %q", errs)
	}
}

// BUG(fixed)(#1,#2): v1 reported EVERY note orphaned; v2 parses MOC links so
// linked notes are excluded. DECIDE(#65): scope is Resources + Areas.
func TestMocsOrphan(t *testing.T) {
	vault := newVaultFixture(t)
	addNote(t, vault, "03-Resources/MOC Music.md", "## Key Notes\n- [[Jazz]]\n", 1)
	addNote(t, vault, "03-Resources/Jazz.md", "# Jazz\n", 1)
	addNote(t, vault, "03-Resources/Blues.md", "# Blues\n", 1)
	addNote(t, vault, "02-Areas/Standup.md", "# Standup\n", 1)
	addNote(t, vault, "01-Projects/Proj.md", "# Proj\n", 1)
	out, errs, err := runCmd(t, "mocs", "orphan")
	if err != nil {
		t.Fatalf("%v\n%s", err, errs)
	}
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	want := []string{"02-Areas/Standup.md", "03-Resources/Blues.md"}
	if !reflect.DeepEqual(lines, want) {
		t.Fatalf("orphans = %v, want %v", lines, want)
	}
	if strings.Contains(out, "Jazz") {
		t.Error("linked Jazz must not be orphaned")
	}
	if strings.Contains(out, "Proj") {
		t.Error("Projects not in orphan scope (row #65)")
	}
	if !strings.Contains(errs, "Orphaned notes") {
		t.Errorf("chrome: %q", errs)
	}
}

// CONTRACT(#64): mocs new creates "MOC <title>.md" in Resources from the
// embedded skeleton; stdout is the vault-relative path (row #123
// discipline). BUG(fixed)(#7,#154): no obsidian://open side effect —
// the only observable outcome is the file and the printed path.
func TestMocsNew(t *testing.T) {
	vaultDir := newVaultFixture(t)
	out, errs, err := runCmd(t, "mocs", "new", "Travel")
	if err != nil {
		t.Fatalf("%v\n%s", err, errs)
	}
	if strings.TrimSpace(out) != "03-Resources/MOC Travel.md" {
		t.Errorf("stdout = %q", out)
	}
	got, rerr := os.ReadFile(filepath.Join(vaultDir, "03-Resources", "MOC Travel.md"))
	if rerr != nil {
		t.Fatal(rerr)
	}
	if !strings.Contains(string(got), "# MOC Travel") || !strings.Contains(string(got), "## Key Notes") {
		t.Errorf("skeleton content missing:\n%s", got)
	}
}

// CONTRACT(#64): empty title errors.
func TestMocsNewEmptyTitleErrors(t *testing.T) {
	newVaultFixture(t)
	_, _, err := runCmd(t, "mocs", "new", "")
	if err == nil {
		t.Fatal("expected an error for empty title")
	}
}

// BUG(fixed)(#153): a title containing "/" cannot escape the Resources
// directory — it is slugified (forbidden chars stripped) before joining.
func TestMocsNewTitleSlugifiedForFilename(t *testing.T) {
	vaultDir := newVaultFixture(t)
	out, _, err := runCmd(t, "mocs", "new", "a/b")
	if err != nil {
		t.Fatal(err)
	}
	rel := strings.TrimSpace(out)
	if strings.Contains(rel, "..") || !strings.HasPrefix(rel, "03-Resources/") {
		t.Fatalf("title escaped Resources: %q", rel)
	}
	if _, statErr := os.Stat(filepath.Join(vaultDir, filepath.FromSlash(rel))); statErr != nil {
		t.Fatalf("file not written inside vault: %v", statErr)
	}
}

// CONTRACT(#99-style): an existing MOC file is refused, never overwritten.
func TestMocsNewRefusesExisting(t *testing.T) {
	vaultDir := newVaultFixture(t)
	addNote(t, vaultDir, "03-Resources/MOC Travel.md", "existing", 0)
	_, _, err := runCmd(t, "mocs", "new", "Travel")
	if err == nil {
		t.Fatal("expected an error for an existing MOC")
	}
}

// CONTRACT(#66): mocs add inserts under "## Key Notes".
func TestMocsAddExistingHeading(t *testing.T) {
	vaultDir := newVaultFixture(t)
	addNote(t, vaultDir, "03-Resources/MOC Music.md", "# MOC Music\n\n## Key Notes\n- [[Old]]\n", 0)
	out, errs, err := runCmd(t, "mocs", "add", "Music", "New Song")
	if err != nil {
		t.Fatalf("%v\n%s", err, errs)
	}
	if strings.TrimSpace(out) != "03-Resources/MOC Music.md" {
		t.Errorf("stdout = %q", out)
	}
	got, _ := os.ReadFile(filepath.Join(vaultDir, "03-Resources", "MOC Music.md"))
	want := "# MOC Music\n\n## Key Notes\n- [[New Song]]\n- [[Old]]\n"
	if string(got) != want {
		t.Errorf("got:\n%q\nwant:\n%q", got, want)
	}
}

// CONTRACT(#66): missing "## Key Notes" appends at EOF, no heading created.
func TestMocsAddMissingHeadingAppendsAtEOF(t *testing.T) {
	vaultDir := newVaultFixture(t)
	addNote(t, vaultDir, "03-Resources/MOC Music.md", "# MOC Music\n\n## Resources\n- [[Foo]]\n", 0)
	_, _, err := runCmd(t, "mocs", "add", "Music", "New Song")
	if err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(filepath.Join(vaultDir, "03-Resources", "MOC Music.md"))
	if !strings.Contains(string(got), "- [[New Song]]\n") || strings.Contains(string(got), "## Key Notes") {
		t.Errorf("got:\n%s", got)
	}
}

// CONTRACT(#47-style): a MOC name that doesn't resolve is an error.
func TestMocsAddUnknownMOCErrors(t *testing.T) {
	newVaultFixture(t)
	_, _, err := runCmd(t, "mocs", "add", "Nonexistent", "Note")
	if err == nil {
		t.Fatal("expected an error")
	}
}

// BUG(fixed)(#155): a note-name containing a newline cannot inject extra
// lines into the MOC.
func TestMocsAddSanitizesNoteName(t *testing.T) {
	vaultDir := newVaultFixture(t)
	addNote(t, vaultDir, "03-Resources/MOC Music.md", "# MOC Music\n\n## Key Notes\n", 0)
	_, _, err := runCmd(t, "mocs", "add", "Music", "Evil]]\n\n## Injected")
	if err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(filepath.Join(vaultDir, "03-Resources", "MOC Music.md"))
	// A raw substring check would false-positive: SanitizeWikilinkText
	// strips the newlines, so the sanitized text legitimately still
	// contains the characters "## Injected" glued onto "Evil" on a single
	// line. The actual contract is that no STANDALONE "## Injected" line
	// was injected into the file.
	for _, line := range strings.Split(string(got), "\n") {
		if line == "## Injected" {
			t.Errorf("note-name injection not sanitized:\n%s", got)
		}
	}
}
