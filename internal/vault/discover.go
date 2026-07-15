package vault

import (
	"os"
	"path/filepath"
	"sort"
)

// DiscoverFolders mirrors triage_llm.py discover_folders: each existing
// root, its subdirectories, and their subdirectories (depth <= 2 below the
// root), sorted at each level. Unreadable directories are skipped, never
// fatal (walk-resilience rule, design spec §core contracts).
func DiscoverFolders(vaultDir string, roots []string) []string {
	var out []string
	for _, root := range roots {
		rootPath := filepath.Join(vaultDir, root)
		if info, err := os.Stat(rootPath); err != nil || !info.IsDir() {
			continue
		}
		out = append(out, root)
		for _, sub := range sortedSubdirs(rootPath) {
			out = append(out, root+"/"+sub)
			for _, sub2 := range sortedSubdirs(filepath.Join(rootPath, sub)) {
				out = append(out, root+"/"+sub+"/"+sub2)
			}
		}
	}
	return out
}

func sortedSubdirs(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	return names
}
