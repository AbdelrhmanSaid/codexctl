package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/AbdelrhmanSaid/codexctl/internal/codex"
)

func chatgptJSON(account string) []byte {
	return fmt.Appendf(nil, `{"tokens":{"account_id":"acct-%s","refresh_token":"r-%s"}}`, account, account)
}

func (h *harness) seedChatGPT(t *testing.T, names ...string) {
	t.Helper()
	for _, name := range names {
		data := chatgptJSON(name)
		if _, err := h.store().Login(name, func(home string) error {
			return os.WriteFile(filepath.Join(home, "auth.json"), data, 0o600)
		}); err != nil {
			t.Fatal(err)
		}
	}
}

func TestUsageCommand(t *testing.T) {
	h := newHarness(t)
	h.seedChatGPT(t, "home", "work")
	h.seed(t, "key")
	h.seedChatGPT(t, "work")
	resets := time.Date(2026, 10, 1, 9, 30, 0, 0, time.Local)
	h.codex.usage = map[string]codex.Usage{
		"acct-home": {AccountID: "acct-home", Primary: &codex.Window{UsedPercent: 15, Minutes: 300, ResetsAt: resets}, Secondary: &codex.Window{UsedPercent: 6, Minutes: 10080}},
		"acct-work": {AccountID: "acct-work", Primary: &codex.Window{UsedPercent: 90, Minutes: 300}},
	}

	stdout, stderr, err := h.run(t, "", "usage")
	if err != nil {
		t.Fatalf("%v\n%s", err, stderr)
	}
	for _, want := range []string{"5H LEFT", "WEEKLY LEFT", "  home", "85%", "2026-10-01 09:30", "94%", "* work", "10%", "  key"} {
		assertContains(t, stdout, want)
	}

	stdout, _, err = h.run(t, "", "usage", "--json", "work", "key")
	if err != nil {
		t.Fatal(err)
	}
	var entries []struct {
		Name     string       `json:"name"`
		Selected bool         `json:"selected"`
		Usage    *codex.Usage `json:"usage"`
		Error    string       `json:"error"`
	}
	if err := json.Unmarshal([]byte(stdout), &entries); err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 || !entries[0].Selected || entries[0].Usage.Primary.UsedPercent != 90 || entries[1].Error == "" {
		t.Fatalf("entries = %+v", entries)
	}
}

func TestUsageCommandReportsFailures(t *testing.T) {
	h := newHarness(t)
	h.seedChatGPT(t, "home")
	_, stderr, err := h.run(t, "", "usage")
	if err == nil || !strings.Contains(err.Error(), "1 profile") {
		t.Fatalf("err = %v, want a failure", err)
	}
	assertContains(t, stderr, "home: 401 Unauthorized")
}

func TestDashboardRowsShowUsage(t *testing.T) {
	h := newHarness(t)
	h.seedChatGPT(t, "home", "work")
	profiles, err := h.store().Profiles()
	if err != nil {
		t.Fatal(err)
	}
	check := &usageCheck{names: []string{"work"}}
	usage := map[string]usageResult{
		"home": {Usage: &codex.Usage{Primary: &codex.Window{UsedPercent: 40, Minutes: 300, ResetsAt: time.Now().Add(3*time.Hour + time.Minute)}}},
	}
	rows := dashboardRows(profiles, usage, check)
	if len(rows[0].Meters) != 1 || rows[0].Meters[0].Label != "5h" || rows[0].Meters[0].Left != 60 {
		t.Fatalf("home meters = %+v", rows[0].Meters)
	}
	assertContains(t, strings.Join(rows[0].Extra, " · "), "5h resets in 3 hours")
	if rows[1].Status != "checking usage…" {
		t.Fatalf("work status = %q", rows[1].Status)
	}

	usage["work"] = usageResult{Err: fmt.Errorf("401 Unauthorized")}
	rows = dashboardRows(profiles, usage, nil)
	if rows[1].Status != "usage unavailable" {
		t.Fatalf("work status = %q", rows[1].Status)
	}
	assertContains(t, strings.Join(rows[1].Extra, " · "), "401 Unauthorized")
}
