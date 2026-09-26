//go:build unix

package daemon

import (
	"errors"
	"syscall"
)

// processAlive sends signal 0, which checks for the process without
// affecting it. EPERM means it exists but belongs to another user.
func processAlive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}
