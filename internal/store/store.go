package store

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
)

const loginDirPrefix = ".login-"

var profileNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

type Store struct {
	CodexHome string
	StateHome string
}

type Check struct {
	Message string
	Warning bool
}

func NewFromEnvironment() (*Store, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("resolve home directory: %w", err)
	}
	codexHome := os.Getenv("CODEX_HOME")
	if codexHome == "" {
		codexHome = filepath.Join(home, ".codex")
	}
	stateHome := os.Getenv("CODEXCTL_HOME")
	if stateHome == "" {
		stateHome = filepath.Join(home, ".codexctl")
	}
	return &Store{CodexHome: filepath.Clean(codexHome), StateHome: filepath.Clean(stateHome)}, nil
}

func (s *Store) Login(name string, runLogin func(home string) error) (string, error) {
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

	s.removeAbandonedLogins()
	tempHome, err := os.MkdirTemp(s.StateHome, loginDirPrefix+"*")
	if err != nil {
		return "", fmt.Errorf("create isolated login directory: %w", err)
	}
	defer os.RemoveAll(tempHome)
	if err := os.Chmod(tempHome, 0o700); err != nil && runtime.GOOS != "windows" {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(tempHome, "config.toml"), []byte("cli_auth_credentials_store = \"file\"\n"), 0o600); err != nil {
		return "", fmt.Errorf("write isolated login config: %w", err)
	}
	if err := runLogin(tempHome); err != nil {
		return "", fmt.Errorf("codex login failed: %w", err)
	}
	loggedIn := filepath.Join(tempHome, "auth.json")
	if _, err := readAuth(loggedIn); err != nil {
		return "", fmt.Errorf("Codex did not produce a valid file-backed login: %w", err)
	}
	data, err := readFile(loggedIn)
	if err != nil {
		return "", err
	}
	// Do not touch the real Codex home until the isolated login has succeeded
	// and produced a valid auth.json.
	if err := s.ensureCodexLayout(); err != nil {
		return "", err
	}
	if err := s.recoverPendingActivation(); err != nil {
		return "", err
	}
	if err := s.ensureFileCredentials(); err != nil {
		return "", err
	}
	// Save refreshes for the previously selected profile before replacing a
	// profile with the new login. This ordering also makes re-login safe.
	warning := s.syncCurrentProfile()
	if err := writeFile(s.profilePath(name), data, 0o600); err != nil {
		return "", fmt.Errorf("save profile: %w", err)
	}
	if err := s.writeActive(name, data); err != nil {
		return "", err
	}
	return warning, nil
}

// removeAbandonedLogins deletes isolated login homes left by a login that was
// killed before it could clean up; they may hold credentials. The caller must
// hold the lock, which guarantees no live login still owns one.
func (s *Store) removeAbandonedLogins() {
	entries, _ := os.ReadDir(s.StateHome)
	for _, entry := range entries {
		if entry.IsDir() && strings.HasPrefix(entry.Name(), loginDirPrefix) {
			_ = os.RemoveAll(filepath.Join(s.StateHome, entry.Name()))
		}
	}
}

func (s *Store) Use(name string) (string, error) {
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
	// Load and validate the requested profile before changing config.toml.
	data, err := s.loadProfile(name)
	if err != nil {
		return "", err
	}
	if err := s.ensureCodexLayout(); err != nil {
		return "", err
	}
	if err := s.recoverPendingActivation(); err != nil {
		return "", err
	}
	if err := s.ensureFileCredentials(); err != nil {
		return "", err
	}
	return s.activateLocked(name, data)
}

func (s *Store) loadProfile(name string) ([]byte, error) {
	data, err := readFile(s.profilePath(name))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("profile %q does not exist; run 'codexctl login %s' first", name, name)
		}
		return nil, err
	}
	if _, err := parseAuth(data); err != nil {
		return nil, fmt.Errorf("profile %q is invalid: %w", name, err)
	}
	return data, nil
}

func (s *Store) activateLocked(name string, data []byte) (string, error) {
	warning := s.syncCurrentProfile()
	if err := s.writeActive(name, data); err != nil {
		return "", err
	}
	return warning, nil
}

func (s *Store) writeActive(name string, data []byte) error {
	if err := writeFile(s.pendingPath(), []byte(name+"\n"), 0o600); err != nil {
		return fmt.Errorf("record pending activation: %w", err)
	}
	if err := s.finishActivation(name, data); err != nil {
		return fmt.Errorf("activation is incomplete and will be resumed by the next login or use: %w", err)
	}
	return s.clearPendingActivation()
}

func (s *Store) finishActivation(name string, data []byte) error {
	if err := writeFile(s.authPath(), data, 0o600); err != nil {
		return fmt.Errorf("activate profile: %w", err)
	}
	if err := writeFile(s.currentPath(), []byte(name+"\n"), 0o600); err != nil {
		return fmt.Errorf("record current profile: %w", err)
	}
	return nil
}

func (s *Store) clearPendingActivation() error {
	if err := refuseSymlink(s.pendingPath()); err != nil {
		return err
	}
	if err := os.Remove(s.pendingPath()); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("activation completed but its recovery marker could not be removed: %w", err)
	}
	return nil
}

// recoverPendingActivation completes a switch interrupted after its durable
// marker was written. The caller holds the store lock, so the profile cannot
// be changed concurrently by another codexctl process.
func (s *Store) recoverPendingActivation() error {
	data, err := readFile(s.pendingPath())
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read pending activation: %w", err)
	}
	name := strings.TrimSpace(string(data))
	if err := validateName(name); err != nil {
		return fmt.Errorf("pending activation marker is invalid: %w", err)
	}
	profile, err := s.loadProfile(name)
	if err != nil {
		return fmt.Errorf("recover pending activation: %w", err)
	}
	if err := s.finishActivation(name, profile); err != nil {
		return fmt.Errorf("recover pending activation: %w", err)
	}
	return s.clearPendingActivation()
}

// syncCurrentProfile preserves refresh-token changes written by Codex while a
// profile was active. An account ID mismatch means another tool/login changed
// auth.json, so overwriting the saved profile would be unsafe.
func (s *Store) syncCurrentProfile() string {
	currentBytes, err := readFile(s.currentPath())
	if err != nil {
		return ""
	}
	name := strings.TrimSpace(string(currentBytes))
	if validateName(name) != nil {
		return "the selected-profile marker is invalid; skipped saving active credential changes"
	}
	activeData, err := readFile(s.authPath())
	if err != nil {
		return "the active auth.json could not be read; skipped saving credential changes"
	}
	savedData, err := readFile(s.profilePath(name))
	if err != nil {
		return "the selected profile snapshot is missing; skipped saving credential changes"
	}
	match, err := compareAccounts(activeData, savedData)
	if err != nil {
		return "the active or saved credential file is invalid; skipped saving credential changes"
	}
	switch match {
	case accountDifferent:
		return "auth.json belongs to a different account than the selected profile; skipped saving credential changes"
	case accountUnverified:
		return "could not verify the identity of the changed auth.json; skipped saving credential changes"
	}
	if err := writeFile(s.profilePath(name), activeData, 0o600); err != nil {
		return "could not preserve refreshed credentials for the previous profile: " + err.Error()
	}
	return ""
}

// List returns the saved profile names in sorted order and the selected one.
func (s *Store) List() ([]string, string, error) {
	entries, err := os.ReadDir(s.profilesDir())
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, "", err
	}
	profiles := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.Type().IsRegular() && strings.HasSuffix(entry.Name(), ".json") {
			profiles = append(profiles, strings.TrimSuffix(entry.Name(), ".json"))
		}
	}
	slices.Sort(profiles)
	current, _, _ := s.Current()
	return profiles, current, nil
}

func (s *Store) Current() (string, bool, error) {
	data, err := readFile(s.currentPath())
	if errors.Is(err, fs.ErrNotExist) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	name := strings.TrimSpace(string(data))
	if err := validateName(name); err != nil {
		return "", false, err
	}
	active, err := readFile(s.authPath())
	if errors.Is(err, fs.ErrNotExist) {
		return name, false, nil
	}
	if err != nil {
		return name, false, err
	}
	saved, err := readFile(s.profilePath(name))
	if errors.Is(err, fs.ErrNotExist) {
		return name, false, nil
	}
	if err != nil {
		return name, false, err
	}
	match, err := compareAccounts(active, saved)
	if err != nil {
		return name, false, nil
	}
	return name, match == accountSame, nil
}

func (s *Store) Doctor() []Check {
	checks := []Check{}
	config := s.configPath()
	data, err := readFile(config)
	if err != nil {
		checks = append(checks, Check{"cannot read " + config, true})
	} else if !rootCredentialStoreIsFile(data) {
		checks = append(checks, Check{"config.toml does not set cli_auth_credentials_store = \"file\"", true})
	} else {
		checks = append(checks, Check{"file-backed credential storage is configured", false})
	}
	if err := refuseSymlink(s.authPath()); err != nil {
		checks = append(checks, Check{err.Error(), true})
	} else if _, err := readAuth(s.authPath()); err != nil {
		checks = append(checks, Check{"active auth.json is missing or invalid", true})
	} else {
		checks = append(checks, Check{"active auth.json is valid JSON and is not a symlink", false})
	}
	profiles, _, err := s.List()
	if err != nil || len(profiles) == 0 {
		checks = append(checks, Check{"no saved profiles found", true})
	} else {
		checks = append(checks, Check{fmt.Sprintf("%d saved profile(s)", len(profiles)), false})
	}
	current, matches, _ := s.Current()
	if current == "" {
		checks = append(checks, Check{"no selected profile", true})
	} else if !matches {
		checks = append(checks, Check{"selected profile does not match active auth.json", true})
	} else {
		checks = append(checks, Check{"active auth.json matches profile " + current, false})
	}
	return checks
}

func validateName(name string) error {
	if !profileNamePattern.MatchString(name) {
		return errors.New("profile names must be 1-64 characters using letters, digits, '.', '_' or '-', and must start with a letter or digit")
	}
	return nil
}
