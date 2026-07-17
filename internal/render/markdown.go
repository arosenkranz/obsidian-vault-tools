// internal/render/markdown.go
package render

import (
	"bytes"
	"strings"

	"github.com/arosenkranz/obsidian-vault-tools/internal/vault"
	"github.com/yuin/goldmark"
)

var md = goldmark.New()

// RenderMarkdownBody strips the note's YAML frontmatter (via
// vault.ParseNote — reusing the same lossless frontmatter detection the
// rest of the codebase uses, rather than hand-rolling a second "---"
// scanner) and converts the remaining body to an HTML fragment via
// goldmark's default configuration (design spec §Architecture: "ov
// render | port (goldmark)"; §Locked decisions pins goldmark, no
// extensions beyond default). Deliberately NOT a byte-for-byte port of
// render_html.py's hand-rolled md_to_html_body (rows #117-119 cover
// only the marker-splicing MECHANISM as a contract; the generated
// HTML's exact bytes are explicitly out of the compatibility contract —
// render is not in the frozen CLI subset, design spec §Compatibility
// contract).
func RenderMarkdownBody(mdContent string) (string, error) {
	_, body := vault.ParseNote(mdContent)
	var buf bytes.Buffer
	if err := md.Convert([]byte(body), &buf); err != nil {
		return "", err
	}
	return strings.TrimRight(buf.String(), "\n"), nil
}
