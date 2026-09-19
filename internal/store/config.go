package store

import (
	"errors"
	"fmt"
	"io/fs"
	"strings"
)

func (s *Store) ensureFileCredentials() error {
	path := s.configPath()
	data, err := readFile(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if rootCredentialStoreIsFile(data) {
		return nil
	}
	updated := setRootCredentialStore(data)
	if err := writeFile(path, updated, 0o600); err != nil {
		return fmt.Errorf("configure file-backed Codex credentials: %w", err)
	}
	return nil
}

const credentialStoreKey = "cli_auth_credentials_store"

// findRootCredentialStore scans the root table of a config.toml, which ends at
// the first [table] header, for the credential store setting. It returns the
// line index and the raw value, or -1 if the key is not set there.
func findRootCredentialStore(lines []string) (int, string) {
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") {
			break
		}
		key, value, ok := strings.Cut(trimmed, "=")
		if ok && strings.TrimSpace(key) == credentialStoreKey {
			return i, strings.TrimSpace(value)
		}
	}
	return -1, ""
}

func rootCredentialStoreIsFile(data []byte) bool {
	i, value := findRootCredentialStore(strings.Split(string(data), "\n"))
	if i < 0 {
		return false
	}
	// Neither quoted form of "file" can contain '#', so this only strips a
	// trailing comment.
	value, _, _ = strings.Cut(value, "#")
	value = strings.TrimSpace(value)
	return value == `"file"` || value == `'file'`
}

func setRootCredentialStore(data []byte) []byte {
	lines := strings.Split(string(data), "\n")
	if i, _ := findRootCredentialStore(lines); i >= 0 {
		lines[i] = credentialStoreKey + ` = "file"`
		return []byte(strings.Join(lines, "\n"))
	}
	prefix := "# Managed by codexctl so named auth.json profiles are effective.\n" + credentialStoreKey + " = \"file\"\n"
	return append([]byte(prefix), data...)
}
