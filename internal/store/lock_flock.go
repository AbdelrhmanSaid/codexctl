//go:build darwin || dragonfly || freebsd || linux || netbsd || openbsd

package store

import (
	"errors"
	"os"
	"syscall"
)

// lockFile takes a non-blocking exclusive flock on path. The descriptor is
// close-on-exec, so the codex child process does not inherit the lock.
func lockFile(path string) (func(), error) {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return nil, err
	}
	for {
		err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if !errors.Is(err, syscall.EINTR) {
			break
		}
	}
	if err != nil {
		f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, errLockHeld
		}
		return nil, err
	}
	recordHolder(f)
	// Closing the descriptor releases the lock.
	return func() { _ = f.Close() }, nil
}
