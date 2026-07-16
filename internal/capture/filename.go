// internal/capture/filename.go
package capture

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/text/unicode/norm"
)

// maxCollisionAttempts bounds the same-minute collision suffix probe
// (behavior inventory row #137: v1's while-loop was unbounded).
const maxCollisionAttempts = 1000

// NextAvailablePath returns an unused path for stem (without extension) in
// dir, probing " (2)", " (3)", ... on a collision (row #50), decided
// case-insensitively against a REAL directory listing rather than a
// filesystem exists() check (row #51 — v2's filename policy is
// case-insensitive and filesystem-independent, unlike v1's `-e` test which
// inherited whatever case sensitivity the host filesystem happened to
// have).
func NextAvailablePath(dir, stem, ext string) (path, name string, err error) {
	existing, statErr := lowerNames(dir)
	if statErr != nil && !os.IsNotExist(statErr) {
		return "", "", statErr
	}
	base := norm.NFC.String(stem)
	name = base + ext
	if !existing[strings.ToLower(name)] {
		return filepath.Join(dir, name), name, nil
	}
	for n := 2; n <= maxCollisionAttempts; n++ {
		name = fmt.Sprintf("%s (%d)%s", base, n, ext)
		if !existing[strings.ToLower(name)] {
			return filepath.Join(dir, name), name, nil
		}
	}
	return "", "", fmt.Errorf("could not find an available name for %q after %d attempts", stem, maxCollisionAttempts)
}

func lowerNames(dir string) (map[string]bool, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return map[string]bool{}, err
	}
	names := make(map[string]bool, len(entries))
	for _, e := range entries {
		names[strings.ToLower(e.Name())] = true
	}
	return names, nil
}
