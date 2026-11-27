// Package fsutil has the crash-safe file write used for every config file sudoku edits.
package fsutil

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// WriteFileAtomic replaces path with data via a temp file + fsync + rename, so a crash never leaves a
// half-written config. The previous content is kept as path+".sudoku.bak". It edits the real file
// behind a symlink instead of replacing the link with a regular file.
func WriteFileAtomic(path string, data []byte) error {
	return write(path, data, true)
}

// WriteFileAtomicNoBackup is WriteFileAtomic for generated state files nobody edits by hand.
func WriteFileAtomicNoBackup(path string, data []byte) error {
	return write(path, data, false)
}

func write(path string, data []byte, backup bool) error {
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		path = resolved
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", filepath.Dir(path), err)
	}
	mode := fs.FileMode(0o644)
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
		if backup {
			if err := backupFile(path, mode); err != nil {
				return err
			}
		}
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".sudoku-*.tmp")
	if err != nil {
		return fmt.Errorf("temp file: %w", err)
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temp: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("sync temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp: %w", err)
	}
	if err := os.Chmod(tmp.Name(), mode); err != nil {
		return fmt.Errorf("chmod temp: %w", err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("replace %s: %w", path, err)
	}
	return nil
}

func backupFile(path string, mode fs.FileMode) error {
	orig, err := os.ReadFile(path)
	if err != nil {
		return nil //nolint:nilerr // nothing readable to back up
	}
	if err := os.WriteFile(path+".sudoku.bak", orig, mode); err != nil { //nolint:gosec // the operator's own config file
		return fmt.Errorf("backup %s: %w", path, err)
	}
	return nil
}
