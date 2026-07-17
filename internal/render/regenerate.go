// internal/render/regenerate.go
package render

import (
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/arosenkranz/obsidian-vault-tools/internal/vault"
)

// ErrSourceMissing is returned when a Pair's Markdown source no longer
// exists on disk.
var ErrSourceMissing = errors.New("markdown source not found")

// Regenerate rebuilds p.HTMLPath's spliced body from p.MDPath's current
// content. Mirrors render_html.py's regenerate() (rows #117-120):
// missing source -> ErrSourceMissing; missing RENDER_BODY markers ->
// ErrNoMarkers (caller reports and continues, never fatal to a batch
// run); the write is via vault.WriteNoteAtomic conditional on the hash
// captured when p.HTMLPath was read at the start of this call — fixing
// v1's plain non-atomic html_path.write_text (row #163, same defect
// family as #42/#101/#116/#128/#129/#160).
func Regenerate(p Pair, now time.Time) error {
	if _, err := os.Stat(p.MDPath); err != nil {
		return fmt.Errorf("%s: %w", p.MDRel, ErrSourceMissing)
	}
	mdContent, err := os.ReadFile(p.MDPath)
	if err != nil {
		return fmt.Errorf("%s: %w", p.MDRel, err)
	}
	htmlContent, hash, err := vault.ReadNote(p.HTMLPath)
	if err != nil {
		return fmt.Errorf("%s: %w", p.HTMLRel, err)
	}
	if !renderBodyStartRe.MatchString(htmlContent) || !renderBodyEndRe.MatchString(htmlContent) {
		return fmt.Errorf("%s: %w", p.HTMLRel, ErrNoMarkers)
	}

	body, err := RenderMarkdownBody(string(mdContent))
	if err != nil {
		return fmt.Errorf("%s: %w", p.MDRel, err)
	}

	spliced, err := spliceBody(htmlContent, body)
	if err != nil {
		return fmt.Errorf("%s: %w", p.HTMLRel, err)
	}
	timestamp := fmt.Sprintf("<!-- RENDER_TIMESTAMP: %s -->", now.Format("2006-01-02"))
	spliced = spliceTimestamp(spliced, timestamp)

	return vault.WriteNoteAtomic(p.HTMLPath, []byte(spliced), hash)
}
