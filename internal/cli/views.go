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

func profileCells(p store.Profile, stamp func(string) string) []string {
	auth := p.AuthMode
	if !p.Valid {
		auth = "invalid"
	}
	return []string{p.Name, auth, p.Email, p.Plan, stamp(p.LastRefresh)}
}

func profileFields(p store.Profile, stamp func(string) string) []tui.Field {
	return []tui.Field{
		{Label: "Auth mode", Value: p.AuthMode},
		{Label: "Account ID", Value: p.AccountID},
		{Label: "Email", Value: p.Email},
		{Label: "Plan", Value: p.Plan},
		{Label: "Last refresh", Value: stamp(p.LastRefresh)},
	}
}

func asWritten(stamp string) string { return stamp }

func (a *app) renderProfiles(cmd *cobra.Command, profiles []store.Profile, verbose, asJSON bool) error {
	out := cmd.OutOrStdout()
	switch {
	case asJSON:
		return writeJSON(out, profiles)
	case a.styledOut:
		t := outTheme(cmd)
		if len(profiles) == 0 {
			fmt.Fprint(out, t.Hint("No profiles yet. Run 'codexctl login' or 'codexctl import' to add one."))
			return nil
		}
		header := []string{"NAME", "ACCOUNT"}
		if verbose {
			header = profileColumns
		}
		rows := make([]tui.Row, len(profiles))
		for i, p := range profiles {
			rows[i] = tui.Row{Cells: []string{p.Name, profileDetail(p)}, Active: p.Selected, Faded: !p.Valid}
			if verbose {
				rows[i].Cells = profileCells(p, relativeTime)
			}
		}
		fmt.Fprint(out, t.Table(header, rows))
		return nil
	case !verbose:
		for _, p := range profiles {
			fmt.Fprintln(out, marker(p.Selected)+p.Name)
		}
		return nil
	}
	w := tabwriter.NewWriter(out, 2, 0, 2, ' ', 0)
	fmt.Fprintln(w, marker(false)+strings.Join(profileColumns, "\t"))
	for _, p := range profiles {
		fmt.Fprintln(w, marker(p.Selected)+strings.Join(profileCells(p, asWritten), "\t"))
	}
	return w.Flush()
}

func (a *app) renderProfile(cmd *cobra.Command, p store.Profile, asJSON bool) error {
	out := cmd.OutOrStdout()
	switch {
	case asJSON:
		return writeJSON(out, p)
	case a.styledOut:
		var badges []string
		if p.Selected {
			badges = append(badges, "active")
		}
		fmt.Fprint(out, outTheme(cmd).Card(p.Name, badges, profileFields(p, relativeTime)))
		return nil
	}
	w := tabwriter.NewWriter(out, 0, 0, 1, ' ', 0)
	fmt.Fprintf(w, "Name:\t%s\n", p.Name)
	fmt.Fprintf(w, "Selected:\t%s\n", yesNo(p.Selected))
	for _, f := range profileFields(p, asWritten) {
		fmt.Fprintf(w, "%s:\t%s\n", f.Label, orDash(f.Value))
	}
	return w.Flush()
}

func (a *app) renderCurrent(cmd *cobra.Command, s *store.Store, name string, matches bool, state store.DaemonState, asJSON bool) error {
	out := cmd.OutOrStdout()
	switch {
	case asJSON:
		return writeJSON(out, struct {
			Name        string `json:"name"`
			Matches     bool   `json:"matches"`
			DaemonStale bool   `json:"daemon_stale"`
		}{name, matches, state.Stale})
	case a.styledOut:
		t := outTheme(cmd)
		line := t.OK.Render("●") + " " + t.Name(name)
		if p, err := s.Show(name); err == nil {
			if detail := profileDetail(p); detail != "" {
				line += "  " + t.Muted.Render(detail)
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
		for _, r := range results {
			status := "ok"
			if !r.OK {
				status = "warn"
			}
			fmt.Fprintf(out, "%-4s %s\n", status, r.Message)
		}
	}
	if failed {
		return errors.New("doctor found one or more problems")
	}
	return nil
}

func writeJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func marker(selected bool) string {
	if selected {
		return "* "
	}
	return "  "
}

func yesNo(v bool) string {
	if v {
		return "yes"
	}
	return "no"
}

func orDash(v string) string {
	if v == "" {
		return "-"
	}
	return v
}

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
