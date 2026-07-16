package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// CONTRACT(#60): inbox count, modified-this-week (Archive excluded), projects,
// MOCs. DECIDE(#124): inbox count unified with `inbox` (top-level). DECIDE(#61):
// modified excludes the configured Archive. DECIDE(#123): chrome to stderr.
func TestReviewSummary(t *testing.T) {
	vault := newVaultFixture(t)
	addNote(t, vault, "00-Inbox/a.md", "x", 1)
	addNote(t, vault, "00-Inbox/b.md", "x", 1)
	addNote(t, vault, "01-Projects/Proj One.md", "x", 1)
	if err := os.MkdirAll(filepath.Join(vault, "01-Projects", "Big Project"), 0o755); err != nil {
		t.Fatal(err)
	}
	addNote(t, vault, "02-Areas/Recent.md", "x", 2)
	addNote(t, vault, "02-Areas/Old.md", "x", 40)
	addNote(t, vault, "04-Archive/Archived.md", "x", 1)
	addNote(t, vault, "03-Resources/MOC Music.md", "## Key Notes\n- [[Jazz]]\n", 1)
	out, errs, err := runCmd(t, "review")
	if err != nil {
		t.Fatalf("%v\n%s", err, errs)
	}
	if !strings.Contains(errs, "Weekly Review") || !strings.Contains(errs, "Next steps") {
		t.Errorf("chrome must be on stderr, got %q", errs)
	}
	if !strings.Contains(out, "inbox\t2\n") {
		t.Errorf("want inbox count 2, got %q", out)
	}
	if !strings.Contains(out, "modified\tRecent\n") {
		t.Errorf("Recent should be modified-this-week: %q", out)
	}
	if strings.Contains(out, "modified\tArchived") || strings.Contains(out, "modified\tOld") {
		t.Errorf("Archive/old notes must not appear in modified: %q", out)
	}
	if !strings.Contains(out, "project\tBig Project\n") || !strings.Contains(out, "project\tProj One\n") {
		t.Errorf("projects missing: %q", out)
	}
	if !strings.Contains(out, "moc\tMOC Music\n") {
		t.Errorf("MOC missing: %q", out)
	}
}
