//go:build !windows

package update

import "os"

// Remove can unlink a running program's file on POSIX.
func Remove(exe string) error {
	return os.Remove(exe)
}
