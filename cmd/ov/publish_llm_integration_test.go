// cmd/ov/publish_llm_integration_test.go
package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// CONTRACT(row #74): publish --llm end to end through the REAL
// subprocess transport (internal/llm.Runner, argv-exec) — the stub
// responds with chatter around an <html> block, and publish.Convert
// must extract exactly that block via llm.ExtractHTMLBlock (first real
// consumer of a decoder built in phase 3 and unused until this phase).
func TestPublishLLMIntegrationExtractsHTMLBlock(t *testing.T) {
	vaultDir := newVaultFixture(t)
	cfg, err := resolveConfig(vaultDir)
	if err != nil {
		t.Fatal(err)
	}
	cfg.DocsHost = "docs.example.com"
	target := filepath.Join(vaultDir, "note.md")
	if err := os.WriteFile(target, []byte("# Note\n\nBody.\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	runner := newLLMIntegrationRunner(t, "Sure!\n<html><body>Converted</body></html>\nEnjoy!", 0, "")
	pusher := &fakePusher{}
	var errBuf bytes.Buffer
	if err := runPublish(cfg, target, true, "", &errBuf, publishDeps{runner: runner, pusher: pusher}); err != nil {
		t.Fatal(err)
	}

	got, err := os.ReadFile(filepath.Join(vaultDir, "Published", "note.html"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(got)) != "<html><body>Converted</body></html>" {
		t.Errorf("published HTML = %q", got)
	}
}

// BUG(fixed)(row #72): a crafted LLM response containing shell
// metacharacters is never interpreted — argv-exec, not `eval`. The stub
// responder's response VALUE contains shell metacharacters ($(...), ;,
// |, &&) to prove the real transport treats the whole thing as inert
// stdout content, never as something executed.
func TestPublishLLMIntegrationNeverEvalsShellMetacharacters(t *testing.T) {
	vaultDir := newVaultFixture(t)
	cfg, err := resolveConfig(vaultDir)
	if err != nil {
		t.Fatal(err)
	}
	cfg.DocsHost = "docs.example.com"
	target := filepath.Join(vaultDir, "note.md")
	if err := os.WriteFile(target, []byte("# Note\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	marker := filepath.Join(t.TempDir(), "pwned")
	dangerous := "<html><body>$(touch " + marker + "); rm -rf /; echo owned && cat /etc/passwd</body></html>"
	runner := newLLMIntegrationRunner(t, dangerous, 0, "")
	pusher := &fakePusher{}
	var errBuf bytes.Buffer
	if err := runPublish(cfg, target, true, "", &errBuf, publishDeps{runner: runner, pusher: pusher}); err != nil {
		t.Fatal(err)
	}

	got, err := os.ReadFile(filepath.Join(vaultDir, "Published", "note.html"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(got)) != strings.TrimSpace(dangerous) {
		t.Errorf("published HTML = %q, want the dangerous text preserved verbatim (never shell-interpreted)", got)
	}
	if _, statErr := os.Stat(marker); statErr == nil {
		t.Fatal("SECURITY: shell metacharacters in the LLM response were executed — eval-class vulnerability reintroduced")
	}
}
