package store

import (
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func inspectAll(t *testing.T, s *Store, names []string, inspect func(name string, home ProfileHome) error) ([]error, Result) {
	t.Helper()
	errs, result, err := s.Inspect(names, inspect)
	if err != nil {
		t.Fatal(err)
	}
	assertNoIsolatedHomes(t, s)
	return errs, result
}

func TestInspectSavesRefreshedCopies(t *testing.T) {
	s := newTestStore(t)
	mustLogin(t, s, "home", chatgptAuth(t, "acct-home", "r1"))
	mustLogin(t, s, "work", chatgptAuth(t, "acct-work", "r2"))
	refreshedHome := chatgptAuth(t, "acct-home", "r1-new")
	refreshedWork := chatgptAuth(t, "acct-work", "r2-new")

	var mu sync.Mutex
	homes := map[string]ProfileHome{}
	errs, result := inspectAll(t, s, []string{"home", "work"}, func(name string, home ProfileHome) error {
		mu.Lock()
		homes[name] = home
		mu.Unlock()
		refreshed := refreshedHome
		if name == "work" {
			refreshed = refreshedWork
		}
		writeBytes(t, filepath.Join(home.Path, "auth.json"), refreshed)
		return nil
	})
	for _, err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	assertWarning(t, result, "")
	if !homes["home"].Isolated || homes["home"].AccountID != "acct-home" {
		t.Fatalf("home got %+v, want an isolated copy", homes["home"])
	}
	if homes["work"].Isolated || homes["work"].Path != s.CodexHome {
		t.Fatalf("selected profile got %+v, want the real Codex home", homes["work"])
	}
	assertFile(t, s.profilePath("home"), refreshedHome)
	assertFile(t, s.profilePath("work"), refreshedWork)
	assertFile(t, s.AuthPath(), refreshedWork)
}

func TestInspectKeepsProfileWhenCopyChangesAccount(t *testing.T) {
	s := newTestStore(t)
	saved := chatgptAuth(t, "acct-home", "r1")
	mustLogin(t, s, "home", saved)
	mustLogin(t, s, "work", chatgptAuth(t, "acct-work", "r2"))

	errs, result := inspectAll(t, s, []string{"home"}, func(_ string, home ProfileHome) error {
		writeBytes(t, filepath.Join(home.Path, "auth.json"), chatgptAuth(t, "acct-other", "r3"))
		return errors.New("boom")
	})
	if errs[0] == nil || errs[0].Error() != "boom" {
		t.Fatalf("err = %v, want the inspect error", errs[0])
	}
	assertWarning(t, result, "another account's credentials")
	assertFile(t, s.profilePath("home"), saved)
}

func TestInspectSkips(t *testing.T) {
	s := newTestStore(t)
	login := chatgptAuth(t, "acct-home", "r1")
	mustLogin(t, s, "home", login)
	mustLogin(t, s, "twin", login)
	mustLogin(t, s, "key", apiKeyAuth(t, "sk-test"))

	var mu sync.Mutex
	var inspected []string
	errs, _ := inspectAll(t, s, []string{"home", "key", "missing", "twin"}, func(name string, _ ProfileHome) error {
		mu.Lock()
		inspected = append(inspected, name)
		mu.Unlock()
		return nil
	})
	if len(inspected) != 1 || inspected[0] != "home" {
		t.Fatalf("inspected %v, want only home", inspected)
	}
	if !errors.Is(errs[1], ErrNotChatGPT) {
		t.Fatalf("API key profile: err = %v", errs[1])
	}
	if errs[2] == nil || !strings.Contains(errs[2].Error(), "does not exist") {
		t.Fatalf("missing profile: err = %v", errs[2])
	}
	if errs[3] == nil || !strings.Contains(errs[3].Error(), `same login as profile "home"`) {
		t.Fatalf("twin profile: err = %v", errs[3])
	}
}
