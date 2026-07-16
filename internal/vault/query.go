package vault

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Note is a scanned markdown note.
type Note struct {
	Path    string    // absolute path
	Rel     string    // vault-relative path, forward slashes
	Name    string    // basename without the .md extension
	ModTime time.Time // filesystem mtime
}

// ProjectEntry is one immediate child of the projects root.
type ProjectEntry struct {
	Name  string // basename (without .md for files)
	IsDir bool
}

// AgeDays returns whole days between mod and now (floored, clamped at 0 for
// future mtimes). Replaces v1 get_file_age, whose find-based primary path
// returned only 0 or 1 so every aged file reported "1" (behavior inventory
// row #18 BUG); the true mtime day count is now always reachable.
func AgeDays(now, mod time.Time) int {
	d := now.Sub(mod)
	if d < 0 {
		return 0
	}
	return int(d / (24 * time.Hour))
}

// ListInbox returns every *.md file directly inside <vaultDir>/<inbox>
// (top-level only, mirroring v1 inbox_list's "$INBOX_DIR"/*.md glob), sorted
// by name. A missing inbox directory surfaces its read error (fs.ErrNotExist)
// for the caller to report; files that vanish mid-listing are skipped (walk
// resilience). Behavior inventory row #56.
func ListInbox(vaultDir, inbox string) ([]Note, error) {
	dir := filepath.Join(vaultDir, inbox)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var notes []Note
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue // vanished between readdir and stat — skip
		}
		notes = append(notes, Note{
			Path:    filepath.Join(dir, e.Name()),
			Rel:     filepath.ToSlash(filepath.Join(inbox, e.Name())),
			Name:    strings.TrimSuffix(e.Name(), ".md"),
			ModTime: info.ModTime(),
		})
	}
	sort.Slice(notes, func(i, j int) bool { return notes[i].Name < notes[j].Name })
	return notes, nil
}

// Stale returns notes anywhere under vaultDir whose AgeDays(now, mtime) is
// strictly greater than days (mirroring find -mtime +N), excluding any note
// with a directory component in excludes, sorted by vault-relative path.
// Behavior inventory rows #62 (CONTRACT) and #61 (BUG: v1 hardcoded the
// exclude paths, ignoring OV_ARCHIVE/OV_META overrides).
func Stale(vaultDir string, days int, now time.Time, excludes []string) ([]Note, error) {
	notes, err := collectNotes(vaultDir, []string{vaultDir}, toSet(excludes))
	if err != nil {
		return nil, err
	}
	var out []Note
	for _, n := range notes {
		if AgeDays(now, n.ModTime) > days {
			out = append(out, n)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Rel < out[j].Rel })
	return out, nil
}

// ModifiedWithin returns notes anywhere under vaultDir whose AgeDays(now,
// mtime) is strictly less than days (mirroring find -mtime -N), excluding any
// note with a directory component in excludes, sorted most-recently-modified
// first then by name. Backs the review "modified this week" section (behavior
// inventory rows #60, #125).
func ModifiedWithin(vaultDir string, days int, now time.Time, excludes []string) ([]Note, error) {
	notes, err := collectNotes(vaultDir, []string{vaultDir}, toSet(excludes))
	if err != nil {
		return nil, err
	}
	var out []Note
	for _, n := range notes {
		if AgeDays(now, n.ModTime) < days {
			out = append(out, n)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].ModTime.Equal(out[j].ModTime) {
			return out[i].ModTime.After(out[j].ModTime)
		}
		return out[i].Name < out[j].Name
	})
	return out, nil
}

// ListProjects returns the immediate children of <vaultDir>/<projects> sorted
// by name: subdirectories (IsDir) and top-level *.md notes. Mirrors v1
// review_vault's "$PROJECTS_DIR"/* listing. A missing projects directory
// returns (nil, nil) (non-fatal, as in v1). Behavior inventory row #60.
func ListProjects(vaultDir, projects string) ([]ProjectEntry, error) {
	entries, err := os.ReadDir(filepath.Join(vaultDir, projects))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var out []ProjectEntry
	for _, e := range entries {
		switch {
		case e.IsDir():
			out = append(out, ProjectEntry{Name: e.Name(), IsDir: true})
		case strings.HasSuffix(e.Name(), ".md"):
			out = append(out, ProjectEntry{Name: strings.TrimSuffix(e.Name(), ".md")})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// collectNotes walks each root (absolute paths), returning every *.md file
// found, with Rel computed relative to vaultDir. Walk-resilient: vanished
// entries and unreadable directories are skipped, never fatal (design spec
// §walk resilience). A non-root directory whose base name is in excludes is
// pruned (mirrors v1's -not -path "*/NAME/*", matching at any depth).
func collectNotes(vaultDir string, roots []string, excludes map[string]bool) ([]Note, error) {
	var notes []Note
	for _, root := range roots {
		err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil // vanished mid-walk or unreadable dir: skip
			}
			if d.IsDir() {
				if p != root && excludes[d.Name()] {
					return fs.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(d.Name(), ".md") {
				return nil
			}
			info, err := d.Info()
			if err != nil {
				return nil // vanished between readdir and stat — skip
			}
			rel, err := filepath.Rel(vaultDir, p)
			if err != nil {
				return nil
			}
			notes = append(notes, Note{
				Path:    p,
				Rel:     filepath.ToSlash(rel),
				Name:    strings.TrimSuffix(d.Name(), ".md"),
				ModTime: info.ModTime(),
			})
			return nil
		})
		if err != nil {
			return notes, err
		}
	}
	return notes, nil
}

func toSet(names []string) map[string]bool {
	if len(names) == 0 {
		return nil
	}
	m := make(map[string]bool, len(names))
	for _, n := range names {
		if n != "" {
			m[n] = true
		}
	}
	return m
}
