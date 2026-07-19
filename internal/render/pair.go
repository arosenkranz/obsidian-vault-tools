// internal/render/pair.go
package render

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/arosenkranz/obsidian-vault-tools/internal/vault"
)

// Pair is one HTML<->Markdown render pairing discovered via an HTML
// file's RENDER_SOURCE comment (row #117).
type Pair struct {
	HTMLPath, MDPath string // absolute, symlink-resolved
	HTMLRel, MDRel   string // vault-relative, forward-slash
}

// FindPairedFiles walks vaultDir for every *.html file containing a
// RENDER_SOURCE comment and resolves its paired Markdown source (row
// #117). Walk-resilient (design spec §core contracts): unreadable
// files/directories are skipped, never fatal. A RENDER_SOURCE value
// that resolves outside the vault (traversal-unsafe) is skipped, not
// fatal — the same containment posture mocs new/add established (row
// #153) applied here to a READ path rather than a write. vaultDir is
// EvalSymlinks'd once up front so every returned Rel is computed
// against the SAME resolved root Regenerate/vault.ContainPath use
// internally — avoiding the garbled "../../private/tmp/..."-style path
// class of bug found and fixed during phase 4's post-review manual
// testing (.superpowers/sdd/progress.md, Phase 4 "Post-review manual
// testing"): that mistake is prevented here proactively instead of
// waiting for another smoke test to catch it.
func FindPairedFiles(vaultDir string) ([]Pair, error) {
	vaultReal, err := filepath.EvalSymlinks(vaultDir)
	if err != nil {
		return nil, err
	}

	var pairs []Pair
	walkErr := filepath.WalkDir(vaultReal, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // walk resilience: skip, never fatal
		}
		if d.IsDir() || !strings.EqualFold(filepath.Ext(d.Name()), ".html") {
			return nil
		}
		content, rerr := os.ReadFile(path)
		if rerr != nil {
			return nil
		}
		m := renderSourceRe.FindStringSubmatch(string(content))
		if m == nil {
			return nil
		}
		mdAbs, cerr := vault.ContainPath(vaultReal, m[1])
		if cerr != nil {
			return nil // traversal-unsafe RENDER_SOURCE value: skip this file, keep walking
		}
		htmlRel, rerr := filepath.Rel(vaultReal, path)
		if rerr != nil {
			return nil
		}
		mdRel, rerr := filepath.Rel(vaultReal, mdAbs)
		if rerr != nil {
			return nil
		}
		pairs = append(pairs, Pair{
			HTMLPath: path,
			MDPath:   mdAbs,
			HTMLRel:  filepath.ToSlash(htmlRel),
			MDRel:    filepath.ToSlash(mdRel),
		})
		return nil
	})
	if walkErr != nil {
		return nil, walkErr
	}
	sort.Slice(pairs, func(i, j int) bool { return pairs[i].HTMLRel < pairs[j].HTMLRel })
	return pairs, nil
}
