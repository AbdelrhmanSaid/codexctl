package store

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"
)

// Rename gives a saved profile a new name. If the profile is the selected
// one, its refreshed credentials are saved first and the selection follows
// the new name. The active auth.json is not touched.
func (s *Store) Rename(oldName, newName string) (string, error) {
	if err := validateName(oldName); err != nil {
		return "", err
	}
	if err := validateName(newName); err != nil {
		return "", err
	}
	if oldName == newName {
		return "", fmt.Errorf("profile %q is already named %q", oldName, newName)
	}
	release, err := s.lock()
	if err != nil {
		return "", err
	}
	defer release()
	if err := s.ensureStateLayout(); err != nil {
		return "", err
	}
	if err := s.recoverPendingActivation(); err != nil {
		return "", err
	}
	if _, err := s.loadProfile(oldName); err != nil {
		return "", err
	}
	exists, err := s.profileExists(newName)
	if err != nil {
		return "", err
	}
	if exists {
		return "", fmt.Errorf("profile %q already exists", newName)
	}
	// Preserve token refreshes before the snapshot is copied so the renamed
	// profile does not lose them.
	warning := ""
	selected := s.selectedName() == oldName
	if selected {
		warning = s.syncCurrentProfile()
	}
	data, err := readFile(s.profilePath(oldName))
	if err != nil {
		return "", err
	}
	// Copy first, then repoint the selection, then delete the old file. If
	// the process dies between those steps the selection always names a
	// profile that exists; at worst the old file lingers as a duplicate.
	if err := writeFile(s.profilePath(newName), data, 0o600); err != nil {
		return "", fmt.Errorf("save profile %q: %w", newName, err)
	}
	if selected {
		if err := writeFile(s.currentPath(), []byte(newName+"\n"), 0o600); err != nil {
			return "", fmt.Errorf("record current profile: %w", err)
		}
	}
	if err := os.Remove(s.profilePath(oldName)); err != nil {
		return "", fmt.Errorf("profile was copied to %q but the old file could not be removed: %w", newName, err)
	}
	return warning, nil
}

// Remove deletes a saved profile. Removing the selected profile clears the
// selection but leaves the active auth.json in place, so Codex stays logged
// in; use `codex logout` to sign out of the account itself.
func (s *Store) Remove(name string) (string, error) {
	if err := validateName(name); err != nil {
		return "", err
	}
	release, err := s.lock()
	if err != nil {
		return "", err
	}
	defer release()
	if err := s.ensureStateLayout(); err != nil {
		return "", err
	}
	if err := s.recoverPendingActivation(); err != nil {
		return "", err
	}
	exists, err := s.profileExists(name)
	if err != nil {
		return "", err
	}
	if !exists {
		return "", fmt.Errorf("profile %q does not exist", name)
	}
	warning := ""
	if s.selectedName() == name {
		if err := removeIfExists(s.currentPath()); err != nil {
			return "", fmt.Errorf("clear current profile: %w", err)
		}
		warning = "the removed profile was selected; auth.json is still active until you run 'codexctl use' or 'codex logout'"
	}
	if err := os.Remove(s.profilePath(name)); err != nil {
		return "", fmt.Errorf("remove profile: %w", err)
	}
	return warning, nil
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

// selectedName returns the profile named by the current marker, or "" when
// there is no valid selection.
func (s *Store) selectedName() string {
	data, err := readFile(s.currentPath())
	if err != nil {
		return ""
	}
	name := strings.TrimSpace(string(data))
	if validateName(name) != nil {
		return ""
	}
	return name
}
