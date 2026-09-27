package store

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
)

func ensureDirs(dirs ...string) error {
	for _, dir := range dirs {
		if err := refuseSymlink(dir); err != nil {
			return err
		}

		if err := os.MkdirAll(dir, 0o700); err != nil {
			return fmt.Errorf("create %s: %w", dir, err)
		}

		if runtime.GOOS != "windows" {
			_ = os.Chmod(dir, 0o700)
		}
	}

	return nil
}

func (s *Store) ensureStateLayout() error {
	return ensureDirs(s.StateHome, s.profilesDir())
}

func (s *Store) ensureCodexLayout() error {
	return ensureDirs(s.CodexHome)
}

func refuseSymlink(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}

	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("refusing symlinked path %s", path)
	}

	return nil
}

// Every read of sensitive state goes through this symlink check.
func readFile(path string) ([]byte, error) {
	if err := refuseSymlink(path); err != nil {
		return nil, err
	}

	return os.ReadFile(path)
}

func writeFile(path string, data []byte, mode fs.FileMode) error {
	if err := refuseSymlink(path); err != nil {
		return err
	}

	return atomicWrite(path, data, mode)
}

func atomicWrite(path string, data []byte, mode fs.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}

	tempFile, err := os.CreateTemp(filepath.Dir(path), ".codexctl-")
	if err != nil {
		return err
	}

	tempName := tempFile.Name()
	defer os.Remove(tempName)

	if err := tempFile.Chmod(mode); err != nil && runtime.GOOS != "windows" {
		tempFile.Close()
		return err
	}

	if _, err := tempFile.Write(data); err != nil {
		tempFile.Close()
		return err
	}

	if err := tempFile.Sync(); err != nil {
		tempFile.Close()
		return err
	}

	if err := tempFile.Close(); err != nil {
		return err
	}

	return os.Rename(tempName, path)
}
