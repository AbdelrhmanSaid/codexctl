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

func fetchUsage(ctx context.Context, profileStore *store.Store, codexClient codexCLI, names []string) (map[string]usageResult, store.Result, error) {
	usages := make(map[string]*codex.Usage, len(names))
	var mu sync.Mutex
	errs, result, err := profileStore.Inspect(names, func(name string, home store.ProfileHome) error {
		usage, err := codexClient.Usage(ctx, home.Path, home.Isolated)
		if err != nil {
			return err
		}

		// Guards against config.toml pointing Codex at a keyring login.
		if home.AccountID != "" && usage.AccountID != "" && usage.AccountID != home.AccountID {
			return errors.New("codex answered for a different account than the profile's")
		}

		mu.Lock()
		usages[name] = &usage
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
		RunE: a.withStore(func(cmd *cobra.Command, profileStore *store.Store, args []string) error {
			codexClient, err := a.findCodex()
			if err != nil {
				return err
			}

			names := args
			if len(names) == 0 {
				if names, _, err = profileStore.List(); err != nil {
					return err
				}

				if len(names) == 0 {
					return errors.New("no profiles yet; run 'codexctl login' or 'codexctl import' to add one")
				}
			}

			var results map[string]usageResult
			var result store.Result
			if err := a.busy(cmd, "Checking usage limits", func() error {
				results, result, err = fetchUsage(cmd.Context(), profileStore, codexClient, names)
				return err
			}); err != nil {
				return err
			}

			for _, warning := range result.Warnings {
				a.warn(cmd, warning)
			}

			current, _, _ := profileStore.Current()
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
			profileUsage := results[name]
			entries[i] = entry{Name: name, Selected: name == current, Usage: profileUsage.Usage}
			if profileUsage.Err != nil {
				entries[i].Error = profileUsage.Err.Error()
			}
		}

		if err := writeJSON(out, entries); err != nil {
			return err
		}
	case a.styledOut:
		theme := outTheme(cmd)
		rows := make([]tui.Row, len(names))
		for i, name := range names {
			profileUsage := results[name]
			rows[i] = tui.Row{Cells: []string{name}, Active: name == current}
			switch {
			case profileUsage.Usage != nil:
				rows[i].Cells = append(rows[i].Cells,
					meter(theme, profileUsage.Usage.Primary), resetsIn(profileUsage.Usage.Primary),
					meter(theme, profileUsage.Usage.Secondary), resetsIn(profileUsage.Usage.Secondary))
			case profileUsage.noLimits():
				rows[i].Cells = append(rows[i].Cells, "no limits (API key)")
			default:
				rows[i].Cells = append(rows[i].Cells, "unavailable")
				rows[i].Faded = true
			}
		}

		fmt.Fprint(out, theme.Table(header, rows))
	default:
		table := tabwriter.NewWriter(out, 2, 0, 2, ' ', 0)
		fmt.Fprintln(table, marker(false)+strings.Join(header, "\t"))
		for _, name := range names {
			cells := []string{name, "-", "-", "-", "-"}
			if usage := results[name].Usage; usage != nil {
				cells = []string{name, percent(usage.Primary), resetsAt(usage.Primary), percent(usage.Secondary), resetsAt(usage.Secondary)}
			}

			fmt.Fprintln(table, marker(name == current)+strings.Join(cells, "\t"))
		}

		if err := table.Flush(); err != nil {
			return err
		}
	}

	failed := 0
	for _, name := range names {
		if profileUsage := results[name]; profileUsage.failed() {
			failed++
			a.warn(cmd, fmt.Sprintf("%s: %v", name, profileUsage.Err))
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
		if usage := results[name].Usage; usage != nil {
			return windowName(usage.Primary, short), windowName(usage.Secondary, long)
		}
	}

	return short, long
}

func windowName(window *codex.Window, fallback string) string {
	switch {
	case window == nil || window.Minutes <= 0:
		return fallback
	case window.Minutes == 7*24*60:
		return "WEEKLY"
	case window.Minutes%60 == 0:
		return fmt.Sprintf("%dH", window.Minutes/60)
	default:
		return fmt.Sprintf("%dM", window.Minutes)
	}
}

func left(window *codex.Window) float64 { return max(0, 100-window.UsedPercent) }

func percent(window *codex.Window) string {
	if window == nil {
		return "-"
	}

	return fmt.Sprintf("%.0f%%", left(window))
}

func meter(theme *tui.Theme, window *codex.Window) string {
	if window == nil {
		return ""
	}

	return theme.Meter(left(window), 10) + " " + percent(window)
}

func resetsAt(window *codex.Window) string {
	if window == nil || window.ResetsAt.IsZero() {
		return "-"
	}

	return window.ResetsAt.Local().Format("2006-01-02 15:04")
}

func resetsIn(window *codex.Window) string {
	if window == nil || window.ResetsAt.IsZero() {
		return ""
	}

	return "in " + span(time.Until(window.ResetsAt))
}
