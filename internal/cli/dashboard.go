package cli

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/AbdelrhmanSaid/codexctl/internal/codex"
	"github.com/AbdelrhmanSaid/codexctl/internal/daemon"
	"github.com/AbdelrhmanSaid/codexctl/internal/store"
	"github.com/AbdelrhmanSaid/codexctl/internal/tui"

	"github.com/spf13/cobra"
)

func (a *app) runDashboard(cmd *cobra.Command) error {
	screen := tui.OpenScreen(env(cmd))
	defer screen.Close()

	a.dashboard = true
	defer func() { a.dashboard = false }()

	cursor, focused := 0, ""
	usage := map[string]usageResult{}
	var check *usageCheck
	// The check saves refreshed credentials, so it must not be abandoned.
	defer func() { a.awaitUsage(cmd, check) }()

	for {
		profileStore, err := a.openStore()
		if err != nil {
			return err
		}

		profiles, err := profileStore.Profiles()
		if err != nil {
			return err
		}

		check = a.startUsageCheck(cmd, profileStore, profiles, usage)
		opts := dashboardOptions(profileStore, dashboardRows(profiles, usage, check))

		if check != nil {
			pending, known := check, maps.Clone(usage)
			opts.Load = func() []tui.DashboardRow {
				<-pending.done
				return dashboardRows(profiles, withUsage(known, pending), nil)
			}
		}

		opts.Results = a.results
		// Follow the focused profile if the list changed around it.
		opts.Cursor = cursor
		for i, profile := range profiles {
			if profile.Name == focused {
				opts.Cursor = i
			}
		}

		choice, err := screen.Dashboard(opts)
		if err != nil || choice.Key == "" {
			return err
		}

		cursor, focused = choice.Row, ""
		if cursor < len(profiles) {
			focused = profiles[cursor].Name
		}

		a.results = nil
		if check != nil {
			a.awaitUsage(cmd, check)
			a.mergeUsage(usage, check)
			check = nil
		}

		if choice.Key == "f" {
			clear(usage)
			continue
		}

		args, err := a.dashboardArgs(cmd, choice, profiles)
		if err == nil {
			err = a.runSubcommand(cmd, args)
		}

		if err != nil && !errors.Is(err, tui.ErrCancelled) && !errors.Is(err, ErrReported) {
			a.record(tui.LevelFail, capitalize(err.Error()))
		}
	}
}

func (a *app) dashboardArgs(cmd *cobra.Command, choice tui.DashboardChoice, profiles []store.Profile) ([]string, error) {
	name := ""
	if choice.Row < len(profiles) {
		name = profiles[choice.Row].Name
	}

	switch choice.Key {
	case "u":
		return []string{"use", name}, nil
	case "r":
		return []string{"rename", name}, nil
	case "d":
		if err := confirmDanger(cmd, fmt.Sprintf("Remove profile %s?", name), "Remove",
			"Its saved login cannot be recovered. The active auth.json is left in place."); err != nil {
			return nil, err
		}
		return []string{"remove", name}, nil
	case "l":
		if err := confirmDanger(cmd, fmt.Sprintf("Log out of %s?", name), "Log out",
			"This ends the account's session and deletes the profile."); err != nil {
			return nil, err
		}
		return []string{"logout", name}, nil
	case "n":
		return []string{"login"}, nil
	case "i":
		return []string{"import"}, nil
	case "R":
		return []string{"restart-daemon"}, nil
	case "D":
		return []string{"doctor"}, nil
	case "U":
		return []string{"update"}, nil
	}

	return nil, fmt.Errorf("unknown dashboard action %q", choice.Key)
}

func (a *app) runSubcommand(cmd *cobra.Command, args []string) error {
	root := a.newRootCommand(cmd.InOrStdin(), cmd.OutOrStdout(), cmd.ErrOrStderr())
	root.SetArgs(args)

	return root.ExecuteContext(cmd.Context())
}

func dashboardOptions(profileStore *store.Store, rows []tui.DashboardRow) tui.DashboardOptions {
	opts := tui.DashboardOptions{Title: "codexctl", Subtitle: buildVersion(), Rows: rows}

	current, matches, err := profileStore.Current()
	switch {
	case err != nil:
		opts.Notes = append(opts.Notes, tui.Note{Text: "Cannot read the selected profile: " + err.Error(), Level: tui.LevelWarn})
	case current == "":
		opts.Notes = append(opts.Notes, tui.Note{Text: "No profile is selected."})
	case !matches:
		opts.Notes = append(opts.Notes, tui.Note{Text: "The active auth.json no longer matches profile " + current + ".", Level: tui.LevelWarn})
	}

	state := profileStore.Daemon()
	restart := false
	switch {
	case state.Stale:
		opts.Notes = append(opts.Notes, tui.Note{Text: fmt.Sprintf("The Codex daemon (pid %d) still uses the previous credentials; press R to restart it.", state.PID), Level: tui.LevelWarn})
		restart = true
	case state.State == daemon.Running:
		opts.Notes = append(opts.Notes, tui.Note{Text: fmt.Sprintf("Codex daemon running (pid %d).", state.PID)})
		restart = true
	case state.State == daemon.Unknown:
		opts.Notes = append(opts.Notes, tui.Note{Text: "Codex daemon status unknown: " + state.Reason})
	}

	opts.Actions = []tui.Action{
		{Key: "u", Label: "use", NeedsRow: true},
		{Key: "r", Label: "rename", NeedsRow: true},
		{Key: "d", Label: "remove", NeedsRow: true},
		{Key: "l", Label: "log out", NeedsRow: true},
		{Key: "n", Label: "log in"},
		{Key: "i", Label: "import"},
	}

	if restart {
		opts.Actions = append(opts.Actions, tui.Action{Key: "R", Label: "restart daemon"})
	}

	opts.Actions = append(opts.Actions,
		tui.Action{Key: "f", Label: "refresh usage"},
		tui.Action{Key: "D", Label: "doctor"},
		tui.Action{Key: "U", Label: "update"})

	return opts
}

func dashboardRows(profiles []store.Profile, usage map[string]usageResult, check *usageCheck) []tui.DashboardRow {
	rows := make([]tui.DashboardRow, len(profiles))
	for i, profile := range profiles {
		row := tui.DashboardRow{Name: profile.Name, Detail: profileDetail(profile), Active: profile.Selected, Invalid: !profile.Valid}
		if profile.Valid {
			row.Extra = profileExtra(profile)
		}

		profileUsage, ok := usage[profile.Name]
		switch {
		case !ok && check != nil && check.covers(profile.Name):
			row.Status = "checking usage…"
		case profileUsage.Usage != nil:
			var resets []string
			for _, window := range []*codex.Window{profileUsage.Usage.Primary, profileUsage.Usage.Secondary} {
				if window == nil {
					continue
				}

				name := strings.ToLower(windowName(window, "limit"))
				row.Meters = append(row.Meters, tui.Meter{Label: name, Left: left(window)})
				if remaining := resetsIn(window); remaining != "" {
					resets = append(resets, name+" resets "+remaining)
				}
			}

			row.Extra = append(resets, row.Extra...)
		case profileUsage.failed():
			row.Status = "usage unavailable"
			row.Extra = []string{"Usage unavailable: " + profileUsage.Err.Error()}
		}

		rows[i] = row
	}

	return rows
}

func profileExtra(profile store.Profile) []string {
	var parts []string
	if profile.AccountID != "" {
		parts = append(parts, "Account "+profile.AccountID)
	}

	if profile.LastRefresh != "" {
		parts = append(parts, "refreshed "+relativeTime(profile.LastRefresh))
	}

	return parts
}

// usageCheck is a fetchUsage running while the dashboard is open.
type usageCheck struct {
	names   []string
	done    chan struct{}
	results map[string]usageResult
	result  store.Result
	err     error
}

func (c *usageCheck) covers(name string) bool { return slices.Contains(c.names, name) }

func (c *usageCheck) finished() bool {
	select {
	case <-c.done:
		return true
	default:
		return false
	}
}

// Checks the profiles without a result, which is all of them after a refresh.
func (a *app) startUsageCheck(cmd *cobra.Command, profileStore *store.Store, profiles []store.Profile, usage map[string]usageResult) *usageCheck {
	var names []string
	for _, profile := range profiles {
		if _, ok := usage[profile.Name]; !ok && profile.Valid {
			names = append(names, profile.Name)
		}
	}

	if len(names) == 0 {
		return nil
	}

	codexClient, err := a.findCodex()
	if err != nil {
		for _, name := range names {
			usage[name] = usageResult{Err: err}
		}

		return nil
	}

	check := &usageCheck{names: names, done: make(chan struct{})}
	go func() {
		defer close(check.done)
		check.results, check.result, check.err = fetchUsage(cmd.Context(), profileStore, codexClient, names)
	}()

	return check
}

func (a *app) awaitUsage(cmd *cobra.Command, check *usageCheck) {
	if check == nil || check.finished() {
		return
	}

	_ = a.busy(cmd, "Finishing the usage check", func() error {
		<-check.done
		return nil
	})
}

func (a *app) mergeUsage(usage map[string]usageResult, check *usageCheck) {
	withUsage(usage, check)

	for _, warning := range check.result.Warnings {
		a.record(tui.LevelWarn, capitalize(warning))
	}
}

// withUsage adds the check's results to usage and returns it.
func withUsage(usage map[string]usageResult, check *usageCheck) map[string]usageResult {
	for _, name := range check.names {
		if check.err != nil {
			usage[name] = usageResult{Err: check.err}
		} else {
			usage[name] = check.results[name]
		}
	}

	return usage
}
