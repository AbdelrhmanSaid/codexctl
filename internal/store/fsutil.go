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

// readFile refuses a symlink at path before reading sensitive state. Keeping
// this check in one helper makes it harder for new call sites to bypass it.
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
	temp, err := os.CreateTemp(filepath.Dir(path), ".codexctl-")
	if err != nil {
		return err
	}
	tempName := temp.Name()
	defer os.Remove(tempName)
	if err := temp.Chmod(mode); err != nil && runtime.GOOS != "windows" {
		temp.Close()
		return err
	}
	if _, err := temp.Write(data); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Sync(); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(tempName, path)
}
