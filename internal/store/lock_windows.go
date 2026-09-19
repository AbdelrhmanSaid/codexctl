package store

import (
	"errors"
	"os"
	"syscall"
)

const errorSharingViolation syscall.Errno = 32

// lockFile opens path with no sharing allowed, so a second open fails for as
// long as this handle exists. The handle is not inheritable.
func lockFile(path string) (func(), error) {
	name, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	handle, err := syscall.CreateFile(name, syscall.GENERIC_READ|syscall.GENERIC_WRITE, 0, nil, syscall.OPEN_ALWAYS, syscall.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		if errors.Is(err, errorSharingViolation) {
			return nil, errLockHeld
		}
		return nil, err
	}
	f := os.NewFile(uintptr(handle), path)
	recordHolder(f)
	return func() { _ = f.Close() }, nil
}
