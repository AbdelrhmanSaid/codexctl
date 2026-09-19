//go:build !(darwin || dragonfly || freebsd || linux || netbsd || openbsd || windows)

package store

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
)

// lockFile falls back to exclusive creation where no OS-level file lock is
// available. Unlike the other implementations, a crashed process leaves the
// file behind and it has to be removed by hand.
func lockFile(path string) (func(), error) {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		if errors.Is(err, fs.ErrExist) {
			return nil, fmt.Errorf("another codexctl operation is running (remove %s only if it is stale)", path)
		}
		return nil, err
	}
	recordHolder(f)
	f.Close()
	return func() { _ = os.Remove(path) }, nil
}
