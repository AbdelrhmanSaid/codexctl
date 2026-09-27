package store

import (
	"errors"
	"os"
	"syscall"
)

const errorSharingViolation syscall.Errno = 32

// No sharing is allowed, so a second open fails while this handle exists.
func lockFile(path string) (func(), error) {
	pathPtr, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}

	handle, err := syscall.CreateFile(pathPtr, syscall.GENERIC_READ|syscall.GENERIC_WRITE, 0, nil, syscall.OPEN_ALWAYS, syscall.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		if errors.Is(err, errorSharingViolation) {
			return nil, errLockHeld
		}
		return nil, err
	}

	file := os.NewFile(uintptr(handle), path)
	recordHolder(file)

	return func() { _ = file.Close() }, nil
}
