// cmd/ov/triage_llm_integration_test.go
package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/arosenkranz/obsidian-vault-tools/internal/llm"
	"github.com/arosenkranz/obsidian-vault-tools/internal/llmtest"
)

// Temp-vault integration tests (design spec §Testing strategy tier 3):
// OV_LLM_CMD points at this same test binary, re-executed in stub mode via
// internal/llmtest — proving the REAL internal/llm subprocess transport,
// not a fake.

func newLLMIntegrationRunner(t *testing.T, response string, exitCode int, stderr string) *llm.Runner {
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

// CONTRACT: triage approve → move + MOC rename sync, exercised through the
// REAL subprocess transport end to end.
func TestLLMTriageIntegrationApproveMovesAndSyncsMOC(t *testing.T) {
	vaultDir := newVaultFixture(t)
	if err := os.WriteFile(filepath.Join(vaultDir, "AGENTS.md"), []byte("AGENTS.md contract"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(vaultDir, "03-Resources", "MOC Music.md"), []byte("# MOC Music\n\n- [[First]] — a tune\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	addNote(t, vaultDir, "00-Inbox/First.md", "---\ntype: inbox\nmoc: [[MOC Music]]\ncreated: 2026-05-14\n---\nbody\n", 0)

	runner := newLLMIntegrationRunner(t, `{"to":"02-Areas/Second.md","new_title":"Second","frontmatter_patch":{"type":"note"},"confidence":"high","rationale":"r"}`, 0, "")
	cfg, err := resolveConfig("")
	if err != nil {
		t.Fatal(err)
	}
	var errBuf bytes.Buffer
	deps := llmTriageDeps{runner: runner, agentsMD: "AGENTS.md contract"}
	if err := runLLMTriage(cfg, bufio.NewReader(strings.NewReader("a\n")), os.Stdout, &errBuf, deps, false, 0); err != nil {
		t.Fatal(err)
	}
	if _, statErr := os.Stat(filepath.Join(vaultDir, "02-Areas", "Second.md")); statErr != nil {
		t.Fatal("expected the note to be moved")
	}
	mocContent, err := os.ReadFile(filepath.Join(vaultDir, "03-Resources", "MOC Music.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(mocContent), "[[Second]]") {
		t.Errorf("MOC not synced via the real transport: %s", mocContent)
	}
}

// BUG(fixed)(#5): body_patch rejection through the real transport — the
// note never touches disk with injected content even when the LLM
// actually returns one.
func TestLLMTriageIntegrationRejectsBodyPatchFromRealTransport(t *testing.T) {
	vaultDir := newVaultFixture(t)
	if err := os.WriteFile(filepath.Join(vaultDir, "AGENTS.md"), []byte("contract"), 0o644); err != nil {
		t.Fatal(err)
	}
	addNote(t, vaultDir, "00-Inbox/First.md", "---\ntype: inbox\n---\noriginal\n", 0)
	runner := newLLMIntegrationRunner(t, `{"to":"02-Areas/X.md","new_title":"X","body_patch":"INJECTED VIA REAL LLM","confidence":"high","rationale":"r"}`, 0, "")
	cfg, err := resolveConfig("")
	if err != nil {
		t.Fatal(err)
	}
	var errBuf bytes.Buffer
	deps := llmTriageDeps{runner: runner, agentsMD: "contract"}
	_ = runLLMTriage(cfg, bufio.NewReader(strings.NewReader("")), os.Stdout, &errBuf, deps, false, 0)
	if content, _ := os.ReadFile(filepath.Join(vaultDir, "00-Inbox", "First.md")); strings.Contains(string(content), "INJECTED") {
		t.Fatal("injected content reached disk")
	}
	if _, statErr := os.Stat(filepath.Join(vaultDir, "02-Areas", "X.md")); !os.IsNotExist(statErr) {
		t.Fatal("target should never have been written")
	}
}

// CONTRACT(#97): PARA-root rejection through the real transport.
func TestLLMTriageIntegrationRejectsNonParaRootFromRealTransport(t *testing.T) {
	vaultDir := newVaultFixture(t)
	if err := os.WriteFile(filepath.Join(vaultDir, "AGENTS.md"), []byte("contract"), 0o644); err != nil {
		t.Fatal(err)
	}
	addNote(t, vaultDir, "00-Inbox/First.md", "---\ntype: inbox\n---\nbody\n", 0)
	runner := newLLMIntegrationRunner(t, `{"to":"99-Meta/evil.md","new_title":"evil","confidence":"high","rationale":"r"}`, 0, "")
	cfg, err := resolveConfig("")
	if err != nil {
		t.Fatal(err)
	}
	var errBuf bytes.Buffer
	deps := llmTriageDeps{runner: runner, agentsMD: "contract"}
	_ = runLLMTriage(cfg, bufio.NewReader(strings.NewReader("")), os.Stdout, &errBuf, deps, false, 0)
	if !strings.Contains(errBuf.String(), "PARA") && !strings.Contains(errBuf.String(), "target is not inside") {
		t.Errorf("expected a PARA-root rejection message, got:\n%s", errBuf.String())
	}
	if _, statErr := os.Stat(filepath.Join(vaultDir, "99-Meta", "evil.md")); !os.IsNotExist(statErr) {
		t.Fatal("target should never have been written")
	}
}

// BUG(fixed)(#145): job timeout kills the whole process group — no
// orphaned child process survives (proven at the internal/llm layer by
// TestRunTimeoutKillsPromptly). This test proves the CLI-facing runner,
// constructed exactly as cmd/ov constructs it in production, surfaces
// llm.ErrTimeout through the real subprocess transport.
func TestLLMTriageIntegrationTimeoutClassified(t *testing.T) {
	self, err := llmtest.SelfCmd()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv(llmtest.StubEnv, "1")
	t.Setenv(llmtest.SleepEnv, "3000")
	runner := llm.NewRunner(self, "")
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	_, err = runner.Run(ctx, "prompt")
	if !errors.Is(err, llm.ErrTimeout) {
		t.Errorf("err = %v, want llm.ErrTimeout", err)
	}
}

// DECIDE(new in v2, row #149): the health-check endpoint's underlying
// mechanism (a minimal real invocation) succeeds through the real
// transport when the stub responds normally.
func TestLLMTriageIntegrationHealthCheckSucceeds(t *testing.T) {
	runner := newLLMIntegrationRunner(t, "ok", 0, "")
	if err := runner.HealthCheck(context.Background()); err != nil {
		t.Errorf("HealthCheck() = %v, want nil", err)
	}
}

// DECIDE(new in v2, row #147): the health check surfaces an auth failure
// through the real transport too.
func TestLLMTriageIntegrationHealthCheckAuthFailure(t *testing.T) {
	runner := newLLMIntegrationRunner(t, "", 1, "Error: not logged in")
	err := runner.HealthCheck(context.Background())
	if !errors.Is(err, llm.ErrAuth) {
		t.Errorf("HealthCheck() = %v, want llm.ErrAuth", err)
	}
}
