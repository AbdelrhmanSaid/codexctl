package store

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

type Profile struct {
	Name     string `json:"name"`
	Selected bool   `json:"selected"`
	Valid    bool   `json:"valid"`
	Identity
}

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

func (s *Store) Show(name string) (Profile, error) {
	if err := ValidateName(name); err != nil {
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

// Import is for accounts logged in with plain `codex login`.
func (s *Store) Import(name string) (Result, error) {
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
	if exists {
		return Result{}, fmt.Errorf("profile %q already exists; remove it first or choose another name", name)
	}
	data, err := readFile(s.AuthPath())
	if errors.Is(err, fs.ErrNotExist) {
		return Result{}, fmt.Errorf("no active auth.json in %s; run 'codexctl login %s' instead", s.CodexHome, name)
	}
	if err != nil {
		return Result{}, err
	}
	if _, err := parseAuth(data); err != nil {
		return Result{}, fmt.Errorf("active auth.json is invalid: %w", err)
	}
	if owner, err := s.profileForAccount(data); err != nil {
		return Result{}, err
	} else if owner != "" {
		return Result{}, fmt.Errorf("this account is already saved as profile %q; run 'codexctl use %s' or 'codexctl sync' instead", owner, owner)
	}
	if err := s.ensureFileCredentials(); err != nil {
		return Result{}, err
	}
	if err := writeFile(s.profilePath(name), data, 0o600); err != nil {
		return Result{}, fmt.Errorf("save profile: %w", err)
	}
	if err := s.selectProfile(name); err != nil {
		return Result{}, err
	}
	return op.done(name)
}

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

func (s *Store) Sync() (Result, error) {
	op, err := s.begin()
	if err != nil {
		return Result{}, err
	}
	defer op.release()
	name := s.selectedName()
	if name == "" {
		return Result{}, errors.New("no profile has been selected")
	}
	if problem := s.syncCurrentProfile(); problem != "" {
		return Result{}, errors.New(problem)
	}
	return op.done(name)
}

// Logout revokes the session in an isolated home, then deletes the profile
// and an active auth.json of the same account.
func (s *Store) Logout(name string, runLogout func(home string) error) (Result, error) {
	if err := ValidateName(name); err != nil {
		return Result{}, err
	}
	op, err := s.begin()
	if err != nil {
		return Result{}, err
	}
	defer op.release()
	data, err := s.loadProfile(name)
	if err != nil {
		return Result{}, err
	}
	selected := s.selectedName() == name
	if selected {
		// Log out with the freshest tokens Codex has written.
		if problem := s.syncCurrentProfile(); problem != "" {
			op.warn(problem)
		} else if data, err = readFile(s.profilePath(name)); err != nil {
			return Result{}, err
		}
	}

	tempHome, err := s.isolatedHome()
	if err != nil {
		return Result{}, err
	}
	defer os.RemoveAll(tempHome)
	if err := os.WriteFile(filepath.Join(tempHome, "auth.json"), data, 0o600); err != nil {
		return Result{}, fmt.Errorf("write isolated logout credentials: %w", err)
	}
	if err := runLogout(tempHome); err != nil {
		return Result{}, fmt.Errorf("codex logout failed; profile %q was kept: %w", name, err)
	}

	if err := os.Remove(s.profilePath(name)); err != nil {
		return Result{}, fmt.Errorf("logged out, but the profile could not be removed: %w", err)
	}
	if selected {
		if err := removeIfExists(s.currentPath()); err != nil {
			return Result{}, fmt.Errorf("logged out, but the current profile marker could not be cleared: %w", err)
		}
		active, err := readFile(s.AuthPath())
		if err == nil {
			if match, err := compareAccounts(active, data); err == nil && match == accountSame {
				if err := removeIfExists(s.AuthPath()); err != nil {
					return Result{}, fmt.Errorf("logged out, but the active auth.json could not be removed: %w", err)
				}
				op.recordSwitch("")
				op.warn("the active auth.json held the logged-out account and was removed; Codex is now signed out")
			}
		}
	}
	return op.done(name)
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
