// cmd/ov/mocs_cleanup_integration_test.go
package main

import (
	"bufio"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/arosenkranz/obsidian-vault-tools/internal/llm"
	"github.com/arosenkranz/obsidian-vault-tools/internal/llmtest"
)

// Temp-vault integration tests (design spec §Testing strategy tier 3):
// OV_LLM_CMD points at this same test binary, re-executed in stub mode
// via internal/llmtest — proving the REAL internal/llm subprocess
// transport, not a fake. Reuses the exact pattern
// triage_llm_integration_test.go established in phase 3.

func newMocCleanupIntegrationRunner(t *testing.T, response string, exitCode int, stderr string) *llm.Runner {
	t.Helper()
	self, err := llmtest.SelfCmd()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv(llmtest.StubEnv, "1")
	t.Setenv(llmtest.ResponseEnv, response)
	if exitCode != 0 {
		t.Setenv(llmtest.ExitCodeEnv, strconv.Itoa(exitCode))
	}
	if stderr != "" {
		t.Setenv(llmtest.StderrEnv, stderr)
	}
	return llm.NewRunner(self, "")
}

// CONTRACT(#116): approve ([y]) writes the proposal, exercised through
// the REAL subprocess transport end to end.
func TestMocCleanupIntegrationApproveWrites(t *testing.T) {
	vaultDir := newVaultFixture(t)
	addNote(t, vaultDir, "03-Resources/MOC Music.md",
		"---\ntype: moc\n---\n# MOC Music\n\n- [[Jazz]] — https://example.com/jazz\n", 0)
	runner := newMocCleanupIntegrationRunner(t,
		`{"new_content":"---\ntype: moc\n---\n# MOC Music\n\n### Example.com\n- [[Jazz]] — https://example.com/jazz\n","duplicates_flagged":[],"summary":"grouped"}`,
		0, "")
	cfg, err := resolveConfig("")
	if err != nil {
		t.Fatal(err)
	}
	var errBuf strings.Builder
	deps := mocCleanupDeps{runner: runner}
	if err := runMocsCleanup(cfg, "Music", false, bufio.NewReader(strings.NewReader("y\n")), &errBuf, &errBuf, deps); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(vaultDir, "03-Resources", "MOC Music.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), "### Example.com") {
		t.Errorf("expected the applied reorganization, got:\n%s", got)
	}
}

// BUG(fixed)(#108): frontmatter mutation is rejected through the real
// transport — the file is never touched.
func TestMocCleanupIntegrationRejectsFrontmatterMutation(t *testing.T) {
	vaultDir := newVaultFixture(t)
	original := "---\ntype: moc\n---\n# MOC Music\n\n- [[Jazz]] — https://example.com/jazz\n"
	addNote(t, vaultDir, "03-Resources/MOC Music.md", original, 0)
	runner := newMocCleanupIntegrationRunner(t,
		`{"new_content":"---\ntype: moc\nextra: injected\n---\n# MOC Music\n\n- [[Jazz]] — https://example.com/jazz\n","duplicates_flagged":[],"summary":""}`,
		0, "")
	cfg, err := resolveConfig("")
	if err != nil {
		t.Fatal(err)
	}
	var errBuf strings.Builder
	deps := mocCleanupDeps{runner: runner}
	if err := runMocsCleanup(cfg, "Music", false, bufio.NewReader(strings.NewReader("y\n")), &errBuf, &errBuf, deps); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(errBuf.String(), "rejected") {
		t.Errorf("expected a rejection report, got:\n%s", errBuf.String())
	}
	got, err := os.ReadFile(filepath.Join(vaultDir, "03-Resources", "MOC Music.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != original {
		t.Errorf("file must be untouched after rejection, got:\n%s", got)
	}
}

// BUG(fixed)(#107): a dropped/renamed BARE wikilink is rejected through
// the real transport.
func TestMocCleanupIntegrationRejectsDroppedBareWikilink(t *testing.T) {
	vaultDir := newVaultFixture(t)
	original := "---\ntype: moc\n---\n# MOC Notes\n\n- [[Neovim]] — my editor setup\n"
	addNote(t, vaultDir, "03-Resources/MOC Notes.md", original, 0)
	runner := newMocCleanupIntegrationRunner(t,
		`{"new_content":"---\ntype: moc\n---\n# MOC Notes\n\n- [[Neovim Notes]] — my editor setup\n","duplicates_flagged":[],"summary":""}`,
		0, "")
	cfg, err := resolveConfig("")
	if err != nil {
		t.Fatal(err)
	}
	var errBuf strings.Builder
	deps := mocCleanupDeps{runner: runner}
	if err := runMocsCleanup(cfg, "Notes", false, bufio.NewReader(strings.NewReader("y\n")), &errBuf, &errBuf, deps); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(errBuf.String(), "rejected") {
		t.Errorf("expected a rejection report, got:\n%s", errBuf.String())
	}
	got, err := os.ReadFile(filepath.Join(vaultDir, "03-Resources", "MOC Notes.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != original {
		t.Errorf("file must be untouched after rejection, got:\n%s", got)
	}
}

// BUG(fixed)(#109): a dropped URL is rejected through the real transport.
func TestMocCleanupIntegrationRejectsDroppedURL(t *testing.T) {
	vaultDir := newVaultFixture(t)
	original := "---\ntype: moc\n---\n# MOC Music\n\n- [[Foo]] — https://example.com/foo\n- [[Bar]] — https://example.com/bar\n"
	addNote(t, vaultDir, "03-Resources/MOC Music.md", original, 0)
	runner := newMocCleanupIntegrationRunner(t,
		`{"new_content":"---\ntype: moc\n---\n# MOC Music\n\n- [[Foo]] — https://example.com/foo\n- [[Bar]] — no url now\n","duplicates_flagged":[],"summary":""}`,
		0, "")
	cfg, err := resolveConfig("")
	if err != nil {
		t.Fatal(err)
	}
	var errBuf strings.Builder
	deps := mocCleanupDeps{runner: runner}
	if err := runMocsCleanup(cfg, "Music", false, bufio.NewReader(strings.NewReader("y\n")), &errBuf, &errBuf, deps); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(errBuf.String(), "rejected") {
		t.Errorf("expected a rejection report, got:\n%s", errBuf.String())
	}
	got, err := os.ReadFile(filepath.Join(vaultDir, "03-Resources", "MOC Music.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != original {
		t.Errorf("file must be untouched after rejection, got:\n%s", got)
	}
}

// CONTRACT(#115): a proposal identical to the original is reported
// "already well-organized" and nothing is written — no confirm prompt is
// shown, so an empty stdin reader (which would otherwise EOF) proves the
// loop never tried to read one.
func TestMocCleanupIntegrationAlreadyWellOrganized(t *testing.T) {
	vaultDir := newVaultFixture(t)
	original := "---\ntype: moc\n---\n# MOC Music\n\n- [[Jazz]] — https://example.com/jazz\n"
	addNote(t, vaultDir, "03-Resources/MOC Music.md", original, 0)
	runner := newMocCleanupIntegrationRunner(t,
		`{"new_content":"---\ntype: moc\n---\n# MOC Music\n\n- [[Jazz]] — https://example.com/jazz\n","duplicates_flagged":[],"summary":""}`,
		0, "")
	cfg, err := resolveConfig("")
	if err != nil {
		t.Fatal(err)
	}
	var errBuf strings.Builder
	deps := mocCleanupDeps{runner: runner}
	if err := runMocsCleanup(cfg, "Music", false, bufio.NewReader(strings.NewReader("")), &errBuf, &errBuf, deps); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(errBuf.String(), "already well-organized") {
		t.Errorf("stderr = %q", errBuf.String())
	}
	got, err := os.ReadFile(filepath.Join(vaultDir, "03-Resources", "MOC Music.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != original {
		t.Error("an unchanged proposal must never be written")
	}
}

// CONTRACT(#113): --all processes every MOC*.md in one run, through the
// real transport. Both fixtures share the identical literal frontmatter
// block ("---\ntype: moc\n---") and the stub returns one canned
// new_content that is a superset of both originals' URLs (both are
// URL-anchored entries, so retitling/reordering is allowed) — Validate
// passes for BOTH targets against this single response, letting the
// test assert both files were actually applied.
func TestMocCleanupIntegrationAllProcessesMultipleMOCs(t *testing.T) {
	vaultDir := newVaultFixture(t)
	addNote(t, vaultDir, "03-Resources/MOC Alpha.md", "---\ntype: moc\n---\n# MOC Alpha\n\n- [[A]] — https://example.com/a\n", 0)
	addNote(t, vaultDir, "03-Resources/MOC Beta.md", "---\ntype: moc\n---\n# MOC Beta\n\n- [[B]] — https://example.com/b\n", 0)
	runner := newMocCleanupIntegrationRunner(t,
		`{"new_content":"---\ntype: moc\n---\n# MOC Combined\n\n### Group\n- [[A]] — https://example.com/a\n- [[B]] — https://example.com/b\n","duplicates_flagged":[],"summary":"merged view"}`,
		0, "")
	cfg, err := resolveConfig("")
	if err != nil {
		t.Fatal(err)
	}
	var errBuf strings.Builder
	deps := mocCleanupDeps{runner: runner}
	// Two targets, each needs its own "y\n" confirm.
	err = runMocsCleanup(cfg, "", true, bufio.NewReader(strings.NewReader("y\ny\n")), &errBuf, &errBuf, deps)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"MOC Alpha.md", "MOC Beta.md"} {
		got, rerr := os.ReadFile(filepath.Join(vaultDir, "03-Resources", name))
		if rerr != nil {
			t.Fatal(rerr)
		}
		if !strings.Contains(string(got), "### Group") {
			t.Errorf("%s should have been applied via --all, got:\n%s", name, got)
		}
	}
	if !strings.Contains(errBuf.String(), "applied") {
		t.Errorf("expected an applied summary line, got:\n%s", errBuf.String())
	}
}
