// internal/triage/prompt.go
package triage

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/arosenkranz/obsidian-vault-tools/internal/vault"
)

const promptTemplate = `You are a triage assistant for a PARA-organized Obsidian vault. Your job is to propose where ONE inbox note should be filed.

Read the AGENTS.md contract below, then produce exactly one JSON object matching the schema in §7.

Output rules:
- Output ONLY the JSON object. No prose. No markdown fences. No commentary before or after.
- This is v1 of triage. Set "links_to_add" to []. Do NOT propose wikilinks.
- Set "body_patch" to null. Do NOT rewrite the body.
- "to" must be a path under one of these existing folders. You may include a new filename inside an existing folder, but do NOT invent new folders:
%s
- "from" must equal: %s
- "new_title" must follow the naming conventions in §3.
- "frontmatter_patch" should upgrade the note from inbox: set type, status, tags. Do NOT include created/modified — the script handles those.
- "confidence" must be one of: high, medium, low.
- "rationale" is one or two sentences.

=== AGENTS.md ===
%s

=== NOTE TO TRIAGE ===
path: %s
current_frontmatter: %s
body:
---BEGIN BODY---
%s
---END BODY---

Return the JSON object now.`

// BuildPrompt assembles the triage prompt for one note, mirroring
// triage_llm.py's PROMPT_TEMPLATE (row #91). current_frontmatter is
// rendered from Frontmatter.Pairs() (the lenient raw-value view, row #85)
// — informational text for the LLM to read, never parsed back, so it does
// not need to match Python's json.dumps(dict) typing exactly (a list-typed
// value like `tags: [a, b]` renders as the string "[a, b]" rather than a
// JSON array — acceptable since this field is prompt context, not a
// machine-parsed response).
func BuildPrompt(folders []string, fromPath, agentsMD string, fm *vault.Frontmatter, body string) string {
	folderLines := make([]string, len(folders))
	for i, f := range folders {
		folderLines[i] = "  - " + f
	}
	fmJSON, _ := json.Marshal(fm.Pairs())
	b := strings.TrimSpace(body)
	if b == "" {
		b = "(empty body)"
	}
	return fmt.Sprintf(promptTemplate, strings.Join(folderLines, "\n"), fromPath, agentsMD, fromPath, string(fmJSON), b)
}
