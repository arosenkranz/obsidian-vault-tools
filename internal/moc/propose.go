// internal/moc/propose.go
package moc

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/arosenkranz/obsidian-vault-tools/internal/llm"
)

// Runner is the subset of *llm.Runner's method set ProposeCleanup needs —
// an interface so tests inject a fake without spawning a real subprocess
// (same injection pattern as triage.Runner; kept as its own package-
// local interface rather than importing triage.Runner, matching this
// codebase's convention of narrow, package-local collaborator
// interfaces with no shared base type).
type Runner interface {
	Run(ctx context.Context, prompt string) (string, error)
}

// ProposeCleanup builds the moc_cleanup.py prompt (row #110) for the MOC
// at mocPath (mocContent is read fresh by the caller — cmd/ov's cleanup
// loop — before calling this, mirroring moc_cleanup.py main()'s own
// per-file read), asks the LLM for a reorganization proposal, and
// decodes the response into a typed Proposal via llm.ExtractJSON's
// 3-tier fallback (row #92) plus the required-field check moc_cleanup.
// py's parse_llm_response performs (row #112): "new_content" must be
// present and a string, else the error carries the raw response.
func ProposeCleanup(ctx context.Context, runner Runner, mocPath, mocContent, mocName string) (Proposal, error) {
	prompt := BuildPrompt(mocContent, mocName)
	raw, err := runner.Run(ctx, prompt)
	if err != nil {
		return Proposal{}, fmt.Errorf("moc cleanup %s: %w", mocPath, err)
	}
	obj, err := llm.ExtractJSON(raw)
	if err != nil {
		return Proposal{}, fmt.Errorf("moc cleanup %s: %w", mocPath, err)
	}
	if _, ok := obj["new_content"].(string); !ok {
		return Proposal{}, fmt.Errorf("moc cleanup %s: LLM response missing required string field 'new_content':\n%s", mocPath, raw)
	}
	buf, err := json.Marshal(obj)
	if err != nil {
		return Proposal{}, fmt.Errorf("moc cleanup %s: re-marshaling decoded JSON: %w", mocPath, err)
	}
	var p Proposal
	if err := json.Unmarshal(buf, &p); err != nil {
		return Proposal{}, fmt.Errorf("moc cleanup %s: proposal did not match the expected schema: %w", mocPath, err)
	}
	return p, nil
}
