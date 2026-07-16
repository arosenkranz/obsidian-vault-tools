package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/arosenkranz/obsidian-vault-tools/internal/config"
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

type fakeMocRunner struct {
	responses []string
	i         int
	gotPrompt []string
}

func (f *fakeMocRunner) Run(ctx context.Context, prompt string) (string, error) {
	f.gotPrompt = append(f.gotPrompt, prompt)
	if f.i >= len(f.responses) {
		return "", errors.New("fakeMocRunner: no more responses")
	}
	r := f.responses[f.i]
	f.i++
	return r, nil
}

// CONTRACT(#114): name XOR --all enforced, else exit 2.
func TestMocsCleanupRequiresNameXorAll(t *testing.T) {
	newVaultFixture(t)
	deps := mocCleanupDeps{runner: &fakeMocRunner{}}
	err := runMocsCleanup(mustResolveConfig(t), "", false, bufio.NewReader(strings.NewReader("")), io.Discard, io.Discard, deps)
	if !errors.Is(err, errExitCode2) {
		t.Fatalf("err = %v, want errExitCode2", err)
	}
}

func TestMocsCleanupRejectsBothNameAndAll(t *testing.T) {
	newVaultFixture(t)
	deps := mocCleanupDeps{runner: &fakeMocRunner{}}
	err := runMocsCleanup(mustResolveConfig(t), "Music", true, bufio.NewReader(strings.NewReader("")), io.Discard, io.Discard, deps)
	if !errors.Is(err, errExitCode2) {
		t.Fatalf("err = %v, want errExitCode2", err)
	}
}

// CONTRACT(#113): an unresolved single name exits 2.
func TestMocsCleanupUnknownNameErrors(t *testing.T) {
	newVaultFixture(t)
	deps := mocCleanupDeps{runner: &fakeMocRunner{}}
	err := runMocsCleanup(mustResolveConfig(t), "Nonexistent", false, bufio.NewReader(strings.NewReader("")), io.Discard, io.Discard, deps)
	if !errors.Is(err, errExitCode2) {
		t.Fatalf("err = %v, want errExitCode2", err)
	}
}

// CONTRACT(#113): --all on a vault with zero MOCs exits 2.
func TestMocsCleanupEmptyVaultAllErrors(t *testing.T) {
	newVaultFixture(t)
	deps := mocCleanupDeps{runner: &fakeMocRunner{}}
	err := runMocsCleanup(mustResolveConfig(t), "", true, bufio.NewReader(strings.NewReader("")), io.Discard, io.Discard, deps)
	if !errors.Is(err, errExitCode2) {
		t.Fatalf("err = %v, want errExitCode2", err)
	}
}

// CONTRACT(#115,#157): an identical proposal is reported "unchanged"
// WITHOUT ever showing a diff or reading from the confirm prompt — the
// unchanged check runs before the y/N prompt, matching v1's ordering
// exactly. Proven here by supplying an empty stdin reader: if the
// implementation incorrectly tried to read a confirm answer, ReadString
// would return io.EOF and (per row #116) be treated as "n"/skip, which
// would make this assertion fail on "unchanged" count — the test would
// only pass by accident unless the ordering is actually right, so the
// count assertion below is the real proof.
func TestMocsCleanupUnchangedSkipsConfirmPrompt(t *testing.T) {
	vaultDir := newVaultFixture(t)
	content := "---\ntype: moc\n---\n# MOC Music\n\n- [[Foo]] — https://example.com/foo\n"
	addNote(t, vaultDir, "03-Resources/MOC Music.md", content, 0)
	runner := &fakeMocRunner{responses: []string{`{"new_content":"---\ntype: moc\n---\n# MOC Music\n\n- [[Foo]] — https://example.com/foo\n","duplicates_flagged":[],"summary":""}`}}
	var errBuf bytes.Buffer
	deps := mocCleanupDeps{runner: runner}
	err := runMocsCleanup(mustResolveConfig(t), "Music", false, bufio.NewReader(strings.NewReader("")), io.Discard, &errBuf, deps)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(errBuf.String(), "already well-organized") {
		t.Errorf("stderr = %q, want the unchanged message", errBuf.String())
	}
	if strings.Contains(errBuf.String(), "Apply this reorganization?") {
		t.Error("an identical proposal must never show the confirm prompt")
	}
}

// BUG(fixed)(#109): a rejected target does not abort the run — --all
// with one rejected and one valid target still processes both, proving
// no partial-apply and no early exit.
func TestMocsCleanupRejectedMovesToNextTarget(t *testing.T) {
	vaultDir := newVaultFixture(t)
	addNote(t, vaultDir, "03-Resources/MOC Alpha.md", "---\ntype: moc\n---\n# MOC Alpha\n\n- [[A]] — https://example.com/a\n", 0)
	addNote(t, vaultDir, "03-Resources/MOC Beta.md", "---\ntype: moc\n---\n# MOC Beta\n\n- [[B]] — https://example.com/b\n", 0)
	runner := &fakeMocRunner{responses: []string{
		// Alpha: frontmatter mutated -> rejected.
		`{"new_content":"---\ntype: moc\nextra: x\n---\n# MOC Alpha\n\n- [[A]] — https://example.com/a\n","duplicates_flagged":[],"summary":""}`,
		// Beta: valid reorganization -> applied (confirmed via stdin "y").
		`{"new_content":"---\ntype: moc\n---\n# MOC Beta\n\n### Group\n- [[B]] — https://example.com/b\n","duplicates_flagged":[],"summary":"grouped"}`,
	}}
	var errBuf bytes.Buffer
	deps := mocCleanupDeps{runner: runner}
	err := runMocsCleanup(mustResolveConfig(t), "", true, bufio.NewReader(strings.NewReader("y\n")), io.Discard, &errBuf, deps)
	if err != nil {
		t.Fatal(err)
	}
	out := errBuf.String()
	if !strings.Contains(out, "rejected") {
		t.Errorf("expected a rejection to be reported:\n%s", out)
	}
	got, rerr := os.ReadFile(filepath.Join(vaultDir, "03-Resources", "MOC Beta.md"))
	if rerr != nil {
		t.Fatal(rerr)
	}
	if !strings.Contains(string(got), "### Group") {
		t.Errorf("Beta should have been applied:\n%s", got)
	}
	betaUnchanged, rerr := os.ReadFile(filepath.Join(vaultDir, "03-Resources", "MOC Alpha.md"))
	if rerr != nil {
		t.Fatal(rerr)
	}
	if strings.Contains(string(betaUnchanged), "extra: x") {
		t.Error("Alpha's rejected proposal must never be written")
	}
}

// DECIDE(#116): EOF on the confirm prompt is "no" for THAT target only
// — the run continues to the next target, unlike triage's abort-on-EOF.
func TestMocsCleanupEOFOnConfirmSkipsOnlyThatTarget(t *testing.T) {
	vaultDir := newVaultFixture(t)
	addNote(t, vaultDir, "03-Resources/MOC Alpha.md", "---\ntype: moc\n---\n# MOC Alpha\n\n- [[A]] — https://example.com/a\n", 0)
	addNote(t, vaultDir, "03-Resources/MOC Beta.md", "---\ntype: moc\n---\n# MOC Beta\n\n- [[B]] — https://example.com/b\n", 0)
	runner := &fakeMocRunner{responses: []string{
		`{"new_content":"---\ntype: moc\n---\n# MOC Alpha\n\n### G\n- [[A]] — https://example.com/a\n","duplicates_flagged":[],"summary":""}`,
		`{"new_content":"---\ntype: moc\n---\n# MOC Beta\n\n### G\n- [[B]] — https://example.com/b\n","duplicates_flagged":[],"summary":""}`,
	}}
	var errBuf bytes.Buffer
	deps := mocCleanupDeps{runner: runner}
	// Empty stdin: BOTH targets hit EOF on their confirm read; both must
	// be skipped, and the run must still complete (not abort after the
	// first EOF) and process Beta too.
	err := runMocsCleanup(mustResolveConfig(t), "", true, bufio.NewReader(strings.NewReader("")), io.Discard, &errBuf, deps)
	if err != nil {
		t.Fatal(err)
	}
	if len(runner.gotPrompt) != 2 {
		t.Fatalf("expected both targets to be proposed (run must not abort after the first EOF), got %d calls", len(runner.gotPrompt))
	}
	for _, name := range []string{"MOC Alpha.md", "MOC Beta.md"} {
		got, rerr := os.ReadFile(filepath.Join(vaultDir, "03-Resources", name))
		if rerr != nil {
			t.Fatal(rerr)
		}
		if strings.Contains(string(got), "### G") {
			t.Errorf("%s should have been skipped (EOF = no), but was applied:\n%s", name, got)
		}
	}
}

func mustResolveConfig(t *testing.T) *config.Config {
	t.Helper()
	cfg, err := resolveConfig("")
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}
