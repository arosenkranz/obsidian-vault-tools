// internal/triage/propose_test.go
package triage

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/arosenkranz/obsidian-vault-tools/internal/vault"
)

type fakeRunner struct {
	response  string
	err       error
	gotPrompt string
}

func (f *fakeRunner) Run(ctx context.Context, prompt string) (string, error) {
	f.gotPrompt = prompt
	if f.err != nil {
		return "", f.err
	}
	return f.response, nil
}

func writeTestNote(t *testing.T, dir, name, content string) vault.Note {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return vault.Note{Path: p, Rel: "00-Inbox/" + name, Name: name[:len(name)-3]}
}

// CONTRACT(#89, #92): Propose builds a prompt, calls the runner, and
// decodes the JSON response into a typed Proposal via the phase 0
// llm.ExtractJSON 3-tier fallback.
func TestProposeDecodesResponse(t *testing.T) {
	cfg := testConfig(t)
	note := writeTestNote(t, filepath.Join(cfg.VaultDir, "00-Inbox"), "2026-05-14 0830 thought.md",
		"---\ntype: inbox\ncreated: 2026-05-14\n---\nSome idea.\n")
	runner := &fakeRunner{response: `{"from":"00-Inbox/2026-05-14 0830 thought.md","to":"02-Areas/Idea.md","new_title":"Idea","frontmatter_patch":{"type":"note"},"body_patch":null,"links_to_add":[],"rationale":"fits","confidence":"high"}`}
	p, err := Propose(context.Background(), cfg, note, "AGENTS.md contents", runner)
	if err != nil {
		t.Fatal(err)
	}
	if p.To != "02-Areas/Idea.md" || p.NewTitle != "Idea" || p.Confidence != "high" {
		t.Errorf("Proposal = %+v", p)
	}
	if p.BodyPatch != nil {
		t.Errorf("BodyPatch = %v, want nil", p.BodyPatch)
	}
}

// CONTRACT(#92): a fenced-JSON response still decodes (ExtractJSON tier 2).
func TestProposeDecodesFencedResponse(t *testing.T) {
	cfg := testConfig(t)
	note := writeTestNote(t, filepath.Join(cfg.VaultDir, "00-Inbox"), "x.md", "---\ntype: inbox\n---\nbody\n")
	runner := &fakeRunner{response: "Here you go:\n```json\n{\"to\":\"02-Areas/X.md\",\"new_title\":\"X\",\"confidence\":\"low\",\"rationale\":\"r\"}\n```\n"}
	p, err := Propose(context.Background(), cfg, note, "AGENTS", runner)
	if err != nil {
		t.Fatal(err)
	}
	if p.To != "02-Areas/X.md" {
		t.Errorf("To = %q", p.To)
	}
}

// A runner failure (e.g. llm.ErrAuth) propagates unchanged so the caller
// (cmd/ov, internal/web) can classify it.
func TestProposePropagatesRunnerError(t *testing.T) {
	cfg := testConfig(t)
	note := writeTestNote(t, filepath.Join(cfg.VaultDir, "00-Inbox"), "x.md", "---\ntype: inbox\n---\nbody\n")
	wantErr := errors.New("llm auth expired")
	runner := &fakeRunner{err: wantErr}
	_, err := Propose(context.Background(), cfg, note, "AGENTS", runner)
	if !errors.Is(err, wantErr) {
		t.Errorf("err = %v, want %v", err, wantErr)
	}
}

// A response with no parseable JSON object propagates a decode error, not
// a zero-value Proposal masquerading as success.
func TestProposeDecodeFailure(t *testing.T) {
	cfg := testConfig(t)
	note := writeTestNote(t, filepath.Join(cfg.VaultDir, "00-Inbox"), "x.md", "---\ntype: inbox\n---\nbody\n")
	runner := &fakeRunner{response: "sorry, I can't help with that"}
	_, err := Propose(context.Background(), cfg, note, "AGENTS", runner)
	if err == nil {
		t.Fatal("expected a decode error")
	}
}
