//go:build unix

package daemon

import (
	"errors"
	"syscall"
)

// Signal 0 checks without affecting the process; EPERM means it exists.
func processAlive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}
