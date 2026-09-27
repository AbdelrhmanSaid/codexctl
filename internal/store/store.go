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

	"github.com/AbdelrhmanSaid/codexctl/internal/daemon"
)

const loginDirPrefix = ".login-"

var profileNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

type Store struct {
	CodexHome string
	StateHome string
	// DetectDaemon is daemon.Detect when nil.
	DetectDaemon func(codexHome string) daemon.Status
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

func (s *Store) Login(name string, runLogin func(home string) error) (Result, error) {
	if err := ValidateName(name); err != nil {
		return Result{}, err
	}
	op, err := s.open()
	if err != nil {
		return Result{}, err
	}
	defer op.release()

	s.removeAbandonedLogins()
	tempHome, err := s.isolatedHome()
	if err != nil {
		return Result{}, err
	}
	defer os.RemoveAll(tempHome)
	if err := runLogin(tempHome); err != nil {
		return Result{}, fmt.Errorf("codex login failed: %w", err)
	}
	loggedIn := filepath.Join(tempHome, "auth.json")
	if _, err := readAuth(loggedIn); err != nil {
		return Result{}, fmt.Errorf("codex login did not produce a valid file-backed login: %w", err)
	}
	data, err := readFile(loggedIn)
	if err != nil {
		return Result{}, err
	}
	// The real Codex home is untouched until the isolated login has
	// succeeded.
	if err := op.prepareCodexHome(); err != nil {
		return Result{}, err
	}
	// Save the previous profile's refreshes first; this also makes re-login
	// safe.
	op.warn(s.syncCurrentProfile())
	if err := writeFile(s.profilePath(name), data, 0o600); err != nil {
		return Result{}, fmt.Errorf("save profile: %w", err)
	}
	if err := op.writeActive(name, data); err != nil {
		return Result{}, err
	}
	return op.done(name)
}

// The caller must hold the lock and remove the directory.
func (s *Store) isolatedLogin(data []byte) (string, error) {
	home, err := s.isolatedHome()
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(home, "auth.json"), data, 0o600); err != nil {
		os.RemoveAll(home)
		return "", fmt.Errorf("write isolated credentials: %w", err)
	}
	return home, nil
}

// The caller must hold the lock and remove the directory.
func (s *Store) isolatedHome() (string, error) {
	home, err := os.MkdirTemp(s.StateHome, loginDirPrefix+"*")
	if err != nil {
		return "", fmt.Errorf("create isolated Codex home: %w", err)
	}
	if err := os.Chmod(home, 0o700); err != nil && runtime.GOOS != "windows" {
		os.RemoveAll(home)
		return "", err
	}
	if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte("cli_auth_credentials_store = \"file\"\n"), 0o600); err != nil {
		os.RemoveAll(home)
		return "", fmt.Errorf("write isolated Codex config: %w", err)
	}
	return home, nil
}

// Left by a killed login and may hold credentials. The caller must hold the
// lock.
func (s *Store) removeAbandonedLogins() {
	entries, _ := os.ReadDir(s.StateHome)
	for _, entry := range entries {
		if entry.IsDir() && strings.HasPrefix(entry.Name(), loginDirPrefix) {
			_ = os.RemoveAll(filepath.Join(s.StateHome, entry.Name()))
		}
	}
}

func (s *Store) Use(name string) (Result, error) {
	if err := ValidateName(name); err != nil {
		return Result{}, err
	}
	op, err := s.open()
	if err != nil {
		return Result{}, err
	}
	defer op.release()
	// Load and validate the requested profile before changing config.toml.
	data, err := s.loadProfile(name)
	if err != nil {
		return Result{}, err
	}
	if err := op.prepareCodexHome(); err != nil {
		return Result{}, err
	}
	op.warn(s.syncCurrentProfile())
	if err := op.writeActive(name, data); err != nil {
		return Result{}, err
	}
	return op.done(name)
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

// An account mismatch means something else changed auth.json, so the profile
// is not overwritten.
func (s *Store) syncCurrentProfile() string {
	currentBytes, err := readFile(s.currentPath())
	if err != nil {
		return ""
	}
	name := strings.TrimSpace(string(currentBytes))
	if ValidateName(name) != nil {
		return "the selected-profile marker is invalid; skipped saving active credential changes"
	}
	activeData, err := readFile(s.AuthPath())
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
	if err := ValidateName(name); err != nil {
		return "", false, err
	}
	active, err := readFile(s.AuthPath())
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
	if err := refuseSymlink(s.AuthPath()); err != nil {
		checks = append(checks, Check{err.Error(), true})
	} else if _, err := readAuth(s.AuthPath()); err != nil {
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
	checks = append(checks, s.daemonCheck())
	return checks
}

func ValidateName(name string) error {
	if !profileNamePattern.MatchString(name) {
		return errors.New("profile names must be 1-64 characters using letters, digits, '.', '_' or '-', and must start with a letter or digit")
	}
	return nil
}
