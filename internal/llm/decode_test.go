// internal/llm/decode_test.go
package llm

import (
	"strings"
	"testing"
)

// CONTRACT: 3-tier fallback mined from triage_llm.py extract_json (281-304):
// direct parse -> fenced block -> first-{ to last-}.
func TestExtractJSON(t *testing.T) {
	cases := []struct {
		name string
		in   string
		to   string // expected value of key "to"
	}{
		{"direct", `{"to": "03-Resources/Music"}`, "03-Resources/Music"},
		{"direct with whitespace", "\n  {\"to\": \"x\"}\n", "x"},
		{"fenced json", "Here you go:\n```json\n{\"to\": \"x\"}\n```\nDone.", "x"},
		{"fenced no lang", "```\n{\"to\": \"x\"}\n```", "x"},
		{"prose wrapped", `Sure! The answer is {"to": "x"} — hope that helps.`, "x"},
		{"nested braces", `Result: {"to": "x", "meta": {"inner": true}}`, "x"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := ExtractJSON(c.in)
			if err != nil {
				t.Fatal(err)
			}
			if got["to"] != c.to {
				t.Errorf("to = %v, want %q", got["to"], c.to)
			}
		})
	}
}

func TestExtractJSONErrors(t *testing.T) {
	if _, err := ExtractJSON("no json here at all"); err == nil ||
		!strings.Contains(err.Error(), "no JSON object found") {
		t.Errorf("want 'no JSON object found', got %v", err)
	}
	if _, err := ExtractJSON("{broken: json,}"); err == nil {
		t.Error("malformed JSON inside braces must error")
	}
}
