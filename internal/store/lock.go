package store

import (
	"errors"
	"fmt"
	"os"
	"strings"
)

// errLockHeld is returned by lockFile when another process holds the lock.
var errLockHeld = errors.New("lock is held")

// lock takes the store-wide lock and returns a function that releases it.
//
// The lock is an OS-level lock on the open lock file rather than the file's
// existence, so it goes away with the process that holds it: a crash, kill or
// Ctrl-C during an interactive `codex login` cannot leave a stale lock behind.
// The file itself is never removed (removing it would let two processes lock
// different files of the same name) and its contents are only informational.
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

// recordHolder notes the current process in a lock file it has just locked.
func recordHolder(f *os.File) {
	if f.Truncate(0) == nil {
		_, _ = f.WriteAt(fmt.Appendf(nil, "%d\n", os.Getpid()), 0)
	}
}

// lockHolder describes the process recorded in a held lock file, if it can be
// read; on Windows the holder's exclusive handle prevents that.
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
