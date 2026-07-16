package vault

import (
	"path"
	"regexp"
	"strings"

	"golang.org/x/text/unicode/norm"
)

// wikilinkRe matches one [[...]] wikilink (also the [[...]] inside an ![[...]]
// embed), capturing the inner text non-greedily so adjacent links stay
// separate. Wikilinks do not nest brackets.
var wikilinkRe = regexp.MustCompile(`\[\[([^\[\]]+?)\]\]`)

// ParseWikilinks returns the link TARGET of every [[wikilink]] in text, in
// order of appearance. The target is the portion before any '|' alias and
// before any '#' heading anchor, trimmed; links with an empty target
// (e.g. [[#heading]] or [[]]) are skipped. Backs MOC item counts (behavior
// inventory row #32) and orphan detection (rows #1, #2).
func ParseWikilinks(text string) []string {
	var targets []string
	for _, m := range wikilinkRe.FindAllStringSubmatch(text, -1) {
		t := m[1]
		if i := strings.IndexByte(t, '|'); i >= 0 {
			t = t[:i]
		}
		if i := strings.IndexByte(t, '#'); i >= 0 {
			t = t[:i]
		}
		if t = strings.TrimSpace(t); t != "" {
			targets = append(targets, t)
		}
	}
	return targets
}

// linkKey normalizes a wikilink target OR a note name to a comparison key:
// the final path component (so [[folder/Note]] and "Note" match), NFC-
// normalized and case-folded. Mirrors Obsidian's basename-based,
// case-insensitive link resolution (behavior inventory row #126); NFC matches
// the v2 filename policy so an NFD link resolves to its NFC file.
func linkKey(s string) string {
	s = path.Base(strings.TrimSpace(s)) // slash-based; [[folder/Note]] -> "Note"
	return strings.ToLower(norm.NFC.String(s))
}
