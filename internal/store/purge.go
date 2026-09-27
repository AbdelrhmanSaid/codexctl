package store

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// Purge deletes codexctl's state directory: every saved profile, the
// selection, and the switch records. The Codex home, including the active
// auth.json and config.toml, is left alone, so Codex stays logged in.
//
// before, when not nil, runs while the lock is held and before anything is
// deleted; if it fails nothing is removed.
func (s *Store) Purge(before func() error) error {
	if _, err := os.Lstat(s.StateHome); errors.Is(err, fs.ErrNotExist) {
		if before != nil {
			return before()
		}
		return nil
	}
	release, err := s.lock()
	if err != nil {
		return err
	}
	released := false
	defer func() {
		if !released {
			release()
		}
	}()
	if before != nil {
		if err := before(); err != nil {
			return err
		}
	}
	entries, err := os.ReadDir(s.StateHome)
	if err != nil {
		return err
	}
	lockName := filepath.Base(s.lockPath())
	for _, entry := range entries {
		if entry.Name() == lockName {
			continue
		}
		if err := os.RemoveAll(filepath.Join(s.StateHome, entry.Name())); err != nil {
			return fmt.Errorf("remove codexctl state: %w", err)
		}
	}
	// The lock file goes last, once released: Windows cannot delete a file
	// that is open, and nothing is left for a concurrent process to corrupt.
	release()
	released = true
	if err := os.RemoveAll(s.StateHome); err != nil {
		return fmt.Errorf("remove codexctl state: %w", err)
	}
	return nil
}
