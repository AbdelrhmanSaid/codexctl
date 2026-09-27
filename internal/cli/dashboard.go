package cli

import (
	"errors"
	"fmt"
	"strings"

	"github.com/AbdelrhmanSaid/codexctl/internal/daemon"
	"github.com/AbdelrhmanSaid/codexctl/internal/store"
	"github.com/AbdelrhmanSaid/codexctl/internal/tui"

	"github.com/spf13/cobra"
)

// runDashboard shows the profile dashboard until the user quits. Each
// action runs the matching subcommand, with its own prompts and output,
// and then the dashboard is drawn again with fresh state.
func (a *app) runDashboard(cmd *cobra.Command) error {
	cursor := 0
	for {
		s, err := a.openStore()
		if err != nil {
			return err
		}
		profiles, err := s.Profiles()
		if err != nil {
			return err
		}
		opts := dashboardOptions(s, profiles)
		opts.Cursor = cursor
		choice, err := tui.Dashboard(env(cmd), opts)
		if err != nil || choice.Key == "" {
			return err
		}
		cursor = choice.Row
		args, err := a.dashboardArgs(cmd, choice, profiles)
		if err == nil {
			err = a.runSubcommand(cmd, args)
		}
		if err != nil && !errors.Is(err, tui.ErrCancelled) && !errors.Is(err, ErrReported) {
			fmt.Fprint(cmd.ErrOrStderr(), errTheme(cmd).Failure(err.Error()))
		}
		fmt.Fprintln(cmd.ErrOrStderr())
	}
}

// dashboardArgs turns a dashboard choice into subcommand arguments, asking
// first before anything is deleted.
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
		if err := confirmDanger(cmd, fmt.Sprintf("Remove profile %s?", name),
			"Its saved login cannot be recovered. The active auth.json is left in place.", "Remove"); err != nil {
			return nil, err
		}
		return []string{"remove", name}, nil
	case "l":
		if err := confirmDanger(cmd, fmt.Sprintf("Log out of %s?", name),
			"This ends the account's session and deletes the profile.", "Log out"); err != nil {
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

// confirmDanger asks a red yes/no question and returns ErrCancelled on no.
func confirmDanger(cmd *cobra.Command, title, description, affirmative string) error {
	ok, err := tui.Confirm(env(cmd), tui.ConfirmOptions{
		Title:       title,
		Description: []string{description},
		Affirmative: affirmative,
		Negative:    "Cancel",
		Danger:      true,
	})
	if err != nil || !ok {
		return tui.ErrCancelled
	}
	return nil
}

// runSubcommand runs codexctl with args on the same streams.
func (a *app) runSubcommand(cmd *cobra.Command, args []string) error {
	root := a.newRootCommand(cmd.InOrStdin(), cmd.OutOrStdout(), cmd.ErrOrStderr())
	root.SetArgs(args)
	return root.ExecuteContext(cmd.Context())
}

// dashboardOptions describes the current state for the dashboard.
func dashboardOptions(s *store.Store, profiles []store.Profile) tui.DashboardOptions {
	opts := tui.DashboardOptions{Title: "codexctl", Subtitle: buildVersion()}
	current, matches, err := s.Current()
	switch {
	case err != nil:
		opts.Notes = append(opts.Notes, tui.Note{Text: "cannot read the selected profile: " + err.Error(), Warn: true})
	case current == "":
		opts.Notes = append(opts.Notes, tui.Note{Text: "No profile is selected."})
	case !matches:
		opts.Notes = append(opts.Notes, tui.Note{Text: "The active auth.json no longer matches profile " + current + ".", Warn: true})
	}

	state := s.Daemon()
	restart := false
	switch {
	case state.Stale:
		opts.Notes = append(opts.Notes, tui.Note{Text: fmt.Sprintf("The Codex daemon (pid %d) still uses the previous credentials; press R to restart it.", state.PID), Warn: true})
		restart = true
	case state.State == daemon.Running:
		opts.Notes = append(opts.Notes, tui.Note{Text: fmt.Sprintf("Codex daemon running (pid %d).", state.PID)})
		restart = true
	case state.State == daemon.Unknown:
		opts.Notes = append(opts.Notes, tui.Note{Text: "Codex daemon status unknown: " + state.Reason})
	}

	for _, p := range profiles {
		row := tui.DashboardRow{Name: p.Name, Detail: profileDetail(p), Active: p.Selected, Invalid: !p.Valid}
		if p.Valid {
			row.Extra = profileExtra(p)
		}
		opts.Rows = append(opts.Rows, row)
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
	opts.Actions = append(opts.Actions, tui.Action{Key: "D", Label: "doctor"}, tui.Action{Key: "U", Label: "update"})
	return opts
}

// profileExtra is the detail line shown for the focused profile.
func profileExtra(p store.Profile) string {
	var parts []string
	if p.AccountID != "" {
		parts = append(parts, "Account "+p.AccountID)
	}
	if p.LastRefresh != "" {
		parts = append(parts, "refreshed "+relativeTime(p.LastRefresh))
	}
	return strings.Join(parts, " · ")
}
