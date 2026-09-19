package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	root := t.TempDir()
	return &Store{CodexHome: filepath.Join(root, ".codex"), StateHome: filepath.Join(root, ".codexctl")}
}

func auth(account, token string) []byte {
	return []byte(`{"tokens":{"account_id":"` + account + `","refresh_token":"` + token + `"}}`)
}

func TestLoginIsIsolatedAndActivates(t *testing.T) {
	s := newTestStore(t)
	warning, err := s.Login("work", func(home string) error {
		if home == s.CodexHome {
			t.Fatal("login was not isolated")
		}
		return os.WriteFile(filepath.Join(home, "auth.json"), auth("account-work", "secret"), 0o600)
	})
	if err != nil || warning != "" {
		t.Fatalf("Login() = warning %q, error %v", warning, err)
	}
	active, _ := os.ReadFile(filepath.Join(s.CodexHome, "auth.json"))
	if !strings.Contains(string(active), "account-work") {
		t.Fatal("new profile was not activated")
	}
	config, _ := os.ReadFile(filepath.Join(s.CodexHome, "config.toml"))
	if !strings.Contains(string(config), `cli_auth_credentials_store = "file"`) {
		t.Fatal("file credential store was not configured")
	}
}

func TestUsePreservesRefreshedCredentials(t *testing.T) {
	s := newTestStore(t)
	for name, account := range map[string]string{"personal": "a", "work": "b"} {
		_, err := s.Login(name, func(home string) error {
			return os.WriteFile(filepath.Join(home, "auth.json"), auth(account, "old"), 0o600)
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.Use("personal"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.CodexHome, "auth.json"), auth("a", "refreshed"), 0o600); err != nil {
		t.Fatal(err)
	}
	if warning, err := s.Use("work"); err != nil || warning != "" {
		t.Fatalf("Use() = warning %q, error %v", warning, err)
	}
	saved, _ := os.ReadFile(s.profilePath("personal"))
	if !strings.Contains(string(saved), "refreshed") {
		t.Fatal("refreshed token was not preserved")
	}
}

func TestUseDoesNotOverwriteOnAccountMismatch(t *testing.T) {
	s := newTestStore(t)
	_, err := s.Login("personal", func(home string) error {
		return os.WriteFile(filepath.Join(home, "auth.json"), auth("a", "safe"), 0o600)
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.CodexHome, "auth.json"), auth("other", "foreign"), 0o600); err != nil {
		t.Fatal(err)
	}
	warning, err := s.Use("personal")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(warning, "different account") {
		t.Fatalf("unexpected warning: %q", warning)
	}
	saved, _ := os.ReadFile(s.profilePath("personal"))
	if strings.Contains(string(saved), "foreign") {
		t.Fatal("profile was overwritten with foreign credentials")
	}
}

func TestReloginDoesNotRestoreOldCredentials(t *testing.T) {
	s := newTestStore(t)
	_, err := s.Login("personal", func(home string) error {
		return os.WriteFile(filepath.Join(home, "auth.json"), auth("a", "old"), 0o600)
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.Login("personal", func(home string) error {
		return os.WriteFile(filepath.Join(home, "auth.json"), auth("a", "new"), 0o600)
	})
	if err != nil {
		t.Fatal(err)
	}
	saved, _ := os.ReadFile(s.profilePath("personal"))
	if !strings.Contains(string(saved), "new") {
		t.Fatalf("re-login was overwritten by old active credentials: %s", saved)
	}
}

func TestInvalidProfileNames(t *testing.T) {
	s := newTestStore(t)
	for _, name := range []string{"", "../work", "/tmp/work", "name with spaces"} {
		if _, err := s.Use(name); err == nil {
			t.Fatalf("Use(%q) succeeded", name)
		}
	}
}

func TestSetRootCredentialStoreBeforeTables(t *testing.T) {
	input := []byte("model = \"x\"\n[history]\npersistence = \"none\"\n")
	result := setRootCredentialStore(input)
	if !strings.HasPrefix(string(result), "# Managed by codexctl") {
		t.Fatalf("setting was not inserted at root: %s", result)
	}
	if !rootCredentialStoreIsFile(result) {
		t.Fatal("file store was not detected")
	}
}
