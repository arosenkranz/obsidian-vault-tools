// internal/publish/prompt.go
//
// Package publish: LLM->HTML prompt assembly and decode, output
// slugification, and rsync/ssh push/remove/list behind injectable
// interfaces (design spec §Architecture: "internal/publish/ LLM→HTML
// convert; rsync/ssh push/remove/list-remote via subprocess"). The
// THIRD, independent prompt-builder package after internal/triage and
// internal/moc — this one's schema is raw HTML, not JSON, so it is
// deliberately NOT forced through any shared abstraction with those two.
package publish

import "fmt"

// defaultGuidance ports vault.sh publish_doc's default --desc value
// (vault.sh:1128).
const defaultGuidance = "clean, modern design with good typography and readable line lengths"

// promptTemplate ports vault.sh publish_doc's --llm prompt assembly
// verbatim (vault.sh:1137-1144, row #72's prompt CONTENT — only the
// eval-based dispatch is fixed there, not the prompt text itself).
const promptTemplate = "Convert this Obsidian markdown note into a complete, self-contained HTML file.\n" +
	"Design guidance: %s\n" +
	"Rules: single file, inline all CSS and JS, no external dependencies.\n" +
	"Return ONLY the HTML — no markdown, no code fences, no explanation.\n" +
	"\n" +
	"---\n" +
	"%s"

// BuildPrompt assembles the publish --llm prompt for one note. An empty
// guidance falls back to defaultGuidance (row #71's --desc default).
func BuildPrompt(noteContent, guidance string) string {
	if guidance == "" {
		guidance = defaultGuidance
	}
	return fmt.Sprintf(promptTemplate, guidance, noteContent)
}
