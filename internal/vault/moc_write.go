// internal/vault/moc_write.go
package vault

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// AppendMOCEntry inserts "- [[title]] — snippet" into content under the
// "## 🔗 Recent Additions" heading, creating that heading (with a leading
// blank line) at EOF if content has no such heading yet. Placement is the v2
// simplification of v1's emoji-heading preference chain (behavior inventory
// rows #8, #41, #42). Pure text transform — the caller re-reads, re-hashes,
// and writes via WriteNoteAtomic (row #129 fix: no more raw >>/mktemp).
func AppendMOCEntry(content, title, snippet string) string {
	const heading = "## 🔗 Recent Additions"
	entry := "- [[" + title + "]] — " + snippet
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		if line == heading {
			out := make([]string, 0, len(lines)+2)
			out = append(out, lines[:i+1]...)
			out = append(out, "", entry)
			out = append(out, lines[i+1:]...)
			return strings.Join(out, "\n")
		}
	}
	trimmed := strings.TrimRight(content, "\n")
	return trimmed + "\n\n" + heading + "\n" + entry + "\n"
}

// FindMOCByName resolves a MOC by name: accepts "Music" or "MOC Music"
// (mirroring v1 find_moc_by_name's "MOC " prefix handling exactly — a
// literal "MOC " prefix is stripped if present, otherwise the name is used
// as-is). Preference: an exact "MOC <bare-name>.md" directly in
// resourcesDir; then the first vault-wide match by sorted path. Behavior
// inventory row #33.
func FindMOCByName(vaultDir, resourcesDir, name string) (*MOC, error) {
	bare := strings.TrimPrefix(name, "MOC ")
	target := "MOC " + bare + ".md"

	resourcesPath := filepath.Join(vaultDir, resourcesDir, target)
	if info, err := os.Stat(resourcesPath); err == nil && !info.IsDir() {
		return mocAt(vaultDir, resourcesPath)
	}
	for _, p := range mocPaths(vaultDir) {
		if filepath.Base(p) == target {
			return mocAt(vaultDir, p)
		}
	}
	return nil, fmt.Errorf("MOC not found: %s", name)
}

func mocAt(vaultDir, p string) (*MOC, error) {
	content, err := os.ReadFile(p)
	if err != nil {
		return nil, err
	}
	rel, err := filepath.Rel(vaultDir, p)
	if err != nil {
		return nil, err
	}
	_, body := ParseNote(string(content))
	return &MOC{
		Path:        p,
		Rel:         filepath.ToSlash(rel),
		Name:        strings.TrimSuffix(filepath.Base(p), ".md"),
		Description: mocDescription(string(content)),
		ItemCount:   len(ParseWikilinks(body)),
	}, nil
}
