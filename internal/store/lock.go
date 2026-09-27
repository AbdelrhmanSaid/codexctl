package store

import (
	"errors"
	"fmt"
	"os"
	"strings"
)

var errLockHeld = errors.New("lock is held")

// An OS-level lock on the open file, so it goes away with the process. The
// file is only removed by Purge.
func (s *Store) lock() (func(), error) {
	if err := refuseSymlink(s.StateHome); err != nil {
		return nil, err
	}

	if err := os.MkdirAll(s.StateHome, 0o700); err != nil {
		return nil, err
	}

	path := s.lockPath()
	if err := refuseSymlink(path); err != nil {
		return nil, err
	}

	release, err := lockFile(path)
	if errors.Is(err, errLockHeld) {
		return nil, fmt.Errorf("another codexctl operation is running%s", lockHolder(path))
	}
	if err != nil {
		return nil, fmt.Errorf("lock %s: %w", path, err)
	}

	return release, nil
}

func recordHolder(file *os.File) {
	if file.Truncate(0) == nil {
		_, _ = file.WriteAt(fmt.Appendf(nil, "%d\n", os.Getpid()), 0)
	}
}

// On Windows the holder's exclusive handle prevents reading it.
func lockHolder(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}

	pid := strings.TrimSpace(string(data))
	if pid == "" || strings.Trim(pid, "0123456789") != "" {
		return ""
	}

	return " (pid " + pid + ")"
}
