// internal/triage/propose.go
package triage

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/arosenkranz/obsidian-vault-tools/internal/llm"
	"github.com/arosenkranz/obsidian-vault-tools/internal/vault"
)

// Runner is the subset of *llm.Runner's method set Propose needs — an
// interface so tests inject a fake without spawning a real subprocess
// (mirrors capture.TitleFetcher's injection pattern). *llm.Runner
// satisfies this interface as-is.
type Runner interface {
	Run(ctx context.Context, prompt string) (string, error)
}

// Propose builds the AGENTS.md §7 triage prompt for note (reading its
// current content fresh from disk), asks the LLM for a filing proposal,
// and decodes the response into a typed Proposal via
// llm.ExtractJSON's 3-tier fallback (row #92). agentsMD is the vault's
// AGENTS.md content, read once by the caller and reused across notes
// (the caller also owns the exit-2-on-missing-AGENTS.md precondition,
// row #104). folders come from vault.DiscoverFolders (depth<=2, the same
// folder list used for phase-1's LLM-facing discovery, row #87).
func Propose(ctx context.Context, cfg Config, note vault.Note, agentsMD string, runner Runner) (Proposal, error) {
	content, _, err := vault.ReadNote(note.Path)
	if err != nil {
		return Proposal{}, err
	}
	fm, body := vault.ParseNote(content)
	folders := vault.DiscoverFolders(cfg.VaultDir, cfg.ParaRoots())
	prompt := BuildPrompt(folders, note.Rel, agentsMD, fm, body)

	raw, err := runner.Run(ctx, prompt)
	if err != nil {
		return Proposal{}, err
	}
	obj, err := llm.ExtractJSON(raw)
	if err != nil {
		return Proposal{}, err
	}
	buf, err := json.Marshal(obj)
	if err != nil {
		return Proposal{}, fmt.Errorf("triage: re-marshaling decoded JSON: %w", err)
	}
	var p Proposal
	if err := json.Unmarshal(buf, &p); err != nil {
		return Proposal{}, fmt.Errorf("triage: proposal did not match the expected schema: %w", err)
	}
	return p, nil
}
