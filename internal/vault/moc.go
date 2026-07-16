// internal/vault/moc.go
package vault

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// MOC is a Map-of-Content note.
type MOC struct {
	Path        string
	Rel         string
	Name        string // basename without .md (e.g. "MOC Music")
	Description string // first non-heading, non-blank line among the first 5
	ItemCount   int    // parsed [[wikilinks]] in the body
}

// mocPaths returns the absolute paths of every MOC*.md anywhere under
// vaultDir, sorted. The MOC*.md glob IS the MOC definition (behavior
// inventory row #34). Walk-resilient.
func mocPaths(vaultDir string) []string {
	var paths []string
	filepath.WalkDir(vaultDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if name := d.Name(); strings.HasPrefix(name, "MOC") && strings.HasSuffix(name, ".md") {
			paths = append(paths, p)
		}
		return nil
	})
	sort.Strings(paths)
	return paths
}

// MOCs returns every MOC in the vault sorted by name, each with its
// description (behavior inventory row #63) and wikilink item count (row #32 —
// v2 counts parsed BODY wikilinks, replacing v1's line-based grep -c that also
// counted a frontmatter moc: line). Unreadable MOCs are skipped.
func MOCs(vaultDir string) ([]MOC, error) {
	var mocs []MOC
	for _, p := range mocPaths(vaultDir) {
		content, err := os.ReadFile(p)
		if err != nil {
			continue // skip unreadable, never fatal
		}
		rel, err := filepath.Rel(vaultDir, p)
		if err != nil {
			continue
		}
		_, body := ParseNote(string(content))
		mocs = append(mocs, MOC{
			Path:        p,
			Rel:         filepath.ToSlash(rel),
			Name:        strings.TrimSuffix(filepath.Base(p), ".md"),
			Description: mocDescription(string(content)),
			ItemCount:   len(ParseWikilinks(body)),
		})
	}
	sort.Slice(mocs, func(i, j int) bool { return mocs[i].Name < mocs[j].Name })
	return mocs, nil
}

// mocDescription mirrors v1 moc_list: among the first 5 lines of the raw file,
// the first line that is neither blank nor a heading (starts with #). Operates
// on raw content (frontmatter included), exactly as v1's
// `head -5 | grep -v "^#" | grep -v "^$" | head -1`. Behavior inventory
// row #63.
func mocDescription(content string) string {
	lines := strings.Split(content, "\n")
	if len(lines) > 5 {
		lines = lines[:5]
	}
	for _, line := range lines {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		return line
	}
	return ""
}

// Orphans returns notes under the given roots (relative folder names) that are
// not the target of any [[wikilink]] in any MOC. This REIMPLEMENTS v1
// moc_orphan, which was doubly broken: its found_in_moc flag was set inside a
// pipeline subshell and never propagated, so every note reported orphaned
// (behavior inventory row #1), and it matched by substring grep rather than
// link parsing (row #2). v2 parses wikilink targets from every MOC's full text
// and compares by NFC + case-folded basename (row #126). Notes whose name
// starts with "MOC" are skipped (they are MOCs, not candidates), matching v1's
// scope (row #65: Resources + Areas only). Sorted by vault-relative path.
func Orphans(vaultDir string, roots []string) ([]Note, error) {
	linked := map[string]bool{}
	for _, p := range mocPaths(vaultDir) {
		content, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		for _, t := range ParseWikilinks(string(content)) {
			linked[linkKey(t)] = true
		}
	}

	absRoots := make([]string, 0, len(roots))
	for _, r := range roots {
		absRoots = append(absRoots, filepath.Join(vaultDir, r))
	}
	notes, err := collectNotes(vaultDir, absRoots, nil)
	if err != nil {
		return nil, err
	}

	var orphans []Note
	for _, n := range notes {
		if strings.HasPrefix(n.Name, "MOC") {
			continue
		}
		if linked[linkKey(n.Name)] {
			continue
		}
		orphans = append(orphans, n)
	}
	sort.Slice(orphans, func(i, j int) bool { return orphans[i].Rel < orphans[j].Rel })
	return orphans, nil
}
