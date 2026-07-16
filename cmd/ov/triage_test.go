// cmd/ov/triage_test.go
package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/arosenkranz/obsidian-vault-tools/internal/llm"
)

func TestTriageSkip(t *testing.T) {
	vaultDir := newVaultFixture(t)
	addNote(t, vaultDir, "00-Inbox/2026-07-15 0800 Foo.md", "# Foo\n", 0)
	cfg, err := resolveConfig("")
	if err != nil {
		t.Fatal(err)
	}
	var errBuf bytes.Buffer
	deps := triageDeps{
		pickFolder: func([]string) (string, error) { t.Fatal("pickFolder should not be called"); return "", nil },
		confirm:    func(string) (bool, error) { t.Fatal("confirm should not be called"); return false, nil },
	}
	if err := runTriage(cfg, bufio.NewReader(strings.NewReader("s\n")), &errBuf, deps); err != nil {
		t.Fatal(err)
	}
	if _, statErr := os.Stat(filepath.Join(vaultDir, "00-Inbox", "2026-07-15 0800 Foo.md")); statErr != nil {
		t.Errorf("skipped note should remain: %v", statErr)
	}
	if !strings.Contains(errBuf.String(), "Skipped") {
		t.Errorf("stderr = %q", errBuf.String())
	}
}

// CONTRACT(#57): Enter (empty choice) triggers the folder picker + move.
func TestTriageMove(t *testing.T) {
	vaultDir := newVaultFixture(t)
	addNote(t, vaultDir, "00-Inbox/2026-07-15 0800 Foo.md", "# Foo\n", 0)
	cfg, _ := resolveConfig("")
	var errBuf bytes.Buffer
	deps := triageDeps{pickFolder: func(folders []string) (string, error) { return "02-Areas", nil }}
	if err := runTriage(cfg, bufio.NewReader(strings.NewReader("\n")), &errBuf, deps); err != nil {
		t.Fatal(err)
	}
	if _, statErr := os.Stat(filepath.Join(vaultDir, "02-Areas", "2026-07-15 0800 Foo.md")); statErr != nil {
		t.Errorf("note should be moved: %v", statErr)
	}
}

// DECIDE(#132): delete requires explicit confirmation.
func TestTriageDeleteConfirmed(t *testing.T) {
	vaultDir := newVaultFixture(t)
	addNote(t, vaultDir, "00-Inbox/2026-07-15 0800 Foo.md", "# Foo\n", 0)
	cfg, _ := resolveConfig("")
	var errBuf bytes.Buffer
	deps := triageDeps{confirm: func(string) (bool, error) { return true, nil }}
	if err := runTriage(cfg, bufio.NewReader(strings.NewReader("d\n")), &errBuf, deps); err != nil {
		t.Fatal(err)
	}
	if _, statErr := os.Stat(filepath.Join(vaultDir, "00-Inbox", "2026-07-15 0800 Foo.md")); !os.IsNotExist(statErr) {
		t.Error("note should be deleted")
	}
}

// DECIDE(#132): declining the confirmation leaves the note untouched.
func TestTriageDeleteDeclined(t *testing.T) {
	vaultDir := newVaultFixture(t)
	addNote(t, vaultDir, "00-Inbox/2026-07-15 0800 Foo.md", "# Foo\n", 0)
	cfg, _ := resolveConfig("")
	var errBuf bytes.Buffer
	deps := triageDeps{confirm: func(string) (bool, error) { return false, nil }}
	if err := runTriage(cfg, bufio.NewReader(strings.NewReader("d\n")), &errBuf, deps); err != nil {
		t.Fatal(err)
	}
	if _, statErr := os.Stat(filepath.Join(vaultDir, "00-Inbox", "2026-07-15 0800 Foo.md")); statErr != nil {
		t.Errorf("declined delete must leave the note: %v", statErr)
	}
}

// CONTRACT(#102): q exits immediately without processing remaining notes.
func TestTriageQuit(t *testing.T) {
	vaultDir := newVaultFixture(t)
	addNote(t, vaultDir, "00-Inbox/2026-07-15 0800 A.md", "# A\n", 0)
	addNote(t, vaultDir, "00-Inbox/2026-07-15 0801 B.md", "# B\n", 0)
	cfg, _ := resolveConfig("")
	var errBuf bytes.Buffer
	deps := triageDeps{pickFolder: func([]string) (string, error) { t.Fatal("should not reach the second note"); return "", nil }}
	if err := runTriage(cfg, bufio.NewReader(strings.NewReader("q\n")), &errBuf, deps); err != nil {
		t.Fatal(err)
	}
	if _, statErr := os.Stat(filepath.Join(vaultDir, "00-Inbox", "2026-07-15 0800 A.md")); statErr != nil {
		t.Errorf("A should remain untouched after quit")
	}
}

// CONTRACT(#102): EOF (Ctrl-D) also quits cleanly — with a real note
// pending, so the read loop genuinely reaches ReadString and hits EOF
// rather than short-circuiting on an empty inbox (found in Task 6 review:
// the original empty-fixture version never exercised the EOF branch).
func TestTriageEOFQuits(t *testing.T) {
	vaultDir := newVaultFixture(t)
	addNote(t, vaultDir, "00-Inbox/2026-07-15 0800 Foo.md", "# Foo\n", 0)
	cfg, _ := resolveConfig("")
	var errBuf bytes.Buffer
	if err := runTriage(cfg, bufio.NewReader(strings.NewReader("")), &errBuf, triageDeps{}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(errBuf.String(), "interrupted") {
		t.Errorf("stderr = %q, want the EOF/interrupted message", errBuf.String())
	}
	if _, statErr := os.Stat(filepath.Join(vaultDir, "00-Inbox", "2026-07-15 0800 Foo.md")); statErr != nil {
		t.Errorf("note must remain untouched after an EOF quit: %v", statErr)
	}
}

// BUG(fixed)(#130): a typed destination cannot escape the vault.
func TestTriageMoveRejectsEscape(t *testing.T) {
	vaultDir := newVaultFixture(t)
	addNote(t, vaultDir, "00-Inbox/2026-07-15 0800 Foo.md", "# Foo\n", 0)
	cfg, _ := resolveConfig("")
	var errBuf bytes.Buffer
	deps := triageDeps{pickFolder: func([]string) (string, error) { return "../../../etc", nil }}
	if err := runTriage(cfg, bufio.NewReader(strings.NewReader("\n")), &errBuf, deps); err != nil {
		t.Fatal(err)
	}
	if _, statErr := os.Stat(filepath.Join(vaultDir, "00-Inbox", "2026-07-15 0800 Foo.md")); statErr != nil {
		t.Errorf("note must remain in inbox after a rejected escape: %v", statErr)
	}
	if !strings.Contains(errBuf.String(), "escapes vault") {
		t.Errorf("stderr = %q, want an escape rejection message", errBuf.String())
	}
}

// BUG(fixed)(#139): a picker result naming an occupied destination refuses
// rather than overwriting.
func TestTriageMoveRefusesExistingDestination(t *testing.T) {
	vaultDir := newVaultFixture(t)
	addNote(t, vaultDir, "00-Inbox/2026-07-15 0800 Foo.md", "new\n", 0)
	addNote(t, vaultDir, "02-Areas/2026-07-15 0800 Foo.md", "old\n", 0)
	cfg, _ := resolveConfig("")
	var errBuf bytes.Buffer
	deps := triageDeps{pickFolder: func([]string) (string, error) { return "02-Areas", nil }}
	if err := runTriage(cfg, bufio.NewReader(strings.NewReader("\n")), &errBuf, deps); err != nil {
		t.Fatal(err)
	}
	got, rerr := os.ReadFile(filepath.Join(vaultDir, "02-Areas", "2026-07-15 0800 Foo.md"))
	if rerr != nil {
		t.Fatal(rerr)
	}
	if !strings.Contains(string(got), "old") {
		t.Error("existing destination note must not be overwritten")
	}
	if _, statErr := os.Stat(filepath.Join(vaultDir, "00-Inbox", "2026-07-15 0800 Foo.md")); statErr != nil {
		t.Error("source note must remain after a refused move")
	}
}

// fakeTriageRunner is a triage.Runner that returns a canned response,
// used by the fast unit tests below (no subprocess). Integration tests in
// triage_llm_integration_test.go use the real stub-subprocess path
// instead.
type fakeTriageRunner struct {
	response string
	err      error
}

func (f *fakeTriageRunner) Run(ctx context.Context, prompt string) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	return f.response, nil
}

func writeLLMTestFixture(t *testing.T, vaultDir, noteName string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(vaultDir, "AGENTS.md"), []byte("AGENTS.md contract text"), 0o644); err != nil {
		t.Fatal(err)
	}
	addNote(t, vaultDir, "00-Inbox/"+noteName, "---\ntype: inbox\ncreated: 2026-05-14\n---\nSome idea.\n", 0)
}

// CONTRACT(#104): --llm returns an error whose exit code is 2 when
// AGENTS.md is missing at the vault root (checked before any note is
// processed).
func TestLLMTriageMissingAgentsMDExits2(t *testing.T) {
	vaultDir := newVaultFixture(t)
	addNote(t, vaultDir, "00-Inbox/x.md", "---\ntype: inbox\n---\nbody\n", 0)
	cfg, err := resolveConfig("")
	if err != nil {
		t.Fatal(err)
	}
	var errBuf bytes.Buffer
	err = runLLMTriage(cfg, bufio.NewReader(strings.NewReader("")), io.Discard, &errBuf, llmTriageDeps{runner: &fakeTriageRunner{}}, false, 0)
	if !errors.Is(err, errExitCode2) {
		t.Errorf("err = %v, want errExitCode2", err)
	}
}

// CONTRACT(#104): the same exit-2 precondition applies to a missing inbox
// directory.
func TestLLMTriageMissingInboxExits2(t *testing.T) {
	vaultDir := t.TempDir()
	clearOVEnv(t)
	if err := os.WriteFile(filepath.Join(vaultDir, "AGENTS.md"), []byte("contract"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(cfgPath, []byte("vault_dir = \""+vaultDir+"\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("OV_CONFIG", cfgPath)
	cfg, err := resolveConfig("")
	if err != nil {
		t.Fatal(err)
	}
	err = runLLMTriage(cfg, bufio.NewReader(strings.NewReader("")), io.Discard, io.Discard, llmTriageDeps{runner: &fakeTriageRunner{}}, false, 0)
	if !errors.Is(err, errExitCode2) {
		t.Errorf("err = %v, want errExitCode2", err)
	}
}

// CONTRACT(#104): --limit N caps the number of notes processed.
func TestLLMTriageLimit(t *testing.T) {
	vaultDir := newVaultFixture(t)
	writeLLMTestFixture(t, vaultDir, "2026-05-14 0800 First.md")
	addNote(t, vaultDir, "00-Inbox/2026-05-14 0801 Second.md", "---\ntype: inbox\n---\nSecond.\n", 0)
	cfg, err := resolveConfig("")
	if err != nil {
		t.Fatal(err)
	}
	runner := &fakeTriageRunner{response: `{"to":"02-Areas/X.md","new_title":"X","confidence":"low","rationale":"r"}`}
	var errBuf bytes.Buffer
	deps := llmTriageDeps{runner: runner, confirm: func(string) (bool, error) { return true, nil }, agentsMD: "AGENTS"}
	if err := runLLMTriage(cfg, bufio.NewReader(strings.NewReader("s\ns\n")), io.Discard, &errBuf, deps, true, 1); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(errBuf.String(), "Second") {
		t.Errorf("expected only the first note processed under --limit 1:\n%s", errBuf.String())
	}
}

// CONTRACT(#104): --dry-run shows every proposal and moves nothing.
func TestLLMTriageDryRunWritesNothing(t *testing.T) {
	vaultDir := newVaultFixture(t)
	writeLLMTestFixture(t, vaultDir, "2026-05-14 0800 First.md")
	cfg, err := resolveConfig("")
	if err != nil {
		t.Fatal(err)
	}
	runner := &fakeTriageRunner{response: `{"to":"02-Areas/X.md","new_title":"X","confidence":"low","rationale":"r"}`}
	var errBuf bytes.Buffer
	deps := llmTriageDeps{runner: runner, agentsMD: "AGENTS"}
	if err := runLLMTriage(cfg, bufio.NewReader(strings.NewReader("")), io.Discard, &errBuf, deps, true, 0); err != nil {
		t.Fatal(err)
	}
	if _, statErr := os.Stat(filepath.Join(vaultDir, "00-Inbox", "2026-05-14 0800 First.md")); statErr != nil {
		t.Error("dry-run must leave the source note in place")
	}
	if _, statErr := os.Stat(filepath.Join(vaultDir, "02-Areas", "X.md")); !os.IsNotExist(statErr) {
		t.Error("dry-run must not write a target note")
	}
	if !strings.Contains(errBuf.String(), "02-Areas/X.md") {
		t.Errorf("dry-run output should show the proposed target:\n%s", errBuf.String())
	}
}

// CONTRACT(#102): 'a' approves and applies the proposal.
func TestLLMTriageApprove(t *testing.T) {
	vaultDir := newVaultFixture(t)
	writeLLMTestFixture(t, vaultDir, "2026-05-14 0800 First.md")
	cfg, err := resolveConfig("")
	if err != nil {
		t.Fatal(err)
	}
	runner := &fakeTriageRunner{response: `{"to":"02-Areas/X.md","new_title":"X","confidence":"high","rationale":"r"}`}
	var errBuf bytes.Buffer
	deps := llmTriageDeps{runner: runner, agentsMD: "AGENTS"}
	if err := runLLMTriage(cfg, bufio.NewReader(strings.NewReader("a\n")), io.Discard, &errBuf, deps, false, 0); err != nil {
		t.Fatal(err)
	}
	if _, statErr := os.Stat(filepath.Join(vaultDir, "02-Areas", "X.md")); statErr != nil {
		t.Error("approved proposal should have been written")
	}
}

// BUG(fixed)(#5): even in the interactive loop, a body_patch-carrying
// proposal is rejected before any write, and the rejection is visible to
// the user (not silently skipped).
func TestLLMTriageRejectsBodyPatchProposal(t *testing.T) {
	vaultDir := newVaultFixture(t)
	writeLLMTestFixture(t, vaultDir, "2026-05-14 0800 First.md")
	cfg, err := resolveConfig("")
	if err != nil {
		t.Fatal(err)
	}
	runner := &fakeTriageRunner{response: `{"to":"02-Areas/X.md","new_title":"X","body_patch":"INJECTED","confidence":"high","rationale":"r"}`}
	var errBuf bytes.Buffer
	deps := llmTriageDeps{runner: runner, agentsMD: "AGENTS"}
	_ = runLLMTriage(cfg, bufio.NewReader(strings.NewReader("")), io.Discard, &errBuf, deps, false, 0)
	if !strings.Contains(errBuf.String(), "body_patch") {
		t.Errorf("rejection reason should be visible in output:\n%s", errBuf.String())
	}
	if _, statErr := os.Stat(filepath.Join(vaultDir, "02-Areas", "X.md")); !os.IsNotExist(statErr) {
		t.Error("a rejected proposal must never be written")
	}
	if content, _ := os.ReadFile(filepath.Join(vaultDir, "00-Inbox", "2026-05-14 0800 First.md")); strings.Contains(string(content), "INJECTED") {
		t.Error("injected content must never reach disk")
	}
}

// CONTRACT(#102): 'q' quits immediately.
func TestLLMTriageQuit(t *testing.T) {
	vaultDir := newVaultFixture(t)
	writeLLMTestFixture(t, vaultDir, "2026-05-14 0800 First.md")
	cfg, err := resolveConfig("")
	if err != nil {
		t.Fatal(err)
	}
	runner := &fakeTriageRunner{response: `{"to":"02-Areas/X.md","new_title":"X","confidence":"high","rationale":"r"}`}
	var errBuf bytes.Buffer
	deps := llmTriageDeps{runner: runner, agentsMD: "AGENTS"}
	if err := runLLMTriage(cfg, bufio.NewReader(strings.NewReader("q\n")), io.Discard, &errBuf, deps, false, 0); err != nil {
		t.Fatal(err)
	}
	if _, statErr := os.Stat(filepath.Join(vaultDir, "00-Inbox", "2026-05-14 0800 First.md")); statErr != nil {
		t.Error("quitting must leave the pending note untouched")
	}
}

// DECIDE(new in v2, row #147): an auth-classified runner error surfaces a
// specific, actionable message, not a generic failure string.
func TestLLMTriageAuthFailureMessage(t *testing.T) {
	vaultDir := newVaultFixture(t)
	writeLLMTestFixture(t, vaultDir, "2026-05-14 0800 First.md")
	cfg, err := resolveConfig("")
	if err != nil {
		t.Fatal(err)
	}
	runner := &fakeTriageRunner{err: fmt.Errorf("%w: not logged in", llm.ErrAuth)}
	var errBuf bytes.Buffer
	deps := llmTriageDeps{runner: runner, agentsMD: "AGENTS"}
	_ = runLLMTriage(cfg, bufio.NewReader(strings.NewReader("q\n")), io.Discard, &errBuf, deps, false, 0)
	if !strings.Contains(errBuf.String(), "auth expired") {
		t.Errorf("expected an auth-specific message, got:\n%s", errBuf.String())
	}
}
