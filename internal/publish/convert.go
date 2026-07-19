// internal/publish/convert.go
package publish

import (
	"context"
	"fmt"

	"github.com/arosenkranz/obsidian-vault-tools/internal/llm"
)

// Runner is the subset of *llm.Runner's method set Convert needs — a
// package-local interface so tests inject a fake without spawning a
// real subprocess (same pattern as internal/moc.Runner/internal/triage's
// own runner seam; deliberately not shared across packages).
type Runner interface {
	Run(ctx context.Context, prompt string) (string, error)
}

// Convert asks the LLM to turn noteContent into a self-contained HTML
// document (BuildPrompt) and decodes the response via
// llm.ExtractHTMLBlock (row #74's contract — the <html>...</html> block
// if present, else the raw response, trimmed). This is publish --llm's
// only consumer of ExtractHTMLBlock, which was built in phase 3 and
// unused in production code until now (design spec §LLM subsystem "Two
// decoders, not one").
func Convert(ctx context.Context, runner Runner, noteContent, guidance string) (string, error) {
	prompt := BuildPrompt(noteContent, guidance)
	raw, err := runner.Run(ctx, prompt)
	if err != nil {
		return "", fmt.Errorf("llm html conversion: %w", err)
	}
	return llm.ExtractHTMLBlock(raw), nil
}
