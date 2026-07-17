// internal/moc/validate.go
package moc

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

var (
	wikilinkRe    = regexp.MustCompile(`\[\[([^\]|]+)`)
	urlRe         = regexp.MustCompile(`https?://\S+`)
	frontmatterRe = regexp.MustCompile(`(?s)^---\n(.*?)\n---\n?`)
)

var (
	// ErrFrontmatterChanged mirrors moc_cleanup.py validate_proposal's
	// frontmatter check (row #108): any change to the block, including
	// adding one where none existed, rejects the whole proposal.
	ErrFrontmatterChanged = errors.New("moc: proposal changes the frontmatter block, which is forbidden")
	// ErrBareWikilinkDropped is row #107's bare-wikilink half: a
	// wikilink with no URL on its line is the only anchor for that
	// entry and must survive verbatim.
	ErrBareWikilinkDropped = errors.New("moc: proposal drops or renames a bare wikilink (no URL to anchor it) present in the original")
	// ErrURLDropped is row #109's URL half — catches dropped entries
	// (anchored or not) the wikilink check alone might miss.
	ErrURLDropped = errors.New("moc: proposal drops a URL present in the original")
)

// frontmatterBlock returns the leading "---\n...\n---\n?" block
// (delimiters included) via the same regex moc_cleanup.py's
// FRONTMATTER_RE uses, or "" if text has none. Deliberately independent
// of vault.ParseNote — this validator's job is byte-for-byte literal
// change detection matching v1's exact algorithm, not vault's lenient
// parsing (which has its own deliberate closing-fence divergence, row
// #84, irrelevant here).
func frontmatterBlock(text string) string {
	return frontmatterRe.FindString(text)
}

// bareWikilinks returns the set of wikilink targets from lines that do
// NOT also contain a URL. A wikilink sharing its line with a URL is
// "anchored" — its display title may be freely corrected (the garbled-
// title-fix feature) because the URL still identifies the entry; a bare
// wikilink has no other anchor, so it must survive verbatim (row #107).
func bareWikilinks(text string) map[string]bool {
	out := make(map[string]bool)
	for _, line := range strings.Split(text, "\n") {
		if urlRe.MatchString(line) {
			continue
		}
		for _, m := range wikilinkRe.FindAllStringSubmatch(line, -1) {
			out[m[1]] = true
		}
	}
	return out
}

func urlSet(text string) map[string]bool {
	out := make(map[string]bool)
	for _, u := range urlRe.FindAllString(text, -1) {
		out[u] = true
	}
	return out
}

func sortedMissing(orig, new map[string]bool) []string {
	var missing []string
	for k := range orig {
		if !new[k] {
			missing = append(missing, k)
		}
	}
	sort.Strings(missing)
	return missing
}

// Validate is the structural safety net, independent of prompt
// compliance (design spec §Architecture, rows #107-109): it does NOT
// guarantee the reorganization is *good* — only that it didn't lose
// frontmatter, URLs, or bare (URL-less) wikilinks present in the
// original. Ports moc_cleanup.py validate_proposal exactly.
func Validate(original, proposed string) error {
	if frontmatterBlock(original) != frontmatterBlock(proposed) {
		return ErrFrontmatterChanged
	}

	dropped := sortedMissing(bareWikilinks(original), bareWikilinks(proposed))
	if len(dropped) > 0 {
		return fmt.Errorf("%w: %s", ErrBareWikilinkDropped, strings.Join(dropped, ", "))
	}

	droppedURLs := sortedMissing(urlSet(original), urlSet(proposed))
	if len(droppedURLs) > 0 {
		return fmt.Errorf("%w: %s", ErrURLDropped, strings.Join(droppedURLs, ", "))
	}

	return nil
}
