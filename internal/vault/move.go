// internal/vault/move.go
package vault

import (
	"fmt"
	"os"
	"path/filepath"
)

// MoveNote moves srcAbs into destDirAbs, keeping its basename. Refuses
// (never silently overwrites, row #139 fix of v1's plain `mv`) when a file
// with that name already exists at the destination. The rename itself is
// the OS's atomic operation on same-filesystem moves — WriteNoteAtomic is
// for content writes, not renames.
func MoveNote(srcAbs, destDirAbs string) (string, error) {
	dest := filepath.Join(destDirAbs, filepath.Base(srcAbs))
	if _, err := os.Lstat(dest); err == nil {
		return "", fmt.Errorf("%s: %w", dest, ErrExists)
	}
	if err := os.Rename(srcAbs, dest); err != nil {
		return "", err
	}
	return dest, nil
}
