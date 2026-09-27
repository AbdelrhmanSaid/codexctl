package cli

import (
	"testing"
)

func TestRootWithoutTerminalPrintsHelp(t *testing.T) {
	h := newHarness(t)
	stdout, _, err := h.run(t, "")
	if err != nil {
		t.Fatal(err)
	}
	assertContains(t, stdout, "Usage:")
	if _, _, err := h.run(t, "", "nonsense"); err == nil {
		t.Fatal("an unknown command succeeded")
	}
}

func TestDashboardOptions(t *testing.T) {
	h := newHarness(t)
	h.seed(t, "home", "work")
	s := h.store()
	profiles, err := s.Profiles()
	if err != nil {
		t.Fatal(err)
	}
	opts := dashboardOptions(s, profiles)
	if len(opts.Rows) != 2 || !opts.Rows[1].Active {
		t.Fatalf("rows = %+v, want work active", opts.Rows)
	}
	for _, action := range opts.Actions {
		if action.Key == "R" {
			t.Fatal("restart offered without a running daemon")
		}
	}

	h.daemon = running(42)
	opts = dashboardOptions(h.store(), profiles)
	found := false
	for _, action := range opts.Actions {
		found = found || action.Key == "R"
	}
	if !found {
		t.Fatal("restart not offered with a running daemon")
	}
}
