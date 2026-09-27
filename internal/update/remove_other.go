//go:build !windows

package update

import "os"

// Remove deletes the executable at exe. A running program's file can be
// unlinked on POSIX systems; the process keeps its open image.
func Remove(exe string) error {
	return os.Remove(exe)
}
