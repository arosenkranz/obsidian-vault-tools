// internal/vault/contain.go
package vault

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ErrEscapesVault is returned by ContainPath when target resolves outside root.
var ErrEscapesVault = errors.New("target escapes vault")

// ContainPath resolves target (absolute, or relative to root) against root,
// following symlinks on both sides, and rejects any result that is absolute
// or ".."-prefixed relative to root's real path. target need not exist yet:
// ContainPath walks up to the nearest existing ancestor to resolve symlinks,
// then rejoins the not-yet-created suffix. Behavior inventory row #6 (BUG:
// v1 used a string-prefix check) and row #130 (BUG: v1 manual triage had no
// containment check at all).
func ContainPath(root, target string) (string, error) {
	rootReal, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", fmt.Errorf("vault root: %w", err)
	}
	abs := target
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(root, target)
	}
	abs = filepath.Clean(abs)

	existing := abs
	var suffix []string
	for {
		if _, err := os.Lstat(existing); err == nil {
			break
		}
		parent := filepath.Dir(existing)
		if parent == existing {
			return "", fmt.Errorf("%s: %w", target, ErrEscapesVault)
		}
		suffix = append([]string{filepath.Base(existing)}, suffix...)
		existing = parent
	}
	existingReal, err := filepath.EvalSymlinks(existing)
	if err != nil {
		return "", err
	}
	full := existingReal
	if len(suffix) > 0 {
		full = filepath.Join(append([]string{existingReal}, suffix...)...)
	}
	rel, err := filepath.Rel(rootReal, full)
	if err != nil {
		return "", fmt.Errorf("%s: %w", target, ErrEscapesVault)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", fmt.Errorf("%s: %w", target, ErrEscapesVault)
	}
	return full, nil
}

// EnsureFolder resolves rel against vaultDir via ContainPath (after
// trimming surrounding slashes, mirroring v1's chosen_display trim), then
// creates it — and any missing parents — if it doesn't already exist.
// Returns the resolved absolute path. Behavior inventory row #37.
func EnsureFolder(vaultDir, rel string) (string, error) {
	rel = strings.Trim(strings.TrimSpace(rel), "/")
	abs, err := ContainPath(vaultDir, rel)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(abs, 0o755); err != nil {
		return "", err
	}
	return abs, nil
}
