package cli

import (
	"errors"
	"fmt"
	"time"

	"github.com/AbdelrhmanSaid/codexctl/internal/store"
	"github.com/AbdelrhmanSaid/codexctl/internal/tui"

	"github.com/spf13/cobra"
)

// ErrReported is returned by a command that has already described its
// failure on screen, so only the exit status is left to set.
var ErrReported = errors.New("failure already reported")

// printProfiles draws saved profiles for a terminal: names with account
// summaries, or with -v a table of every detail.
func printProfiles(cmd *cobra.Command, profiles []store.Profile, verbose bool) {
	t := outTheme(cmd)
	out := cmd.OutOrStdout()
	if len(profiles) == 0 {
		fmt.Fprint(out, t.Hint("No profiles yet. Run 'codexctl login' or 'codexctl import' to add one."))
		return
	}
	rows := make([]tui.Row, len(profiles))
	for i, p := range profiles {
		row := tui.Row{Active: p.Selected, Faded: !p.Valid}
		if verbose {
			auth := p.AuthMode
			if !p.Valid {
				auth = "invalid"
			}
			row.Cells = []string{p.Name, auth, p.Email, p.Plan, relativeTime(p.LastRefresh)}
		} else {
			row.Cells = []string{p.Name, profileDetail(p)}
		}
		rows[i] = row
	}
	header := []string{"NAME", "ACCOUNT"}
	if verbose {
		header = []string{"NAME", "AUTH", "EMAIL", "PLAN", "LAST REFRESH"}
	}
	fmt.Fprint(out, t.Table(header, rows))
}

// printProfileCard draws one profile's details in a box.
func printProfileCard(cmd *cobra.Command, p store.Profile) {
	var badges []string
	if p.Selected {
		badges = append(badges, "active")
	}
	fmt.Fprint(cmd.OutOrStdout(), outTheme(cmd).Card(p.Name, badges, []tui.Field{
		{Label: "Auth mode", Value: p.AuthMode},
		{Label: "Account ID", Value: p.AccountID},
		{Label: "Email", Value: p.Email},
		{Label: "Plan", Value: p.Plan},
		{Label: "Last refresh", Value: relativeTime(p.LastRefresh)},
	}))
}

// printCurrent draws the selected profile with its account summary.
func printCurrent(cmd *cobra.Command, s *store.Store, name string) {
	t := outTheme(cmd)
	line := t.OK.Render("●") + " " + t.Name(name)
	if p, err := s.Show(name); err == nil {
		if detail := profileDetail(p); detail != "" {
			line += "  " + t.Muted.Render(detail)
		}
	}
	fmt.Fprintln(cmd.OutOrStdout(), line)
}

// relativeTime renders an RFC 3339 timestamp as a local time and how long
// ago it was, or returns it unchanged if it does not parse.
func relativeTime(stamp string) string {
	at, err := time.Parse(time.RFC3339Nano, stamp)
	if err != nil {
		return stamp
	}
	return at.Local().Format("2006-01-02 15:04") + " (" + ago(time.Since(at)) + ")"
}

func ago(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return countNoun(int(d.Minutes()), "minute") + " ago"
	case d < 24*time.Hour:
		return countNoun(int(d.Hours()), "hour") + " ago"
	default:
		return countNoun(int(d.Hours()/24), "day") + " ago"
	}
}
