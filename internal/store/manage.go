package store

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"
)

// Rename does not touch the active auth.json.
func (s *Store) Rename(oldName, newName string) (Result, error) {
	if err := ValidateName(oldName); err != nil {
		return Result{}, err
	}
	if err := ValidateName(newName); err != nil {
		return Result{}, err
	}
	if oldName == newName {
		return Result{}, fmt.Errorf("profile %q is already named %q", oldName, newName)
	}
	op, err := s.begin()
	if err != nil {
		return Result{}, err
	}
	defer op.release()
	if _, err := s.loadProfile(oldName); err != nil {
		return Result{}, err
	}
	exists, err := s.profileExists(newName)
	if err != nil {
		return Result{}, err
	}
	if exists {
		return Result{}, fmt.Errorf("profile %q already exists", newName)
	}
	// Save token refreshes before the snapshot is copied.
	selected := s.selectedName() == oldName
	if selected {
		op.warn(s.syncCurrentProfile())
	}
	data, err := readFile(s.profilePath(oldName))
	if err != nil {
		return Result{}, err
	}
	// Copy, repoint, then delete: the selection always names a profile that
	// exists.
	if err := writeFile(s.profilePath(newName), data, 0o600); err != nil {
		return Result{}, fmt.Errorf("save profile %q: %w", newName, err)
	}
	if selected {
		if err := s.selectProfile(newName); err != nil {
			return Result{}, err
		}
	}
	if err := os.Remove(s.profilePath(oldName)); err != nil {
		return Result{}, fmt.Errorf("profile was copied to %q but the old file could not be removed: %w", newName, err)
	}
	return op.done(newName)
}

// Remove leaves the active auth.json, so Codex stays logged in.
func (s *Store) Remove(name string) (Result, error) {
	if err := ValidateName(name); err != nil {
		return Result{}, err
	}
	op, err := s.begin()
	if err != nil {
		return Result{}, err
	}
	defer op.release()
	exists, err := s.profileExists(name)
	if err != nil {
		return Result{}, err
	}
	if !exists {
		return Result{}, fmt.Errorf("profile %q does not exist", name)
	}
	if s.selectedName() == name {
		if err := removeIfExists(s.currentPath()); err != nil {
			return Result{}, fmt.Errorf("clear current profile: %w", err)
		}
		op.warn("the removed profile was selected; auth.json is still active until you run 'codexctl use' or 'codex logout'")
	}
	if err := os.Remove(s.profilePath(name)); err != nil {
		return Result{}, fmt.Errorf("remove profile: %w", err)
	}
	return op.done(name)
}

func (s *Store) profileExists(name string) (bool, error) {
	path := s.profilePath(name)
	if err := refuseSymlink(path); err != nil {
		return false, err
	}
	_, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func (s *Store) selectProfile(name string) error {
	if err := writeFile(s.currentPath(), []byte(name+"\n"), 0o600); err != nil {
		return fmt.Errorf("record current profile: %w", err)
	}
	return nil
}

// "" when there is no valid selection.
func (s *Store) selectedName() string {
	data, err := readFile(s.currentPath())
	if err != nil {
		return ""
	}
	name := strings.TrimSpace(string(data))
	if ValidateName(name) != nil {
		return ""
	}
	return name
}
