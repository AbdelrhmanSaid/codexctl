package store

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// Purge leaves the Codex home alone. before runs under the lock; if it fails
// nothing is removed.
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

	// The lock file goes last, once released: Windows cannot delete an open
	// file.
	release()
	released = true

	if err := os.RemoveAll(s.StateHome); err != nil {
		return fmt.Errorf("remove codexctl state: %w", err)
	}

	return nil
}
