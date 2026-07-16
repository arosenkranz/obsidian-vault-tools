// internal/vault/folders.go
package vault

import (
	"os"
	"path/filepath"
	"sort"
)

// ListAllFolders returns every PARA root plus every subdirectory beneath it,
// at any depth, deduplicated, in preorder with siblings sorted at each
// level. Backs the manual triage/capture folder picker (behavior inventory
// row #35: unlike DiscoverFolders' depth<=2 used for the LLM prompt, the
// human picker walks full depth like v1 bash's list_all_para_folders).
// Unreadable/vanished directories are skipped (walk resilience).
func ListAllFolders(vaultDir string, roots []string) []string {
	seen := make(map[string]bool)
	var out []string
	for _, root := range roots {
		rootPath := filepath.Join(vaultDir, root)
		if info, err := os.Stat(rootPath); err != nil || !info.IsDir() {
			continue
		}
		walkFolders(rootPath, root, seen, &out)
	}
	return out
}

func walkFolders(absPath, relPath string, seen map[string]bool, out *[]string) {
	if seen[relPath] {
		return
	}
	seen[relPath] = true
	*out = append(*out, relPath)
	entries, err := os.ReadDir(absPath)
	if err != nil {
		return
	}
	var dirs []string
	for _, e := range entries {
		if e.IsDir() {
			dirs = append(dirs, e.Name())
		}
	}
	sort.Strings(dirs)
	for _, d := range dirs {
		walkFolders(filepath.Join(absPath, d), relPath+"/"+d, seen, out)
	}
}
