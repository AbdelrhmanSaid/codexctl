package cli

import (
	"testing"

	"github.com/AbdelrhmanSaid/codexctl/internal/store"
)

func TestMissingProfileNameOutsideTerminal(t *testing.T) {
	h := newHarness(t)
	h.seed(t, "work")
	for _, args := range [][]string{{"use"}, {"show"}, {"remove"}, {"logout"}, {"rename", "work"}, {"login"}, {"import"}} {
		_, _, err := h.run(t, "", args...)
		if err == nil {
			t.Fatalf("%v without a name succeeded", args)
		}
		assertContains(t, err.Error(), "missing PROFILE_NAME")
	}
}

func TestRemoveAcceptsSeveralNames(t *testing.T) {
	h := newHarness(t)
	h.seed(t, "work", "home", "spare")
	stdout, _, err := h.run(t, "", "remove", "work", "spare")
	if err != nil {
		t.Fatal(err)
	}
	assertContains(t, stdout, `Removed profile "work".`)
	assertContains(t, stdout, `Removed profile "spare".`)
	names, _, err := h.store().List()
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 1 || names[0] != "home" {
		t.Fatalf("profiles = %v, want [home]", names)
	}
}

func TestProfileItems(t *testing.T) {
	profiles := []store.Profile{
		{Name: "work", Valid: true, Identity: store.Identity{Email: "a@example.com", Plan: "plus"}},
		{Name: "key", Valid: true, Selected: true, Identity: store.Identity{AuthMode: "apikey"}},
		{Name: "broken"},
	}
	items, cursor := profileItems(profiles, false)
	if cursor != 1 {
		t.Fatalf("cursor = %d, want the selected profile", cursor)
	}
	if items[0].Detail != "a@example.com · plus" || items[1].Detail != "API key" || items[1].Badge != "active" {
		t.Fatalf("unexpected rows %+v", items)
	}
	if items[2].Disabled == "" {
		t.Fatal("an unreadable profile is selectable")
	}
	if items, _ = profileItems(profiles, true); items[2].Disabled != "" {
		t.Fatal("allowInvalid did not make the unreadable profile selectable")
	}
}
