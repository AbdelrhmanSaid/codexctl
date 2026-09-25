package store

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Profile describes a saved profile without exposing its credentials.
type Profile struct {
	Name     string `json:"name"`
	Selected bool   `json:"selected"`
	Valid    bool   `json:"valid"`
	Identity
}

// Profiles returns every saved profile with its identity, sorted by name.
func (s *Store) Profiles() ([]Profile, error) {
	names, current, err := s.List()
	if err != nil {
		return nil, err
	}
	profiles := make([]Profile, 0, len(names))
	for _, name := range names {
		profile := Profile{Name: name, Selected: name == current}
		if data, err := readFile(s.profilePath(name)); err == nil {
			if info, err := parseAuth(data); err == nil {
				profile.Valid = true
				profile.Identity = info.identity()
			}
		}
		profiles = append(profiles, profile)
	}
	return profiles, nil
}

// Show returns the identity of one saved profile.
func (s *Store) Show(name string) (Profile, error) {
	if err := validateName(name); err != nil {
		return Profile{}, err
	}
	data, err := s.loadProfile(name)
	if err != nil {
		return Profile{}, err
	}
	info, _ := parseAuth(data)
	return Profile{
		Name:     name,
		Selected: s.selectedName() == name,
		Valid:    true,
		Identity: info.identity(),
	}, nil
}

// Import saves the active auth.json as a new profile and selects it. It is
// meant for accounts that were logged in with plain `codex login` before
// codexctl was installed.
func (s *Store) Import(name string) error {
	if err := validateName(name); err != nil {
		return err
	}
	release, err := s.lock()
	if err != nil {
		return err
	}
	defer release()
	if err := s.ensureStateLayout(); err != nil {
		return err
	}
	if err := s.recoverPendingActivation(); err != nil {
		return err
	}
	exists, err := s.profileExists(name)
	if err != nil {
		return err
	}
	if exists {
		return fmt.Errorf("profile %q already exists; remove it first or choose another name", name)
	}
	data, err := readFile(s.authPath())
	if errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("no active auth.json in %s; run 'codexctl login %s' instead", s.CodexHome, name)
	}
	if err != nil {
		return err
	}
	if _, err := parseAuth(data); err != nil {
		return fmt.Errorf("active auth.json is invalid: %w", err)
	}
	if owner, err := s.profileForAccount(data); err != nil {
		return err
	} else if owner != "" {
		return fmt.Errorf("this account is already saved as profile %q; run 'codexctl use %s' or 'codexctl sync' instead", owner, owner)
	}
	if err := s.ensureFileCredentials(); err != nil {
		return err
	}
	if err := writeFile(s.profilePath(name), data, 0o600); err != nil {
		return fmt.Errorf("save profile: %w", err)
	}
	if err := writeFile(s.currentPath(), []byte(name+"\n"), 0o600); err != nil {
		return fmt.Errorf("record current profile: %w", err)
	}
	return nil
}

// profileForAccount returns the name of the saved profile that holds the
// same account as data, or "" if none does.
func (s *Store) profileForAccount(data []byte) (string, error) {
	names, _, err := s.List()
	if err != nil {
		return "", err
	}
	for _, name := range names {
		saved, err := readFile(s.profilePath(name))
		if err != nil {
			continue
		}
		if match, err := compareAccounts(data, saved); err == nil && match == accountSame {
			return name, nil
		}
	}
	return "", nil
}

// Sync saves credential refreshes from the active auth.json into the selected
// profile and returns that profile's name.
func (s *Store) Sync() (string, error) {
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
	name := s.selectedName()
	if name == "" {
		return "", errors.New("no profile has been selected")
	}
	if problem := s.syncCurrentProfile(); problem != "" {
		return "", errors.New(problem)
	}
	return name, nil
}

// Logout runs `codex logout` for a saved profile inside an isolated home, so
// the session is revoked without touching the real Codex home, then deletes
// the profile. If the profile was selected and the active auth.json holds
// the same account, that file is removed as well, since its tokens are no
// longer usable.
func (s *Store) Logout(name string, runLogout func(home string) error) (string, error) {
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
	data, err := s.loadProfile(name)
	if err != nil {
		return "", err
	}
	var warnings []string
	selected := s.selectedName() == name
	if selected {
		// Log out with the freshest tokens Codex has written.
		if problem := s.syncCurrentProfile(); problem != "" {
			warnings = append(warnings, problem)
		} else if data, err = readFile(s.profilePath(name)); err != nil {
			return "", err
		}
	}

	tempHome, err := s.isolatedHome()
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tempHome)
	if err := os.WriteFile(filepath.Join(tempHome, "auth.json"), data, 0o600); err != nil {
		return "", fmt.Errorf("write isolated logout credentials: %w", err)
	}
	if err := runLogout(tempHome); err != nil {
		return "", fmt.Errorf("codex logout failed; profile %q was kept: %w", name, err)
	}

	if err := os.Remove(s.profilePath(name)); err != nil {
		return "", fmt.Errorf("logged out, but the profile could not be removed: %w", err)
	}
	if selected {
		if err := removeIfExists(s.currentPath()); err != nil {
			return "", fmt.Errorf("logged out, but the current profile marker could not be cleared: %w", err)
		}
		active, err := readFile(s.authPath())
		if err == nil {
			if match, err := compareAccounts(active, data); err == nil && match == accountSame {
				if err := removeIfExists(s.authPath()); err != nil {
					return "", fmt.Errorf("logged out, but the active auth.json could not be removed: %w", err)
				}
				warnings = append(warnings, "the active auth.json held the logged-out account and was removed; Codex is now signed out")
			}
		}
	}
	return strings.Join(warnings, "; "), nil
}

func removeIfExists(path string) error {
	if err := refuseSymlink(path); err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}
