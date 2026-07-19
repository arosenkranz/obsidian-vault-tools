// internal/newnote/newnote_test.go
package newnote

import (
	"strings"
	"testing"
)

// CONTRACT(#59): {{title}} is substituted into every embedded template.
func TestSubstituteReplacesTitle(t *testing.T) {
	for name, tmpl := range map[string]string{
		"project":  ProjectTemplate,
		"meeting":  MeetingTemplate,
		"learning": LearningTemplate,
	} {
		t.Run(name, func(t *testing.T) {
			got := Substitute(tmpl, "My Title")
			if !strings.Contains(got, "# My Title") {
				t.Errorf("%s template: expected title substituted into the heading, got:\n%s", name, got)
			}
			if strings.Contains(got, "{{title}}") {
				t.Errorf("%s template: {{title}} placeholder survived substitution:\n%s", name, got)
			}
		})
	}
}

// BUG(fixed)(#165): a title containing sed-replacement metacharacters
// (&, a backslash-digit sequence, /) survives a literal, unmodified
// substitution — v1's sed-based substitution would have corrupted or
// broken on at least the "/" case (breaks the s/// delimiter) and the
// "&" case (whole-match backreference in sed's replacement text).
func TestSubstituteHandlesMetacharacterTitles(t *testing.T) {
	dangerous := `Q&A / Notes \1 review`
	got := Substitute("# {{title}}\n", dangerous)
	want := "# " + dangerous + "\n"
	if got != want {
		t.Errorf("Substitute = %q, want %q", got, want)
	}
}

// Substitute replaces every occurrence, not just the first (mirrors
// sed's trailing "g" flag).
func TestSubstituteReplacesEveryOccurrence(t *testing.T) {
	got := Substitute("{{title}} - {{title}}", "X")
	if got != "X - X" {
		t.Errorf("got = %q", got)
	}
}

// CONTRACT(#59): General-type notes with no template get a bare "# Title"
// note.
func TestBare(t *testing.T) {
	got := Bare("Quick Thought")
	if got != "# Quick Thought\n\n" {
		t.Errorf("got = %q", got)
	}
}
