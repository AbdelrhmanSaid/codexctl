package store

import (
	"bytes"
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

func TestImportSavesActiveLogin(t *testing.T) {
	s := newTestStore(t)
	active := chatgptAuth(t, "acct-a", "r1")
	writeBytes(t, s.authPath(), active)

	if err := s.Import("existing"); err != nil {
		t.Fatal(err)
	}
	assertFile(t, s.profilePath("existing"), active)
	assertFile(t, s.authPath(), active)
	assertCurrent(t, s, "existing")
	if !rootCredentialStoreIsFile(readBytes(t, s.configPath())) {
		t.Fatal("import did not configure file-backed credentials")
	}
}

func TestImportRefusals(t *testing.T) {
	tests := []struct {
		name  string
		setup func(t *testing.T, s *Store)
		want  string
	}{
		{"no auth.json", func(*testing.T, *Store) {}, "no active auth.json"},
		{"invalid auth.json", func(t *testing.T, s *Store) {
			writeBytes(t, s.authPath(), []byte("nope"))
		}, "invalid"},
		{"name taken", func(t *testing.T, s *Store) {
			mustLogin(t, s, "new", chatgptAuth(t, "acct-a", "r1"))
			writeBytes(t, s.authPath(), chatgptAuth(t, "acct-b", "r1"))
		}, "already exists"},
		{"account already saved", func(t *testing.T, s *Store) {
			mustLogin(t, s, "a", chatgptAuth(t, "acct-a", "r1"))
			writeBytes(t, s.authPath(), chatgptAuth(t, "acct-a", "r2"))
		}, `already saved as profile "a"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newTestStore(t)
			tt.setup(t, s)
			before := s.selectedName()
			err := s.Import("new")
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, want it to mention %q", err, tt.want)
			}
			assertCurrent(t, s, before)
		})
	}
}

// fakeLogout stands in for `codex logout` and records the credentials it was
// given.
func fakeLogout(t *testing.T, got *[]byte, err error) func(string) error {
	return func(home string) error {
		*got = readBytes(t, filepath.Join(home, "auth.json"))
		return err
	}
}

func TestLogoutSelectedProfile(t *testing.T) {
	s := newTestStore(t)
	mustLogin(t, s, "a", chatgptAuth(t, "acct-a", "r1"))
	refreshed := chatgptAuth(t, "acct-a", "r2")
	writeBytes(t, s.authPath(), refreshed)

	var loggedOut []byte
	warning, err := s.Logout("a", fakeLogout(t, &loggedOut, nil))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(loggedOut, refreshed) {
		t.Fatalf("codex logout got stale credentials:\n%s", loggedOut)
	}
	assertWarning(t, warning, "signed out")
	assertMissing(t, s.profilePath("a"))
	assertMissing(t, s.currentPath())
	assertMissing(t, s.authPath())
	assertNoIsolatedHomes(t, s)
}

func TestLogoutOtherProfileKeepsActiveLogin(t *testing.T) {
	s := newTestStore(t)
	a := chatgptAuth(t, "acct-a", "r1")
	b := chatgptAuth(t, "acct-b", "r1")
	mustLogin(t, s, "a", a)
	mustLogin(t, s, "b", b)

	var loggedOut []byte
	warning, err := s.Logout("a", fakeLogout(t, &loggedOut, nil))
	if err != nil {
		t.Fatal(err)
	}
	assertWarning(t, warning, "")
	if !bytes.Equal(loggedOut, a) {
		t.Fatalf("codex logout got the wrong credentials:\n%s", loggedOut)
	}
	assertMissing(t, s.profilePath("a"))
	assertFile(t, s.authPath(), b)
	assertCurrent(t, s, "b")
}

func TestLogoutKeepsAuthJSONOfAnotherAccount(t *testing.T) {
	s := newTestStore(t)
	a := chatgptAuth(t, "acct-a", "r1")
	mustLogin(t, s, "a", a)
	other := chatgptAuth(t, "acct-x", "r1")
	writeBytes(t, s.authPath(), other)

	var loggedOut []byte
	warning, err := s.Logout("a", fakeLogout(t, &loggedOut, nil))
	if err != nil {
		t.Fatal(err)
	}
	assertWarning(t, warning, "different account")
	if !bytes.Equal(loggedOut, a) {
		t.Fatalf("codex logout got the wrong credentials:\n%s", loggedOut)
	}
	assertFile(t, s.authPath(), other)
	assertMissing(t, s.profilePath("a"))
	assertMissing(t, s.currentPath())
}

func TestFailedLogoutKeepsProfile(t *testing.T) {
	s := newTestStore(t)
	a := chatgptAuth(t, "acct-a", "r1")
	mustLogin(t, s, "a", a)

	var loggedOut []byte
	if _, err := s.Logout("a", fakeLogout(t, &loggedOut, errors.New("network down"))); err == nil {
		t.Fatal("logout succeeded")
	}
	assertFile(t, s.profilePath("a"), a)
	assertFile(t, s.authPath(), a)
	assertCurrent(t, s, "a")
	assertNoIsolatedHomes(t, s)
}

func TestLogoutMissingProfile(t *testing.T) {
	s := newTestStore(t)
	called := false
	if _, err := s.Logout("missing", func(string) error { called = true; return nil }); err == nil {
		t.Fatal("logout of a missing profile succeeded")
	}
	if called {
		t.Fatal("codex logout ran for a missing profile")
	}
}
