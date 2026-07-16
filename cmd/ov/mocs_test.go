package main

import (
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
