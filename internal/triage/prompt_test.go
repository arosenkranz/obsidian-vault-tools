// internal/triage/prompt_test.go
package triage

import (
	"os"
	"testing"

	"github.com/arosenkranz/obsidian-vault-tools/internal/vault"
)

// Golden file (design spec §Testing strategy tier 2: "both LLM prompt
// assemblies (prompt drift = silent behavior change)"). Update the golden
// file deliberately (never silently) when the prompt text intentionally
// changes: `UPDATE_GOLDEN=1 go test ./internal/triage/... -run TestBuildPromptGolden`.
func TestBuildPromptGolden(t *testing.T) {
	content := "---\ntype: inbox\ncreated: 2026-05-14\nsource: cli\n---\nSome raw capture text.\n"
	fm, body := vault.ParseNote(content)
	folders := []string{"01-Projects", "02-Areas", "02-Areas/Learning", "03-Resources"}
	got := BuildPrompt(folders, "00-Inbox/2026-05-14 0830 thought.md", "=== fake AGENTS.md contents ===\n", fm, body)

	goldenPath := "testdata/prompt_golden.txt"
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.WriteFile(goldenPath, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Errorf("BuildPrompt drifted from golden file.\ngot:\n%s\nwant:\n%s", got, want)
	}
}

// CONTRACT(#91): folders are rendered one per line with a "  - " prefix,
// matching v1's PROMPT_TEMPLATE folder rendering.
func TestBuildPromptFoldersFormatted(t *testing.T) {
	fm, body := vault.ParseNote("no frontmatter here")
	got := BuildPrompt([]string{"01-Projects", "02-Areas"}, "00-Inbox/x.md", "AGENTS", fm, body)
	if !stringsContains(got, "  - 01-Projects\n  - 02-Areas") {
		t.Errorf("prompt did not contain formatted folder list:\n%s", got)
	}
}

// CONTRACT: an empty body renders as the literal placeholder v1 used.
func TestBuildPromptEmptyBodyPlaceholder(t *testing.T) {
	fm, body := vault.ParseNote("---\ntype: inbox\n---\n   \n")
	got := BuildPrompt(nil, "00-Inbox/x.md", "AGENTS", fm, body)
	if !stringsContains(got, "(empty body)") {
		t.Errorf("expected the empty-body placeholder, got:\n%s", got)
	}
}

func stringsContains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return len(needle) == 0
}
