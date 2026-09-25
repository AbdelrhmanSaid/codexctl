package store

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestLoginSavesAndActivates(t *testing.T) {
	s := newTestStore(t)
	data := chatgptAuth(t, "acct-a", "r1")
	warning, err := s.Login("a", fakeLogin(data))
	if err != nil {
		t.Fatal(err)
	}
	assertWarning(t, warning, "")
	assertFile(t, s.profilePath("a"), data)
	assertFile(t, s.authPath(), data)
	assertCurrent(t, s, "a")
	if !rootCredentialStoreIsFile(readBytes(t, s.configPath())) {
		t.Fatal("login did not configure file-backed credentials")
	}
	assertMissing(t, s.pendingPath())
	assertNoIsolatedHomes(t, s)
}

func TestLoginRunsInIsolatedHome(t *testing.T) {
	s := newTestStore(t)
	_, err := s.Login("a", func(home string) error {
		if filepath.Clean(home) == filepath.Clean(s.CodexHome) {
			t.Fatal("login ran in the real Codex home")
		}
		if !rootCredentialStoreIsFile(readBytes(t, filepath.Join(home, "config.toml"))) {
			t.Fatal("isolated home is not configured for file-backed credentials")
		}
		return fakeLogin(chatgptAuth(t, "acct-a", "r1"))(home)
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestFailedLoginLeavesStateUntouched(t *testing.T) {
	tests := []struct {
		name  string
		login func(string) error
	}{
		{"codex fails", func(string) error { return errors.New("cancelled") }},
		{"no auth.json", func(string) error { return nil }},
		{"invalid auth.json", fakeLogin([]byte("not json"))},
		{"auth.json is not an object", fakeLogin([]byte("[]"))},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newTestStore(t)
			a := chatgptAuth(t, "acct-a", "r1")
			mustLogin(t, s, "a", a)

			if _, err := s.Login("b", tt.login); err == nil {
				t.Fatal("login succeeded")
			}
			assertFile(t, s.authPath(), a)
			assertFile(t, s.profilePath("a"), a)
			assertMissing(t, s.profilePath("b"))
			assertCurrent(t, s, "a")
			assertNoIsolatedHomes(t, s)
		})
	}
}

func TestFailedFirstLoginDoesNotTouchCodexHome(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.Login("a", func(string) error { return errors.New("cancelled") }); err == nil {
		t.Fatal("login succeeded")
	}
	assertMissing(t, s.CodexHome)
}

func TestLoginAgainReplacesProfile(t *testing.T) {
	s := newTestStore(t)
	mustLogin(t, s, "a", chatgptAuth(t, "acct-a", "r1"))
	again := chatgptAuth(t, "acct-a", "r2")
	mustLogin(t, s, "a", again)
	assertFile(t, s.profilePath("a"), again)
	assertFile(t, s.authPath(), again)
}

func TestLoginRemovesAbandonedIsolatedHomes(t *testing.T) {
	s := newTestStore(t)
	stale := filepath.Join(s.StateHome, loginDirPrefix+"stale")
	writeBytes(t, filepath.Join(stale, "auth.json"), chatgptAuth(t, "acct-x", "r1"))
	mustLogin(t, s, "a", chatgptAuth(t, "acct-a", "r1"))
	assertMissing(t, stale)
}

func TestUseActivatesProfile(t *testing.T) {
	s := newTestStore(t)
	a := chatgptAuth(t, "acct-a", "r1")
	b := chatgptAuth(t, "acct-b", "r1")
	mustLogin(t, s, "a", a)
	mustLogin(t, s, "b", b)

	warning, err := s.Use("a")
	if err != nil {
		t.Fatal(err)
	}
	assertWarning(t, warning, "")
	assertFile(t, s.authPath(), a)
	assertCurrent(t, s, "a")
	assertMissing(t, s.pendingPath())
}

func TestSwitchingKeepsRefreshedTokens(t *testing.T) {
	s := newTestStore(t)
	mustLogin(t, s, "a", chatgptAuth(t, "acct-a", "r1"))

	// Codex refreshes a's session, then b logs in.
	refreshedA := chatgptAuth(t, "acct-a", "r2")
	writeBytes(t, s.authPath(), refreshedA)
	mustLogin(t, s, "b", chatgptAuth(t, "acct-b", "r1"))
	assertFile(t, s.profilePath("a"), refreshedA)

	// Codex refreshes b's session, then the user switches back to a.
	refreshedB := chatgptAuth(t, "acct-b", "r2")
	writeBytes(t, s.authPath(), refreshedB)
	if _, err := s.Use("a"); err != nil {
		t.Fatal(err)
	}
	assertFile(t, s.profilePath("b"), refreshedB)
	assertFile(t, s.authPath(), refreshedA)
}

func TestSwitchingNeverSavesAnotherAccount(t *testing.T) {
	tests := []struct {
		name    string
		active  func(t *testing.T) []byte
		warning string
	}{
		{"different account", func(t *testing.T) []byte { return chatgptAuth(t, "acct-x", "r1") }, "different account"},
		{"unverifiable account", func(t *testing.T) []byte { return apiKeyAuth(t, "sk-other") }, "could not verify"},
		{"invalid auth.json", func(*testing.T) []byte { return []byte("{") }, "invalid"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newTestStore(t)
			a := chatgptAuth(t, "acct-a", "r1")
			b := chatgptAuth(t, "acct-b", "r1")
			mustLogin(t, s, "b", b)
			mustLogin(t, s, "a", a)

			// Something other than codexctl replaced auth.json.
			writeBytes(t, s.authPath(), tt.active(t))
			warning, err := s.Use("b")
			if err != nil {
				t.Fatal(err)
			}
			assertWarning(t, warning, tt.warning)
			assertFile(t, s.profilePath("a"), a)
			assertFile(t, s.authPath(), b)
			assertCurrent(t, s, "b")
		})
	}
}

func TestUseRejectsMissingOrInvalidProfile(t *testing.T) {
	s := newTestStore(t)
	a := chatgptAuth(t, "acct-a", "r1")
	mustLogin(t, s, "a", a)
	writeBytes(t, s.profilePath("broken"), []byte("not json"))

	for _, name := range []string{"missing", "broken", "../a", ""} {
		if _, err := s.Use(name); err == nil {
			t.Fatalf("use %q succeeded", name)
		}
		assertFile(t, s.authPath(), a)
		assertCurrent(t, s, "a")
	}
}

func TestUseAddsFileCredentialStoreToConfig(t *testing.T) {
	s := newTestStore(t)
	mustLogin(t, s, "a", chatgptAuth(t, "acct-a", "r1"))
	writeBytes(t, s.configPath(), []byte("model = \"gpt-5\"\n\n[tui]\ntheme = \"dark\"\n"))

	if _, err := s.Use("a"); err != nil {
		t.Fatal(err)
	}
	config := string(readBytes(t, s.configPath()))
	if !rootCredentialStoreIsFile([]byte(config)) {
		t.Fatalf("config.toml was not updated:\n%s", config)
	}
	if !strings.Contains(config, "model = \"gpt-5\"") || !strings.Contains(config, "[tui]\ntheme = \"dark\"") {
		t.Fatalf("existing settings were lost:\n%s", config)
	}
}

// An interrupted switch from b to a wrote auth.json but not current. Without
// recovery, the next command would see a's credentials under the name b.
func interruptedSwitch(t *testing.T) (*Store, []byte, []byte) {
	t.Helper()
	s := newTestStore(t)
	a := chatgptAuth(t, "acct-a", "r1")
	b := chatgptAuth(t, "acct-b", "r1")
	mustLogin(t, s, "a", a)
	mustLogin(t, s, "b", b)
	writeBytes(t, s.pendingPath(), []byte("a\n"))
	writeBytes(t, s.authPath(), a)
	return s, a, b
}

func TestUseFinishesInterruptedSwitch(t *testing.T) {
	s, _, b := interruptedSwitch(t)
	warning, err := s.Use("b")
	if err != nil {
		t.Fatal(err)
	}
	assertWarning(t, warning, "")
	assertFile(t, s.profilePath("b"), b)
	assertFile(t, s.authPath(), b)
	assertCurrent(t, s, "b")
	assertMissing(t, s.pendingPath())
}

func TestSyncFinishesInterruptedSwitch(t *testing.T) {
	s, a, b := interruptedSwitch(t)
	name, err := s.Sync()
	if err != nil {
		t.Fatal(err)
	}
	if name != "a" {
		t.Fatalf("synced %q, want a", name)
	}
	assertFile(t, s.authPath(), a)
	assertFile(t, s.profilePath("b"), b)
	assertCurrent(t, s, "a")
	assertMissing(t, s.pendingPath())
}

func TestInvalidPendingMarkerStopsSwitch(t *testing.T) {
	s := newTestStore(t)
	a := chatgptAuth(t, "acct-a", "r1")
	mustLogin(t, s, "a", a)
	writeBytes(t, s.pendingPath(), []byte("../../etc\n"))
	if _, err := s.Use("a"); err == nil || !strings.Contains(err.Error(), "pending activation") {
		t.Fatalf("err = %v, want a pending activation error", err)
	}
}

func TestSync(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.Sync(); err == nil {
		t.Fatal("sync without a selected profile succeeded")
	}

	mustLogin(t, s, "a", chatgptAuth(t, "acct-a", "r1"))
	refreshed := chatgptAuth(t, "acct-a", "r2")
	writeBytes(t, s.authPath(), refreshed)
	if name, err := s.Sync(); err != nil || name != "a" {
		t.Fatalf("sync = %q, %v", name, err)
	}
	assertFile(t, s.profilePath("a"), refreshed)

	writeBytes(t, s.authPath(), chatgptAuth(t, "acct-x", "r1"))
	if _, err := s.Sync(); err == nil || !strings.Contains(err.Error(), "different account") {
		t.Fatalf("err = %v, want a different-account error", err)
	}
	assertFile(t, s.profilePath("a"), refreshed)
}

func TestCurrentReportsMismatch(t *testing.T) {
	s := newTestStore(t)
	if name, _, err := s.Current(); err != nil || name != "" {
		t.Fatalf("current = %q, %v; want no selection", name, err)
	}
	mustLogin(t, s, "a", chatgptAuth(t, "acct-a", "r1"))
	if name, matches, err := s.Current(); err != nil || name != "a" || !matches {
		t.Fatalf("current = %q, %v, %v", name, matches, err)
	}
	writeBytes(t, s.authPath(), chatgptAuth(t, "acct-x", "r1"))
	if name, matches, err := s.Current(); err != nil || name != "a" || matches {
		t.Fatalf("current = %q, %v, %v; want a mismatch", name, matches, err)
	}
}

func TestLockIsExclusive(t *testing.T) {
	s := newTestStore(t)
	release, err := s.lock()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Use("a"); err == nil || !strings.Contains(err.Error(), "another codexctl operation") {
		t.Fatalf("err = %v, want a lock error", err)
	}
	release()
	again, err := s.lock()
	if err != nil {
		t.Fatalf("lock was not released: %v", err)
	}
	again()
}

func TestSymlinksAreRefused(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("creating symlinks needs extra privileges on Windows")
	}
	tests := []struct {
		name string
		path func(*Store) string
	}{
		{"active auth.json", (*Store).authPath},
		{"profile being activated", func(s *Store) string { return s.profilePath("a") }},
		{"current marker", (*Store).currentPath},
		{"config.toml", (*Store).configPath},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newTestStore(t)
			mustLogin(t, s, "a", chatgptAuth(t, "acct-a", "r1"))
			mustLogin(t, s, "b", chatgptAuth(t, "acct-b", "r1"))

			target := filepath.Join(t.TempDir(), "target")
			original := chatgptAuth(t, "acct-target", "r1")
			writeBytes(t, target, original)
			link := tt.path(s)
			if err := os.Remove(link); err != nil && !os.IsNotExist(err) {
				t.Fatal(err)
			}
			if err := os.Symlink(target, link); err != nil {
				t.Fatal(err)
			}

			if _, err := s.Use("a"); err == nil {
				t.Fatal("use succeeded through a symlink")
			}
			assertFile(t, target, original)
			if info, err := os.Lstat(link); err != nil || info.Mode()&os.ModeSymlink == 0 {
				t.Fatalf("symlink at %s was replaced", link)
			}
		})
	}
}

func TestSymlinkedStateHomeIsRefused(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("creating symlinks needs extra privileges on Windows")
	}
	s := newTestStore(t)
	target := t.TempDir()
	if err := os.Symlink(target, s.StateHome); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Login("a", fakeLogin(chatgptAuth(t, "acct-a", "r1"))); err == nil {
		t.Fatal("login succeeded with a symlinked state directory")
	}
	if entries, _ := os.ReadDir(target); len(entries) != 0 {
		t.Fatalf("files were written through the symlink: %v", entries)
	}
}

func TestFilePermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX modes do not apply on Windows")
	}
	s := newTestStore(t)
	mustLogin(t, s, "a", chatgptAuth(t, "acct-a", "r1"))
	for path, want := range map[string]os.FileMode{
		s.StateHome:        0o700,
		s.profilesDir():    0o700,
		s.profilePath("a"): 0o600,
		s.currentPath():    0o600,
		s.authPath():       0o600,
	} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if got := info.Mode().Perm(); got != want {
			t.Errorf("%s has mode %o, want %o", path, got, want)
		}
	}
}

func TestValidateName(t *testing.T) {
	valid := []string{"a", "work", "Personal-2", "team.alpha", "a_b", "0", strings.Repeat("x", 64)}
	invalid := []string{"", "-a", ".a", "_a", "../a", "a/b", `a\b`, "a b", "a:b", strings.Repeat("x", 65), "é"}
	for _, name := range valid {
		if err := validateName(name); err != nil {
			t.Errorf("validateName(%q) = %v, want nil", name, err)
		}
	}
	for _, name := range invalid {
		if validateName(name) == nil {
			t.Errorf("validateName(%q) accepted an invalid name", name)
		}
	}
}
