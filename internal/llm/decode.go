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

// ExtractJSON is a 1:1 port of triage_llm.py extract_json — tolerant of
// LLMs that wrap output in prose or markdown fences.
func ExtractJSON(text string) (map[string]any, error) {
	text = strings.TrimSpace(text)

	var out map[string]any
	if err := json.Unmarshal([]byte(text), &out); err == nil {
		return out, nil
	}
	if m := jsonFenceRe.FindStringSubmatch(text); m != nil {
		if err := json.Unmarshal([]byte(strings.TrimSpace(m[1])), &out); err == nil {
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
