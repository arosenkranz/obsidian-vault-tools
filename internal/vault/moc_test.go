// internal/vault/moc_test.go
package vault

import (
	"reflect"
	"testing"
	"time"
)

// CONTRACT(#34): a MOC is any MOC*.md, sorted by name.
// CONTRACT(#63): description = first non-#, non-blank line among the first 5.
// DECIDE(#32): item count = parsed body wikilinks (not v1's grep -c of lines).
func TestMOCs(t *testing.T) {
	vault := t.TempDir()
	now := time.Now()
	writeAt(t, vault, "03-Resources/MOC Music.md",
		"# MOC Music\n\n> Everything about music\n\n## Key Notes\n- [[Jazz]]\n- [[Blues|the blues]]\n", now)
	writeAt(t, vault, "03-Resources/MOC Empty.md",
		"# MOC Empty\n\n## Key Notes\n", now)
	got, err := MOCs(vault)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Name != "MOC Empty" || got[1].Name != "MOC Music" {
		t.Fatalf("MOCs names/order = %+v", got)
	}
	if got[1].Description != "> Everything about music" {
		t.Errorf("description = %q", got[1].Description)
	}
	if got[1].ItemCount != 2 {
		t.Errorf("item count = %d, want 2", got[1].ItemCount)
	}
	if got[0].ItemCount != 0 || got[0].Description != "" {
		t.Errorf("empty MOC = %+v, want count 0, no description", got[0])
	}
}

// BUG(fixed)(#1,#2): v1 reported EVERY note orphaned — the found_in_moc flag
// was set in a pipeline subshell and never propagated, and matching was a
// substring grep. v2 parses [[wikilink]] targets and matches by basename.
// DECIDE(#65): scope is Resources + Areas only (Projects/Archive not scanned).
func TestOrphans(t *testing.T) {
	vault := t.TempDir()
	now := time.Now()
	writeAt(t, vault, "03-Resources/MOC Music.md",
		"# MOC Music\n\n## Key Notes\n- [[Jazz]]\n- [[folder/Meeting]]\n", now)
	writeAt(t, vault, "03-Resources/Jazz.md", "# Jazz\n", now)   // linked
	writeAt(t, vault, "03-Resources/Blues.md", "# Blues\n", now) // ORPHAN
	writeAt(t, vault, "02-Areas/Meeting.md", "# Meeting\n", now) // linked via path target basename
	writeAt(t, vault, "02-Areas/Standup.md", "# Standup\n", now) // ORPHAN
	writeAt(t, vault, "01-Projects/Proj.md", "# Proj\n", now)    // not scanned
	got, err := Orphans(vault, []string{"03-Resources", "02-Areas"})
	if err != nil {
		t.Fatal(err)
	}
	var rels []string
	for _, n := range got {
		rels = append(rels, n.Rel)
	}
	want := []string{"02-Areas/Standup.md", "03-Resources/Blues.md"}
	if !reflect.DeepEqual(rels, want) {
		t.Fatalf("orphans = %v, want %v", rels, want)
	}
}

// DECIDE(#126): link matching is case-insensitive — [[jazz]] links Jazz.md.
func TestOrphansCaseInsensitive(t *testing.T) {
	vault := t.TempDir()
	now := time.Now()
	writeAt(t, vault, "03-Resources/MOC X.md", "## Key Notes\n- [[jazz]]\n", now)
	writeAt(t, vault, "03-Resources/Jazz.md", "# Jazz\n", now)
	got, err := Orphans(vault, []string{"03-Resources"})
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range got {
		if n.Name == "Jazz" {
			t.Error("Jazz linked via [[jazz]] must not be orphaned (row #126)")
		}
	}
}
