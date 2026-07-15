package vault

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

var (
	ErrChangedOnDisk = errors.New("note changed on disk since it was read — refresh and retry")
	ErrExists        = errors.New("note already exists")
)

func hashBytes(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func ReadNote(path string) (string, string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", "", err
	}
	return string(b), hashBytes(b), nil
}

// WriteNoteAtomic: dot-prefixed temp in the TARGET directory (never
// os.TempDir — rename must be same-filesystem, and Obsidian ignores
// dotfiles so partial writes never sync), fsync, rename, best-effort
// dir fsync. See design spec §core contracts.
func WriteNoteAtomic(path string, content []byte, expectedHash string) error {
	if expectedHash == "" {
		if _, err := os.Lstat(path); err == nil {
			return fmt.Errorf("%s: %w", path, ErrExists)
		}
	} else {
		cur, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("conditional write: %w", err)
		}
		if hashBytes(cur) != expectedHash {
			return fmt.Errorf("%s: %w", path, ErrChangedOnDisk)
		}
	}

	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".ov-tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op after successful rename

	if _, err := tmp.Write(content); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return err
	}
	// Best-effort directory fsync so the rename itself is durable.
	if d, err := os.Open(dir); err == nil {
		_ = d.Sync()
		_ = d.Close()
	}
	return nil
}
