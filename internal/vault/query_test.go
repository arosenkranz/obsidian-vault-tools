package vault

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// writeAt writes vault/rel with content and sets its mtime to mod. Parents
// are created. Returns the absolute path.
func writeAt(t *testing.T, vault, rel, content string, mod time.Time) string {
	t.Helper()
	p := filepath.Join(vault, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(p, mod, mod); err != nil {
		t.Fatal(err)
	}
	return p
}

// BUG(fixed)(#18): v1 get_file_age returned only 0 or 1 for aged files; v2
// AgeDays reports the real floored day count.
func TestAgeDays(t *testing.T) {
	now := time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		mod  time.Time
		want int
	}{
		{now, 0},
		{now.Add(-23 * time.Hour), 0},
		{now.Add(-24 * time.Hour), 1},
		{now.Add(-10 * 24 * time.Hour), 10},
		{now.Add(24 * time.Hour), 0}, // future clamps to 0
	}
	for _, c := range cases {
		if got := AgeDays(now, c.mod); got != c.want {
			t.Errorf("AgeDays(now, now%+v) = %d, want %d", c.mod.Sub(now), got, c.want)
		}
	}
}

// CONTRACT(#56): inbox lists top-level *.md only, sorted by name.
func TestListInbox(t *testing.T) {
	vault := t.TempDir()
	now := time.Now()
	writeAt(t, vault, "00-Inbox/b note.md", "x", now)
	writeAt(t, vault, "00-Inbox/a note.md", "x", now)
	writeAt(t, vault, "00-Inbox/notes.txt", "x", now)   // not .md
	writeAt(t, vault, "00-Inbox/sub/deep.md", "x", now) // not top-level
	got, err := ListInbox(vault, "00-Inbox")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Name != "a note" || got[1].Name != "b note" {
		t.Fatalf("ListInbox = %+v, want [a note, b note]", got)
	}
	if got[0].Rel != "00-Inbox/a note.md" {
		t.Errorf("Rel = %q, want vault-relative slash path", got[0].Rel)
	}
}

func TestListInboxMissing(t *testing.T) {
	_, err := ListInbox(t.TempDir(), "00-Inbox")
	if !os.IsNotExist(err) {
		t.Errorf("missing inbox must return fs.ErrNotExist, got %v", err)
	}
}

// CONTRACT(#62): stale = notes older than `days`, vault-relative path + age.
// BUG(fixed)(#61): exclusions are by configured NAME (here custom archive),
// not hardcoded literal paths, and apply at any depth.
func TestStale(t *testing.T) {
	vault := t.TempDir()
	now := time.Now()
	old := now.Add(-100 * 24 * time.Hour)
	writeAt(t, vault, "03-Resources/old.md", "x", old)
	writeAt(t, vault, "03-Resources/fresh.md", "x", now.Add(-2*24*time.Hour))
	writeAt(t, vault, "Archive-XX/archived.md", "x", old)        // excluded by name
	writeAt(t, vault, "03-Resources/Daily Notes/d.md", "x", old) // excluded, nested
	got, err := Stale(vault, 90, now, []string{"Archive-XX", "Daily Notes"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Rel != "03-Resources/old.md" {
		t.Fatalf("Stale = %+v, want only 03-Resources/old.md", got)
	}
}

// DECIDE(#125): modified-within sorts newest first, then by name.
func TestModifiedWithin(t *testing.T) {
	vault := t.TempDir()
	now := time.Now()
	writeAt(t, vault, "02-Areas/older.md", "x", now.Add(-3*24*time.Hour))
	writeAt(t, vault, "02-Areas/newer.md", "x", now.Add(-1*24*time.Hour))
	writeAt(t, vault, "02-Areas/stale.md", "x", now.Add(-40*24*time.Hour)) // excluded by age
	writeAt(t, vault, "04-Archive/arch.md", "x", now.Add(-1*24*time.Hour)) // excluded by name
	got, err := ModifiedWithin(vault, 7, now, []string{"04-Archive"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Name != "newer" || got[1].Name != "older" {
		t.Fatalf("ModifiedWithin = %+v, want [newer, older]", got)
	}
}

// CONTRACT(#60): projects lists immediate children — dirs and top-level notes.
func TestListProjects(t *testing.T) {
	vault := t.TempDir()
	now := time.Now()
	writeAt(t, vault, "01-Projects/Solo Note.md", "x", now)
	if err := os.MkdirAll(filepath.Join(vault, "01-Projects", "Big Project"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeAt(t, vault, "01-Projects/notes.txt", "x", now) // ignored
	got, err := ListProjects(vault, "01-Projects")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Name != "Big Project" || !got[0].IsDir {
		t.Fatalf("ListProjects[0] = %+v, want dir Big Project", got)
	}
	if got[1].Name != "Solo Note" || got[1].IsDir {
		t.Fatalf("ListProjects[1] = %+v, want file Solo Note", got)
	}
}

func TestListProjectsMissing(t *testing.T) {
	got, err := ListProjects(t.TempDir(), "01-Projects")
	if err != nil || got != nil {
		t.Errorf("missing projects dir = (%v, %v), want (nil, nil)", got, err)
	}
}

// Walk resilience (design spec §core contracts): an unreadable directory is
// skipped, never fatal; reachable notes still return.
func TestStaleWalkResilient(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root bypasses permission bits")
	}
	vault := t.TempDir()
	now := time.Now()
	old := now.Add(-100 * 24 * time.Hour)
	writeAt(t, vault, "03-Resources/reachable.md", "x", old)
	writeAt(t, vault, "03-Resources/locked/hidden.md", "x", old)
	blocked := filepath.Join(vault, "03-Resources", "locked")
	if err := os.Chmod(blocked, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(blocked, 0o755) })
	got, err := Stale(vault, 90, now, nil)
	if err != nil {
		t.Fatalf("walk must be resilient, got %v", err)
	}
	found := false
	for _, n := range got {
		if n.Name == "reachable" {
			found = true
		}
	}
	if !found {
		t.Errorf("reachable note missing after resilient walk: %+v", got)
	}
}
