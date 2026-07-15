// Package llm: subprocess transport and output decoders for OV_LLM_CMD.
// This file is decoders only — pure functions, no exec.
package llm

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

var jsonFenceRe = regexp.MustCompile("(?s)```(?:json)?\\s*(.*?)```")

// ExtractJSON ports triage_llm.py extract_json — tolerant of LLMs that wrap
// output in prose or markdown fences. One deliberate tightening over the
// python: only a top-level JSON *object* is accepted, so bare/fenced
// non-object JSON (null, arrays, scalars) falls through to tier 3 instead
// of being returned.
func ExtractJSON(text string) (map[string]any, error) {
	text = strings.TrimSpace(text)

	var out map[string]any
	// json.Unmarshal leaves a nil map for JSON null — treat as not-found.
	if err := json.Unmarshal([]byte(text), &out); err == nil && out != nil {
		return out, nil
	}
	if m := jsonFenceRe.FindStringSubmatch(text); m != nil {
		if err := json.Unmarshal([]byte(strings.TrimSpace(m[1])), &out); err == nil && out != nil {
			return out, nil
		}
	}
	start := strings.Index(text, "{")
	end := strings.LastIndex(text, "}")
	if start != -1 && end > start {
		if err := json.Unmarshal([]byte(text[start:end+1]), &out); err != nil {
			return nil, fmt.Errorf("could not parse JSON from LLM response: %w\n--- raw ---\n%s", err, text)
		}
		return out, nil
	}
	return nil, fmt.Errorf("no JSON object found in LLM response:\n--- raw ---\n%s", text)
}
