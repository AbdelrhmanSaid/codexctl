package store

import (
	"errors"
	"testing"
)

func TestPurgeRemovesStateButKeepsCodexHome(t *testing.T) {
	s := newTestStore(t)
	active := chatgptAuth(t, "acct-a", "r1")
	mustLogin(t, s, "work", active)
	config := readBytes(t, s.configPath())

	ran := false
	if err := s.Purge(func() error { ran = true; return nil }); err != nil {
		t.Fatal(err)
	}

	if !ran {
		t.Fatal("Purge did not run before")
	}

	assertMissing(t, s.StateHome)
	assertFile(t, s.AuthPath(), active)
	assertFile(t, s.configPath(), config)
}

func TestPurgeKeepsStateWhenBeforeFails(t *testing.T) {
	s := newTestStore(t)
	data := apiKeyAuth(t, "key")
	mustLogin(t, s, "work", data)

	failure := errors.New("cannot remove binary")
	if err := s.Purge(func() error { return failure }); !errors.Is(err, failure) {
		t.Fatalf("Purge error = %v, want %v", err, failure)
	}

	assertFile(t, s.profilePath("work"), data)
	assertCurrent(t, s, "work")
}

func TestPurgeWithoutStateRunsBefore(t *testing.T) {
	s := newTestStore(t)

	ran := false
	if err := s.Purge(func() error { ran = true; return nil }); err != nil {
		t.Fatal(err)
	}

	if !ran {
		t.Fatal("Purge did not run before")
	}

	assertMissing(t, s.StateHome)
}
