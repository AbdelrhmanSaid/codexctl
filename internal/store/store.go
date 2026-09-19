package store

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
)

var profileNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

type Store struct {
	CodexHome string
	StateHome string
}

type Check struct {
	Message string
	Warning bool
}

type authInfo struct {
	Tokens struct {
		AccountID string `json:"account_id"`
	} `json:"tokens"`
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
	if err := s.ensureLayout(); err != nil {
		return "", err
	}
	if err := s.ensureFileCredentials(); err != nil {
		return "", err
	}

	tempHome, err := os.MkdirTemp(s.StateHome, ".login-")
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
	data, err := os.ReadFile(loggedIn)
	if err != nil {
		return "", err
	}
	// Save refreshes for the previously selected profile before replacing a
	// profile with the new login. This ordering also makes re-login safe.
	warning := s.syncCurrentProfile()
	if err := atomicWrite(s.profilePath(name), data, 0o600); err != nil {
		return "", fmt.Errorf("save profile: %w", err)
	}
	if err := s.writeActive(name, data); err != nil {
		return "", err
	}
	return warning, nil
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
	if err := s.ensureLayout(); err != nil {
		return "", err
	}
	if err := s.ensureFileCredentials(); err != nil {
		return "", err
	}
	return s.activateLocked(name)
}

func (s *Store) activateLocked(name string) (string, error) {
	target := s.profilePath(name)
	data, err := os.ReadFile(target)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", fmt.Errorf("profile %q does not exist; run 'codexctl login %s' first", name, name)
		}
		return "", err
	}
	if _, err := parseAuth(data); err != nil {
		return "", fmt.Errorf("profile %q is invalid: %w", name, err)
	}

	warning := s.syncCurrentProfile()
	if err := s.writeActive(name, data); err != nil {
		return "", err
	}
	return warning, nil
}

func (s *Store) writeActive(name string, data []byte) error {
	if err := refuseSymlink(filepath.Join(s.CodexHome, "auth.json")); err != nil {
		return err
	}
	if err := atomicWrite(filepath.Join(s.CodexHome, "auth.json"), data, 0o600); err != nil {
		return fmt.Errorf("activate profile: %w", err)
	}
	if err := atomicWrite(filepath.Join(s.StateHome, "current"), []byte(name+"\n"), 0o600); err != nil {
		return fmt.Errorf("record current profile: %w", err)
	}
	return nil
}

// syncCurrentProfile preserves refresh-token changes written by Codex while a
// profile was active. An account ID mismatch means another tool/login changed
// auth.json, so overwriting the saved profile would be unsafe.
func (s *Store) syncCurrentProfile() string {
	currentBytes, err := os.ReadFile(filepath.Join(s.StateHome, "current"))
	if err != nil {
		return ""
	}
	name := strings.TrimSpace(string(currentBytes))
	if validateName(name) != nil {
		return "the selected-profile marker is invalid; skipped saving active credential changes"
	}
	activeData, err := os.ReadFile(filepath.Join(s.CodexHome, "auth.json"))
	if err != nil {
		return "the active auth.json could not be read; skipped saving credential changes"
	}
	savedData, err := os.ReadFile(s.profilePath(name))
	if err != nil {
		return "the selected profile snapshot is missing; skipped saving credential changes"
	}
	active, errA := parseAuth(activeData)
	saved, errS := parseAuth(savedData)
	if errA != nil || errS != nil {
		return "the active or saved credential file is invalid; skipped saving credential changes"
	}
	if active.Tokens.AccountID != "" && saved.Tokens.AccountID != "" && active.Tokens.AccountID != saved.Tokens.AccountID {
		return "auth.json belongs to a different account than the selected profile; skipped saving credential changes"
	}
	if active.Tokens.AccountID == "" && saved.Tokens.AccountID == "" && hash(activeData) != hash(savedData) {
		return "could not verify the identity of the changed auth.json; skipped saving credential changes"
	}
	if err := atomicWrite(s.profilePath(name), activeData, 0o600); err != nil {
		return "could not preserve refreshed credentials for the previous profile: " + err.Error()
	}
	return ""
}

func (s *Store) List() ([]string, string, error) {
	entries, err := os.ReadDir(filepath.Join(s.StateHome, "profiles"))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, "", err
	}
	profiles := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.Type().IsRegular() && strings.HasSuffix(entry.Name(), ".json") {
			profiles = append(profiles, strings.TrimSuffix(entry.Name(), ".json"))
		}
	}
	current, _, _ := s.Current()
	return profiles, current, nil
}

func (s *Store) Current() (string, bool, error) {
	data, err := os.ReadFile(filepath.Join(s.StateHome, "current"))
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
	active, err := os.ReadFile(filepath.Join(s.CodexHome, "auth.json"))
	if err != nil {
		return name, false, nil
	}
	saved, err := os.ReadFile(s.profilePath(name))
	if err != nil {
		return name, false, nil
	}
	a, errA := parseAuth(active)
	b, errB := parseAuth(saved)
	if errA != nil || errB != nil {
		return name, false, nil
	}
	if a.Tokens.AccountID != "" && b.Tokens.AccountID != "" {
		return name, a.Tokens.AccountID == b.Tokens.AccountID, nil
	}
	return name, hash(active) == hash(saved), nil
}

func (s *Store) Doctor() []Check {
	checks := []Check{}
	config := filepath.Join(s.CodexHome, "config.toml")
	data, err := os.ReadFile(config)
	if err != nil {
		checks = append(checks, Check{"cannot read " + config, true})
	} else if !rootCredentialStoreIsFile(data) {
		checks = append(checks, Check{"config.toml does not set cli_auth_credentials_store = \"file\"", true})
	} else {
		checks = append(checks, Check{"file-backed credential storage is configured", false})
	}
	if err := refuseSymlink(filepath.Join(s.CodexHome, "auth.json")); err != nil {
		checks = append(checks, Check{err.Error(), true})
	} else if _, err := readAuth(filepath.Join(s.CodexHome, "auth.json")); err != nil {
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

func (s *Store) ensureLayout() error {
	for _, dir := range []string{s.CodexHome, s.StateHome, filepath.Join(s.StateHome, "profiles")} {
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

func (s *Store) ensureFileCredentials() error {
	path := filepath.Join(s.CodexHome, "config.toml")
	if err := refuseSymlink(path); err != nil {
		return err
	}
	data, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if rootCredentialStoreIsFile(data) {
		return nil
	}
	updated := setRootCredentialStore(data)
	if err := atomicWrite(path, updated, 0o600); err != nil {
		return fmt.Errorf("configure file-backed Codex credentials: %w", err)
	}
	return nil
}

func rootCredentialStoreIsFile(data []byte) bool {
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") {
			break
		}
		parts := strings.SplitN(trimmed, "=", 2)
		if len(parts) == 2 && strings.TrimSpace(parts[0]) == "cli_auth_credentials_store" {
			return strings.TrimSpace(parts[1]) == `"file"`
		}
	}
	return false
}

func setRootCredentialStore(data []byte) []byte {
	lines := strings.Split(string(data), "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") {
			break
		}
		parts := strings.SplitN(trimmed, "=", 2)
		if len(parts) == 2 && strings.TrimSpace(parts[0]) == "cli_auth_credentials_store" {
			lines[i] = `cli_auth_credentials_store = "file"`
			return []byte(strings.Join(lines, "\n"))
		}
	}
	prefix := "# Managed by codexctl so named auth.json profiles are effective.\ncli_auth_credentials_store = \"file\"\n"
	return append([]byte(prefix), data...)
}

func (s *Store) lock() (func(), error) {
	if err := refuseSymlink(s.StateHome); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(s.StateHome, 0o700); err != nil {
		return nil, err
	}
	lock := filepath.Join(s.StateHome, "lock")
	f, err := os.OpenFile(lock, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		if errors.Is(err, fs.ErrExist) {
			return nil, fmt.Errorf("another codexctl operation is running (remove %s only if it is stale)", lock)
		}
		return nil, err
	}
	fmt.Fprintf(f, "%d\n", os.Getpid())
	f.Close()
	return func() { _ = os.Remove(lock) }, nil
}

func (s *Store) profilePath(name string) string {
	return filepath.Join(s.StateHome, "profiles", name+".json")
}

func validateName(name string) error {
	if !profileNamePattern.MatchString(name) {
		return errors.New("profile names must be 1-64 characters using letters, digits, '.', '_' or '-', and must start with a letter or digit")
	}
	return nil
}

func readAuth(path string) (authInfo, error) {
	if err := refuseSymlink(path); err != nil {
		return authInfo{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return authInfo{}, err
	}
	return parseAuth(data)
}

func parseAuth(data []byte) (authInfo, error) {
	var info authInfo
	if len(bytes.TrimSpace(data)) == 0 || json.Unmarshal(data, &info) != nil {
		return info, errors.New("credential file is not valid JSON")
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(data, &object); err != nil || object == nil {
		return info, errors.New("credential file must be a JSON object")
	}
	return info, nil
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

func hash(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
