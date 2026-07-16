// internal/capture/note.go
package capture

import (
	"fmt"
	"regexp"
	"strings"
)

// Params holds everything needed to assemble a captured note's content.
type Params struct {
	Title   string
	Body    string
	Tags    []string
	Source  string
	MOCName string
	Created string // YYYY-MM-DD
}

var headingStripRe = regexp.MustCompile(`^\s*#+\s*`)

// frontmatterUnsafeRe matches the first character that would corrupt YAML
// flow syntax or inject an additional frontmatter line if interpolated
// verbatim into a single-line scalar/flow-sequence value (found in the
// phase-2 final whole-branch review: BuildNote previously interpolated
// tags/source unsanitized).
var frontmatterUnsafeRe = regexp.MustCompile(`[\r\n\[\],"]`)

// sanitizeFrontmatterScalar truncates a value at its first newline or YAML
// flow-sequence metacharacter before it's interpolated into a frontmatter
// tags/source line, then trims the result. Truncating (rather than just
// deleting the offending character) matters: a value like "cli\ntype:
// evil" must not collapse into "clitype: evil" — the attacker-controlled
// suffix has to go, not just the newline that separated it.
func sanitizeFrontmatterScalar(s string) string {
	if loc := frontmatterUnsafeRe.FindStringIndex(s); loc != nil {
		s = s[:loc[0]]
	}
	return strings.TrimSpace(s)
}

// BuildNote assembles frontmatter + heading + body exactly as v1
// capture_note (behavior inventory row #52: type/created/modified/source/
// [tags]/[moc]; row #53: "# Title" heading, first body line dropped iff it
// duplicates the title modulo leading #s; row #54: MOC footer when linked;
// row #128: the caller writes this content via vault.WriteNoteAtomic,
// never a raw redirect).
func BuildNote(p Params) string {
	var b strings.Builder
	b.WriteString("---\n")
	b.WriteString("type: inbox\n")
	fmt.Fprintf(&b, "created: %s\n", p.Created)
	fmt.Fprintf(&b, "modified: %s\n", p.Created)
	// BUG(fixed)(#142): tags/source interpolated into YAML frontmatter
	// without sanitization — sanitize before writing (phase-2 final
	// whole-branch review).
	fmt.Fprintf(&b, "source: %s\n", sanitizeFrontmatterScalar(p.Source))
	if len(p.Tags) > 0 {
		sanitized := make([]string, 0, len(p.Tags))
		for _, t := range p.Tags {
			if s := sanitizeFrontmatterScalar(t); s != "" {
				sanitized = append(sanitized, s)
			}
		}
		if len(sanitized) > 0 {
			fmt.Fprintf(&b, "tags: [%s]\n", strings.Join(sanitized, ", "))
		}
	}
	if p.MOCName != "" {
		fmt.Fprintf(&b, "moc: [[%s]]\n", p.MOCName)
	}
	b.WriteString("---\n\n")
	fmt.Fprintf(&b, "# %s\n\n", p.Title)
	b.WriteString(bodyWithoutTitleEcho(p.Body, p.Title))
	b.WriteString("\n")
	if p.MOCName != "" {
		fmt.Fprintf(&b, "\n---\n*Added to [[%s]] on %s*\n", p.MOCName, p.Created)
	}
	return b.String()
}

// bodyWithoutTitleEcho drops leading blank lines and the body's first
// non-blank line iff it equals title after stripping leading markdown
// heading markers (row #53); every other line is kept verbatim.
func bodyWithoutTitleEcho(body, title string) string {
	lines := strings.Split(body, "\n")
	decided := false
	var out []string
	for _, line := range lines {
		if !decided {
			if strings.TrimSpace(line) == "" {
				continue
			}
			if headingStripRe.ReplaceAllString(line, "") == title {
				decided = true
				continue
			}
			decided = true
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}
