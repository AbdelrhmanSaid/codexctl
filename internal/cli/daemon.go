package cli

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/AbdelrhmanSaid/codexctl/internal/codex"
	"github.com/AbdelrhmanSaid/codexctl/internal/daemon"
	"github.com/AbdelrhmanSaid/codexctl/internal/store"
	"github.com/AbdelrhmanSaid/codexctl/internal/tui"

	"github.com/spf13/cobra"
)

// A daemon caches credentials when it starts. One that is not verifiably
// running is never restarted, since Codex would start one.
func (a *app) offerDaemonRestart(cmd *cobra.Command, profileStore *store.Store, authChanged bool) {
	if !authChanged {
		return
	}

	state := profileStore.Daemon()
	errOut := cmd.ErrOrStderr()

	switch state.State {
	case daemon.NotRunning:
		return
	case daemon.Unknown:
		a.warn(cmd, "cannot tell whether a Codex app-server daemon is running: "+state.Reason+"; if one is, run 'codexctl restart-daemon' so it reloads the new credentials")
		return
	}

	if a.tui {
		restart, err := tui.Confirm(env(cmd), tui.ConfirmOptions{
			Title: "Restart the Codex app-server daemon now?",
			Description: []string{
				fmt.Sprintf("It is running (pid %d) and still uses the previous credentials.", state.PID),
				"Restarting loads the new ones but interrupts every active Codex session.",
			},
			Affirmative: "Restart now",
			Negative:    "Later",
			Danger:      true,
		})
		if err != nil || !restart {
			fmt.Fprint(errOut, errTheme(cmd).Hint("Run 'codexctl restart-daemon' when you are ready."))
			return
		}

		if err := a.restartDaemon(cmd, profileStore); err != nil && !errors.Is(err, tui.ErrCancelled) {
			a.warn(cmd, err.Error())
		}

		return
	}

	fmt.Fprintf(errOut, "A Codex app-server daemon (pid %d) is running and still uses the previous credentials.\n", state.PID)
	fmt.Fprintln(errOut, "Restarting it loads the new credentials but interrupts active Codex sessions.")

	if !a.interactive {
		fmt.Fprintln(errOut, "Run 'codexctl restart-daemon' when you are ready.")
		return
	}

	if !confirm(cmd, "Restart it now? [y/N] ") {
		fmt.Fprintln(errOut, "Left the daemon running. Run 'codexctl restart-daemon' when you are ready.")
		return
	}

	if err := a.restartDaemon(cmd, profileStore); err != nil {
		a.warn(cmd, err.Error())
	}
}

// Anything but an explicit yes is no.
func confirm(cmd *cobra.Command, question string) bool {
	fmt.Fprint(cmd.ErrOrStderr(), question)

	line, err := bufio.NewReader(cmd.InOrStdin()).ReadString('\n')
	if err != nil && line == "" {
		fmt.Fprintln(cmd.ErrOrStderr())
		return false
	}

	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
		return true
	}

	return false
}

// The caller has checked that a daemon is running.
func (a *app) restartDaemon(cmd *cobra.Command, profileStore *store.Store) error {
	codexClient, err := a.findCodex()
	if err != nil {
		return fmt.Errorf("cannot restart the daemon: %w", err)
	}

	// Codex prints a JSON result on stdout; only its stderr is kept.
	err = a.quietly(cmd, "Restarting the Codex app-server daemon", "", func(stdio codex.Stdio) error {
		if err := codexClient.RestartDaemon(profileStore.CodexHome, codex.Stdio{Out: io.Discard, Err: stdio.Err}); err != nil {
			return fmt.Errorf("restarting the Codex app-server daemon failed; check whether it is still running with 'codexctl doctor': %w", err)
		}

		return nil
	})
	if err != nil {
		return err
	}

	a.success(cmd, say("Restarted the Codex app-server daemon").withHint("It now uses the active auth.json."))

	return nil
}

func (a *app) newRestartDaemonCommand() *cobra.Command {
	var yes bool

	cmd := &cobra.Command{
		Use:   "restart-daemon",
		Short: "Restart the Codex app-server daemon so it reloads the active credentials",
		Long: "Restart the shared Codex app-server daemon so it reloads $CODEX_HOME/auth.json.\n" +
			"The daemon caches credentials when it starts and offers no way to reload them, so this is\n" +
			"the only way to make it use a newly selected profile. Restarting interrupts every Codex\n" +
			"session running on the daemon. Nothing happens unless a daemon is verifiably running.",
		Args: cobra.NoArgs,
		RunE: a.withStore(func(cmd *cobra.Command, profileStore *store.Store, _ []string) error {
			var state store.DaemonState
			if err := a.busy(cmd, "Looking for the Codex app-server daemon", func() error {
				state = profileStore.Daemon()
				return nil
			}); err != nil {
				return err
			}

			switch state.State {
			case daemon.NotRunning:
				return fmt.Errorf("no Codex app-server daemon is running for %s; codexctl does not start one", profileStore.CodexHome)
			case daemon.Unknown:
				return fmt.Errorf("refusing to restart: cannot tell whether a Codex app-server daemon is running: %s", state.Reason)
			}

			if a.tui && !yes {
				if err := confirmDanger(cmd, fmt.Sprintf("Restart the Codex app-server daemon (pid %d)?", state.PID), "Restart",
					"This interrupts every active Codex session; a turn in progress may be lost."); err != nil {
					return err
				}
			} else {
				fmt.Fprintf(cmd.ErrOrStderr(), "Restarting the Codex app-server daemon (pid %d); this interrupts active Codex sessions.\n", state.PID)
			}

			return a.restartDaemon(cmd, profileStore)
		}),
	}

	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "do not ask for confirmation on a terminal")

	return cmd
}
