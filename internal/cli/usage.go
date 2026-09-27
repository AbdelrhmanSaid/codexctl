package cli

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"text/tabwriter"
	"time"

	"github.com/AbdelrhmanSaid/codexctl/internal/codex"
	"github.com/AbdelrhmanSaid/codexctl/internal/store"
	"github.com/AbdelrhmanSaid/codexctl/internal/tui"

	"github.com/spf13/cobra"
)

type usageResult struct {
	Usage *codex.Usage
	Err   error
}

func (r usageResult) noLimits() bool { return errors.Is(r.Err, store.ErrNotChatGPT) }
func (r usageResult) failed() bool   { return r.Err != nil && !r.noLimits() }

func fetchUsage(ctx context.Context, s *store.Store, c codexCLI, names []string) (map[string]usageResult, store.Result, error) {
	usages := make(map[string]*codex.Usage, len(names))
	var mu sync.Mutex
	errs, result, err := s.Inspect(names, func(name string, home store.ProfileHome) error {
		u, err := c.Usage(ctx, home.Path, home.Isolated)
		if err != nil {
			return err
		}
		// Guards against config.toml pointing Codex at a keyring login.
		if home.AccountID != "" && u.AccountID != "" && u.AccountID != home.AccountID {
			return errors.New("codex answered for a different account than the profile's")
		}
		mu.Lock()
		usages[name] = &u
		mu.Unlock()
		return nil
	})
	if err != nil {
		return nil, result, err
	}
	results := make(map[string]usageResult, len(names))
	for i, name := range names {
		results[name] = usageResult{Usage: usages[name], Err: errs[i]}
	}
	return results, result, nil
}

func (a *app) newUsageCommand() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "usage [PROFILE_NAME...]",
		Short: "Show each profile's Codex usage limits",
		Long: "Ask Codex how much of each account's usage limits is used and when they reset.\n" +
			"Profiles other than the selected one are checked in an isolated directory, and\n" +
			"credentials Codex refreshes there are saved back into the profile.",
		Args:              cobra.ArbitraryArgs,
		ValidArgsFunction: a.completeProfiles,
		RunE: a.withStore(func(cmd *cobra.Command, s *store.Store, args []string) error {
			c, err := a.findCodex()
			if err != nil {
				return err
			}
			names := args
			if len(names) == 0 {
				if names, _, err = s.List(); err != nil {
					return err
				}
				if len(names) == 0 {
					return errors.New("no profiles yet; run 'codexctl login' or 'codexctl import' to add one")
				}
			}
			var results map[string]usageResult
			var result store.Result
			if err := a.busy(cmd, "Checking usage limits", func() error {
				results, result, err = fetchUsage(cmd.Context(), s, c, names)
				return err
			}); err != nil {
				return err
			}
			for _, warning := range result.Warnings {
				a.warn(cmd, warning)
			}
			current, _, _ := s.Current()
			return a.renderUsage(cmd, names, current, results, asJSON)
		}),
	}
	addJSONFlag(cmd, &asJSON)
	return cmd
}

func (a *app) renderUsage(cmd *cobra.Command, names []string, current string, results map[string]usageResult, asJSON bool) error {
	out := cmd.OutOrStdout()
	short, long := windowHeaders(names, results)
	header := []string{"NAME", short + " LEFT", "RESETS", long + " LEFT", "RESETS"}
	switch {
	case asJSON:
		type entry struct {
			Name     string       `json:"name"`
			Selected bool         `json:"selected"`
			Usage    *codex.Usage `json:"usage,omitempty"`
			Error    string       `json:"error,omitempty"`
		}
		entries := make([]entry, len(names))
		for i, name := range names {
			r := results[name]
			entries[i] = entry{Name: name, Selected: name == current, Usage: r.Usage}
			if r.Err != nil {
				entries[i].Error = r.Err.Error()
			}
		}
		if err := writeJSON(out, entries); err != nil {
			return err
		}
	case a.styledOut:
		t := outTheme(cmd)
		rows := make([]tui.Row, len(names))
		for i, name := range names {
			r := results[name]
			rows[i] = tui.Row{Cells: []string{name}, Active: name == current}
			switch {
			case r.Usage != nil:
				rows[i].Cells = append(rows[i].Cells,
					meter(t, r.Usage.Primary), resetsIn(r.Usage.Primary),
					meter(t, r.Usage.Secondary), resetsIn(r.Usage.Secondary))
			case r.noLimits():
				rows[i].Cells = append(rows[i].Cells, "no limits (API key)")
			default:
				rows[i].Cells = append(rows[i].Cells, "unavailable")
				rows[i].Faded = true
			}
		}
		fmt.Fprint(out, t.Table(header, rows))
	default:
		w := tabwriter.NewWriter(out, 2, 0, 2, ' ', 0)
		fmt.Fprintln(w, marker(false)+strings.Join(header, "\t"))
		for _, name := range names {
			cells := []string{name, "-", "-", "-", "-"}
			if u := results[name].Usage; u != nil {
				cells = []string{name, percent(u.Primary), resetsAt(u.Primary), percent(u.Secondary), resetsAt(u.Secondary)}
			}
			fmt.Fprintln(w, marker(name == current)+strings.Join(cells, "\t"))
		}
		if err := w.Flush(); err != nil {
			return err
		}
	}
	failed := 0
	for _, name := range names {
		if r := results[name]; r.failed() {
			failed++
			a.warn(cmd, fmt.Sprintf("%s: %v", name, r.Err))
		}
	}
	if failed > 0 {
		return fmt.Errorf("could not read usage limits for %s", countNoun(failed, "profile"))
	}
	return nil
}

func windowHeaders(names []string, results map[string]usageResult) (short, long string) {
	short, long = "SHORT", "LONG"
	for _, name := range names {
		if u := results[name].Usage; u != nil {
			return windowName(u.Primary, short), windowName(u.Secondary, long)
		}
	}
	return short, long
}

func windowName(w *codex.Window, fallback string) string {
	switch {
	case w == nil || w.Minutes <= 0:
		return fallback
	case w.Minutes == 7*24*60:
		return "WEEKLY"
	case w.Minutes%60 == 0:
		return fmt.Sprintf("%dH", w.Minutes/60)
	default:
		return fmt.Sprintf("%dM", w.Minutes)
	}
}

func left(w *codex.Window) float64 { return max(0, 100-w.UsedPercent) }

func percent(w *codex.Window) string {
	if w == nil {
		return "-"
	}
	return fmt.Sprintf("%.0f%%", left(w))
}

func meter(t *tui.Theme, w *codex.Window) string {
	if w == nil {
		return ""
	}
	return t.Meter(left(w), 10) + " " + percent(w)
}

func resetsAt(w *codex.Window) string {
	if w == nil || w.ResetsAt.IsZero() {
		return "-"
	}
	return w.ResetsAt.Local().Format("2006-01-02 15:04")
}

func resetsIn(w *codex.Window) string {
	if w == nil || w.ResetsAt.IsZero() {
		return ""
	}
	return "in " + span(time.Until(w.ResetsAt))
}
