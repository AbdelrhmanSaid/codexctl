package cli

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/AbdelrhmanSaid/codexctl/internal/codex"
	"github.com/AbdelrhmanSaid/codexctl/internal/daemon"
	"github.com/AbdelrhmanSaid/codexctl/internal/store"

	"github.com/spf13/cobra"
)

// isTerminal reports whether f is an interactive terminal, in which case a
// command may ask a question before restarting the daemon.
func isTerminal(f *os.File) bool {
	info, err := f.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

// offerDaemonRestart runs after a command that may have changed the active
// auth.json. The Codex app-server daemon caches credentials when it starts,
// so if one is running it still uses the previous account until restarted.
// A restart interrupts active sessions, so it only happens when the user
// answers yes on a terminal; scripts get a hint instead. A daemon that is
// not verifiably running is never restarted, since Codex would start one.
func (a *app) offerDaemonRestart(cmd *cobra.Command, s *store.Store) {
	if !s.AuthChanged() {
		return
	}
	state := s.Daemon()
	errOut := cmd.ErrOrStderr()
	switch state.State {
	case daemon.NotRunning:
		return
	case daemon.Unknown:
		printWarning(cmd, "cannot tell whether a Codex app-server daemon is running: "+state.Reason+"; if one is, run 'codexctl restart-daemon' so it reloads the new credentials")
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
	if err := a.restartDaemon(cmd, s); err != nil {
		printWarning(cmd, err.Error())
	}
}

// confirm asks a yes/no question on stderr and reads one line of stdin.
// Anything but an explicit yes, including end of input, is no.
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

// restartDaemon restarts the daemon of the store's Codex home. The caller has
// checked that one is running.
func (a *app) restartDaemon(cmd *cobra.Command, s *store.Store) error {
	c, err := a.findCodex()
	if err != nil {
		return fmt.Errorf("cannot restart the daemon: %w", err)
	}
	// Codex prints the restart result as JSON; keep it off stdout so the
	// command's own output stays scriptable.
	stdio := codex.Stdio{Out: cmd.ErrOrStderr(), Err: cmd.ErrOrStderr()}
	if err := c.RestartDaemon(s.CodexHome, stdio); err != nil {
		return fmt.Errorf("restarting the Codex app-server daemon failed; check whether it is still running with 'codexctl doctor': %w", err)
	}
	fmt.Fprintln(cmd.OutOrStdout(), "Restarted the Codex app-server daemon; it now uses the active auth.json.")
	return nil
}

func (a *app) newRestartDaemonCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "restart-daemon",
		Short: "Restart the Codex app-server daemon so it reloads the active credentials",
		Long: "Restart the shared Codex app-server daemon so it reloads $CODEX_HOME/auth.json.\n" +
			"The daemon caches credentials when it starts and offers no way to reload them, so this is\n" +
			"the only way to make it use a newly selected profile. Restarting interrupts every Codex\n" +
			"session running on the daemon. Nothing happens unless a daemon is verifiably running.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			s, err := a.openStore()
			if err != nil {
				return err
			}
			state := s.Daemon()
			switch state.State {
			case daemon.NotRunning:
				return fmt.Errorf("no Codex app-server daemon is running for %s; codexctl does not start one", s.CodexHome)
			case daemon.Unknown:
				return fmt.Errorf("refusing to restart: cannot tell whether a Codex app-server daemon is running: %s", state.Reason)
			}
			fmt.Fprintf(cmd.ErrOrStderr(), "Restarting the Codex app-server daemon (pid %d); this interrupts active Codex sessions.\n", state.PID)
			return a.restartDaemon(cmd, s)
		},
	}
}
