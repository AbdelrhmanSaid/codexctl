package store

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// newTestStore returns a store whose Codex home and state home are private
// temporary directories.
func newTestStore(t *testing.T) *Store {
	t.Helper()
	root := t.TempDir()
	return &Store{
		CodexHome: filepath.Join(root, "codex"),
		StateHome: filepath.Join(root, "codexctl"),
	}
}

// chatgptAuth builds a ChatGPT-style auth.json for account. refresh stands in
// for the token material Codex rewrites when it refreshes a session, so two
// files for the same account can differ.
func chatgptAuth(t *testing.T, account, refresh string) []byte {
	t.Helper()
	return marshalAuth(t, map[string]any{
		"OPENAI_API_KEY": nil,
		"last_refresh":   "2026-09-01T00:00:00Z",
		"tokens": map[string]any{
			"id_token":      fakeIDToken(t, account+"@example.com", "plus", account),
			"access_token":  "access-" + refresh,
			"refresh_token": refresh,
			"account_id":    account,
		},
	})
}

// apiKeyAuth builds an auth.json that holds only an API key, so it carries no
// account ID.
func apiKeyAuth(t *testing.T, key string) []byte {
	t.Helper()
	return marshalAuth(t, map[string]any{"OPENAI_API_KEY": key})
}

func marshalAuth(t *testing.T, v any) []byte {
	t.Helper()
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// fakeIDToken returns an unsigned JWT carrying the claims codexctl displays.
func fakeIDToken(t *testing.T, email, plan, account string) string {
	t.Helper()
	claims, err := json.Marshal(map[string]any{
		"email": email,
		"https://api.openai.com/auth": map[string]any{
			"chatgpt_account_id": account,
			"chatgpt_plan_type":  plan,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	enc := base64.RawURLEncoding.EncodeToString
	return enc([]byte(`{"alg":"none"}`)) + "." + enc(claims) + ".sig"
}

// fakeLogin stands in for `codex login`: it writes data as the isolated
// home's auth.json.
func fakeLogin(data []byte) func(string) error {
	return func(home string) error {
		return os.WriteFile(filepath.Join(home, "auth.json"), data, 0o600)
	}
}

func mustLogin(t *testing.T, s *Store, name string, data []byte) {
	t.Helper()
	if _, err := s.Login(name, fakeLogin(data)); err != nil {
		t.Fatalf("login %s: %v", name, err)
	}
}

func readBytes(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func writeBytes(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func assertFile(t *testing.T, path string, want []byte) {
	t.Helper()
	if got := readBytes(t, path); !bytes.Equal(got, want) {
		t.Fatalf("%s:\n got: %s\nwant: %s", path, got, want)
	}
}

func assertMissing(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Lstat(path); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("%s should not exist (err = %v)", path, err)
	}
}

func assertCurrent(t *testing.T, s *Store, want string) {
	t.Helper()
	if got := s.selectedName(); got != want {
		t.Fatalf("selected profile = %q, want %q", got, want)
	}
}

func assertWarning(t *testing.T, warning, want string) {
	t.Helper()
	if want == "" && warning != "" {
		t.Fatalf("unexpected warning: %s", warning)
	}
	if !strings.Contains(warning, want) {
		t.Fatalf("warning %q does not mention %q", warning, want)
	}
}

// assertNoIsolatedHomes checks that no temporary login home, which may hold
// credentials, was left in the state directory.
func assertNoIsolatedHomes(t *testing.T, s *Store) {
	t.Helper()
	entries, err := os.ReadDir(s.StateHome)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), loginDirPrefix) {
			t.Fatalf("isolated home %s was left behind", entry.Name())
		}
	}
}
