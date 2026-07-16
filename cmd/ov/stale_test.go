// cmd/ov/stale_test.go
package main

import (
	"strings"
	"testing"
)

// CONTRACT(#62): notes older than the default 90 days, vault-relative + age.
// BUG(fixed)(#61): Archive excluded by configured name; Daily Notes via the
// default stale_exclude. DECIDE(#123): records to stdout, chrome to stderr.
func TestStaleCommand(t *testing.T) {
	vault := newVaultFixture(t)
	addNote(t, vault, "03-Resources/ancient.md", "x", 200)
	addNote(t, vault, "03-Resources/recent.md", "x", 5)
	addNote(t, vault, "04-Archive/old-archived.md", "x", 200)
	addNote(t, vault, "Daily Notes/2020.md", "x", 200)
	out, errs, err := runCmd(t, "stale")
	if err != nil {
		t.Fatalf("%v\n%s", err, errs)
	}
	if strings.Contains(out, "recent") || strings.Contains(out, "Archive") || strings.Contains(out, "Daily Notes") {
		t.Errorf("stdout leaked excluded/fresh notes: %q", out)
	}
	if !strings.HasPrefix(strings.TrimRight(out, "\n"), "03-Resources/ancient.md\t") {
		t.Errorf("stdout = %q, want ancient.md record", out)
	}
	if !strings.Contains(errs, "90+ days") {
		t.Errorf("stderr = %q, want header naming the threshold", errs)
	}
}

func TestStaleCustomDays(t *testing.T) {
	vault := newVaultFixture(t)
	addNote(t, vault, "03-Resources/n.md", "x", 10)
	out, _, err := runCmd(t, "stale", "7")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "03-Resources/n.md") {
		t.Errorf("stale 7 should include a 10-day-old note: %q", out)
	}
	out2, _, _ := runCmd(t, "stale", "30")
	if strings.Contains(out2, "n.md") {
		t.Errorf("stale 30 should exclude a 10-day-old note: %q", out2)
	}
}

func TestStaleBadDays(t *testing.T) {
	newVaultFixture(t)
	if _, _, err := runCmd(t, "stale", "abc"); err == nil {
		t.Error("non-integer days must error")
	}
}
