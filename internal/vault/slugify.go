package vault

import (
	"regexp"
	"strings"

	"golang.org/x/text/unicode/norm"
)

var (
	headingMarkersRe = regexp.MustCompile(`^\s*#+\s*`)
	// Forbidden filename chars — mined superset used by BOTH old impls,
	// deliberately including @ & # (vault.sh:150, triage_llm.py:191).
	forbiddenFnRe = regexp.MustCompile(`[\\/:*?"<>|@&#]+`)
	wsRunRe       = regexp.MustCompile(`\s+`)
	trailingTokRe = regexp.MustCompile(`\s+\S*$`)
)

// Slugify converts a raw title into a safe filename stem. Case is preserved.
// maxLen is a rune budget; truncation never cuts mid-word. NFC-normalized
// per the v2 filename policy (design spec).
func Slugify(s string, maxLen int) string {
	if s == "" {
		return "Untitled"
	}
	s = norm.NFC.String(s)
	s = headingMarkersRe.ReplaceAllString(s, "")
	s = forbiddenFnRe.ReplaceAllString(s, " ")
	s = wsRunRe.ReplaceAllString(s, " ")
	s = strings.TrimSpace(s)
	if r := []rune(s); len(r) > maxLen {
		s = string(r[:maxLen])
		s = trailingTokRe.ReplaceAllString(s, "")
		s = strings.TrimSpace(s)
	}
	if s == "" {
		return "Untitled"
	}
	return s
}
