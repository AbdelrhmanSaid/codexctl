package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/AbdelrhmanSaid/codexctl/internal/store"
	"github.com/AbdelrhmanSaid/codexctl/internal/tui"

	"github.com/spf13/cobra"
)

// ErrReported means the failure is already on screen; only the exit status is
// left.
var ErrReported = errors.New("failure already reported")

var profileColumns = []string{"NAME", "AUTH", "EMAIL", "PLAN", "LAST REFRESH"}

func profileCells(profile store.Profile, stamp func(string) string) []string {
	auth := profile.AuthMode
	if !profile.Valid {
		auth = "invalid"
	}

	return []string{profile.Name, auth, profile.Email, profile.Plan, stamp(profile.LastRefresh)}
}

func profileFields(profile store.Profile, stamp func(string) string) []tui.Field {
	return []tui.Field{
		{Label: "Auth mode", Value: profile.AuthMode},
		{Label: "Account ID", Value: profile.AccountID},
		{Label: "Email", Value: profile.Email},
		{Label: "Plan", Value: profile.Plan},
		{Label: "Last refresh", Value: stamp(profile.LastRefresh)},
	}
}

func asWritten(stamp string) string { return stamp }

func (a *app) renderProfiles(cmd *cobra.Command, profiles []store.Profile, verbose, asJSON bool) error {
	out := cmd.OutOrStdout()
	switch {
	case asJSON:
		return writeJSON(out, profiles)
	case a.styledOut:
		theme := outTheme(cmd)
		if len(profiles) == 0 {
			fmt.Fprint(out, theme.Hint("No profiles yet. Run 'codexctl login' or 'codexctl import' to add one."))
			return nil
		}

		header := []string{"NAME", "ACCOUNT"}
		if verbose {
			header = profileColumns
		}

		rows := make([]tui.Row, len(profiles))
		for i, profile := range profiles {
			rows[i] = tui.Row{Cells: []string{profile.Name, profileDetail(profile)}, Active: profile.Selected, Faded: !profile.Valid}
			if verbose {
				rows[i].Cells = profileCells(profile, relativeTime)
			}
		}

		fmt.Fprint(out, theme.Table(header, rows))
		return nil
	case !verbose:
		for _, profile := range profiles {
			fmt.Fprintln(out, marker(profile.Selected)+profile.Name)
		}

		return nil
	}

	table := tabwriter.NewWriter(out, 2, 0, 2, ' ', 0)
	fmt.Fprintln(table, marker(false)+strings.Join(profileColumns, "\t"))
	for _, profile := range profiles {
		fmt.Fprintln(table, marker(profile.Selected)+strings.Join(profileCells(profile, asWritten), "\t"))
	}

	return table.Flush()
}

func (a *app) renderProfile(cmd *cobra.Command, profile store.Profile, asJSON bool) error {
	out := cmd.OutOrStdout()
	switch {
	case asJSON:
		return writeJSON(out, profile)
	case a.styledOut:
		var badges []string
		if profile.Selected {
			badges = append(badges, "active")
		}

		fmt.Fprint(out, outTheme(cmd).Card(profile.Name, badges, profileFields(profile, relativeTime)))
		return nil
	}

	table := tabwriter.NewWriter(out, 0, 0, 1, ' ', 0)
	fmt.Fprintf(table, "Name:\t%s\n", profile.Name)
	fmt.Fprintf(table, "Selected:\t%s\n", yesNo(profile.Selected))
	for _, field := range profileFields(profile, asWritten) {
		fmt.Fprintf(table, "%s:\t%s\n", field.Label, orDash(field.Value))
	}

	return table.Flush()
}

func (a *app) renderCurrent(cmd *cobra.Command, profileStore *store.Store, name string, matches bool, state store.DaemonState, asJSON bool) error {
	out := cmd.OutOrStdout()
	switch {
	case asJSON:
		return writeJSON(out, struct {
			Name        string `json:"name"`
			Matches     bool   `json:"matches"`
			DaemonStale bool   `json:"daemon_stale"`
		}{name, matches, state.Stale})
	case a.styledOut:
		theme := outTheme(cmd)
		line := theme.OK.Render("●") + " " + theme.Name(name)
		if profile, err := profileStore.Show(name); err == nil {
			if detail := profileDetail(profile); detail != "" {
				line += "  " + theme.Muted.Render(detail)
			}
		}

		fmt.Fprintln(out, line)
	default:
		fmt.Fprintln(out, name)
	}

	if !matches {
		a.warn(cmd, "the active auth.json no longer matches the selected profile")
	}

	if state.Stale {
		a.warn(cmd, fmt.Sprintf("the Codex app-server daemon (pid %d) started before this profile was selected and still uses the previous credentials; run 'codexctl restart-daemon' to reload it (this interrupts active Codex sessions)", state.PID))
	}

	return nil
}

func (a *app) renderChecks(cmd *cobra.Command, checks []store.Check, asJSON bool) error {
	type result struct {
		Message string `json:"message"`
		OK      bool   `json:"ok"`
	}

	out := cmd.OutOrStdout()
	failed := false
	results := make([]result, len(checks))
	items := make([]tui.Check, len(checks))
	for i, check := range checks {
		failed = failed || check.Warning
		results[i] = result{check.Message, !check.Warning}
		items[i] = tui.Check{Message: capitalize(check.Message), OK: !check.Warning}
	}

	// Problems first, in case the dashboard has no room for every check.
	for _, level := range []tui.Level{tui.LevelWarn, tui.LevelOK} {
		for _, item := range items {
			if item.OK == (level == tui.LevelOK) {
				a.record(level, item.Message)
			}
		}
	}

	switch {
	case asJSON:
		if err := writeJSON(out, results); err != nil {
			return err
		}
	case a.styledOut:
		fmt.Fprint(out, outTheme(cmd).Checklist(items))
		if failed {
			return ErrReported
		}
	default:
		for _, checkResult := range results {
			status := "ok"
			if !checkResult.OK {
				status = "warn"
			}

			fmt.Fprintf(out, "%-4s %s\n", status, checkResult.Message)
		}
	}

	if failed {
		return errors.New("doctor found one or more problems")
	}

	return nil
}

func writeJSON(w io.Writer, value any) error {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

func marker(selected bool) string {
	if selected {
		return "* "
	}

	return "  "
}

func yesNo(value bool) string {
	if value {
		return "yes"
	}

	return "no"
}

func orDash(value string) string {
	if value == "" {
		return "-"
	}

	return value
}

func relativeTime(stamp string) string {
	parsed, err := time.Parse(time.RFC3339Nano, stamp)
	if err != nil {
		return stamp
	}

	return parsed.Local().Format("2006-01-02 15:04") + " (" + ago(time.Since(parsed)) + ")"
}

func ago(elapsed time.Duration) string {
	if elapsed < time.Minute {
		return "just now"
	}

	return span(elapsed) + " ago"
}

func span(duration time.Duration) string {
	switch {
	case duration < time.Minute:
		return "under a minute"
	case duration < time.Hour:
		return countNoun(int(duration.Minutes()), "minute")
	case duration < 24*time.Hour:
		return countNoun(int(duration.Hours()), "hour")
	default:
		return countNoun(int(duration.Hours()/24), "day")
	}
}
