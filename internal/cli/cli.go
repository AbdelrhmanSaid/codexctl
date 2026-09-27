package cli

import (
	"errors"
	"fmt"
	"io"
	"runtime/debug"
	"strings"

	"github.com/AbdelrhmanSaid/codexctl/internal/codex"
	"github.com/AbdelrhmanSaid/codexctl/internal/store"
	"github.com/AbdelrhmanSaid/codexctl/internal/tui"
	"github.com/AbdelrhmanSaid/codexctl/internal/update"

	"github.com/spf13/cobra"
)

// version is replaced with the release tag by GoReleaser. Keep a useful
// fallback for binaries built directly with `go build` or `go install`.
var version = "dev"

func buildVersion() string {
	if version != "dev" {
		return version
	}
	info, ok := debug.ReadBuildInfo()
	if !ok || info.Main.Version == "" || info.Main.Version == "(devel)" {
		return version
	}
	return strings.TrimPrefix(info.Main.Version, "v")
}

// restartClients is the advice after the active login has changed.
const restartClients = "Restart running Codex clients to pick it up."

// codexCLI is the part of the Codex CLI that commands depend on.
type codexCLI interface {
	Login(home string, opts codex.LoginOptions, stdio codex.Stdio) error
	Logout(home string, stdio codex.Stdio) error
	RestartDaemon(home string, stdio codex.Stdio) error
}

// app holds what commands need from outside the process. The dependencies
// are resolved lazily so that help never touches the environment;
// profile-name completion only reads the profile list.
type app struct {
	openStore func() (*store.Store, error)
	findCodex func() (codexCLI, error)
	// executable locates the running binary and how it was installed.
	executable func() (string, update.Method, error)
	// interactive is whether stdin is a terminal, so commands may ask
	// before restarting the daemon.
	interactive bool
	// tui is whether prompts may use the full terminal UI: stdin and
	// stderr are terminals and the UI has not been turned off.
	tui bool
	// styledOut and styledErr are whether stdout and stderr get colors
	// and layout rather than the plain text scripts rely on.
	styledOut, styledErr bool
	// dashboard is whether the dashboard is open. It draws over what an
	// action printed, so results holds what the action reported for the
	// dashboard to show.
	dashboard bool
	results   []tui.Note
}

func Run(args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	a := &app{
		openStore: store.NewFromEnvironment,
		findCodex: func() (codexCLI, error) {
			c, err := codex.Find()
			if err != nil {
				return nil, err
			}
			return c, nil
		},
		executable: findExecutable,
	}
	a.detectTerminals(stdin, stdout, stderr)
	root := a.newRootCommand(stdin, stdout, stderr)
	root.SetArgs(args)
	return root.Execute()
}

func (a *app) newRootCommand(stdin io.Reader, stdout, stderr io.Writer) *cobra.Command {
	root := &cobra.Command{
		Use:           "codexctl",
		Short:         "Manage named Codex login profiles",
		Version:       buildVersion(),
		SilenceErrors: true,
		SilenceUsage:  true,
	}
	// Without a subcommand, a terminal gets the dashboard and anything
	// else gets the help text.
	root.RunE = func(cmd *cobra.Command, _ []string) error {
		if !a.tui {
			return cmd.Help()
		}
		return a.runDashboard(cmd)
	}
	root.SetIn(stdin)
	root.SetOut(stdout)
	root.SetErr(stderr)
	root.SetHelpCommand(&cobra.Command{Hidden: true})
	root.AddCommand(
		a.newLoginCommand(),
		a.newImportCommand(),
		a.newUseCommand(),
		a.newListCommand(),
		a.newCurrentCommand(),
		a.newShowCommand(),
		a.newSyncCommand(),
		a.newRenameCommand(),
		a.newRemoveCommand(),
		a.newLogoutCommand(),
		a.newDoctorCommand(),
		a.newRestartDaemonCommand(),
		a.newUpdateCommand(),
		a.newUninstallCommand(),
		newCompletionCommand(root),
	)
	return root
}

// withStore is the RunE of a command that works on the store.
func (a *app) withStore(run func(cmd *cobra.Command, s *store.Store, args []string) error) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		s, err := a.openStore()
		if err != nil {
			return err
		}
		return run(cmd, s, args)
	}
}

func (a *app) newLoginCommand() *cobra.Command {
	var opts codex.LoginOptions
	cmd := &cobra.Command{
		Use:               "login [PROFILE_NAME]",
		Short:             "Log in and save a named profile",
		Args:              cobra.MaximumNArgs(1),
		ValidArgsFunction: cobra.NoFileCompletions,
		RunE: a.withStore(func(cmd *cobra.Command, s *store.Store, args []string) error {
			// Look for codex before anything else is done, so a missing
			// install fails before any state is created.
			c, err := a.findCodex()
			if err != nil {
				return err
			}
			name, err := a.newNameArg(cmd, s, args, 0, "PROFILE_NAME", tui.InputOptions{
				Title:       "Name for this login",
				Description: []string{"Letters, digits, '.', '_' and '-'. Reusing a name logs that profile in again."},
				Placeholder: "work",
			}, true)
			if err != nil {
				return err
			}
			secret := ""
			if a.tui && !opts.DeviceAuth && !opts.APIKey && !opts.AccessToken {
				if opts, secret, err = a.chooseLoginMethod(cmd); err != nil {
					return err
				}
			}
			result, err := a.runLogin(cmd, s, c, name, opts, secret)
			if err != nil {
				return err
			}
			a.finish(cmd, s, result, say("Saved and activated profile %s", profileName(result.Profile)).withHint(restartClients))
			return nil
		}),
	}
	cmd.Flags().BoolVar(&opts.DeviceAuth, "device-auth", false, "use Codex device authentication")
	cmd.Flags().BoolVar(&opts.APIKey, "with-api-key", false, "read an API key from stdin")
	cmd.Flags().BoolVar(&opts.AccessToken, "with-access-token", false, "read an access token from stdin")
	cmd.MarkFlagsMutuallyExclusive("device-auth", "with-api-key", "with-access-token")
	return cmd
}

func (a *app) newImportCommand() *cobra.Command {
	return &cobra.Command{
		Use:               "import [PROFILE_NAME]",
		Short:             "Save the active auth.json as a profile",
		Long:              "Save the credentials Codex is currently using as a named profile and select it.\nUse this for an account that was logged in with plain 'codex login'.",
		Args:              cobra.MaximumNArgs(1),
		ValidArgsFunction: cobra.NoFileCompletions,
		RunE: a.withStore(func(cmd *cobra.Command, s *store.Store, args []string) error {
			name, err := a.newNameArg(cmd, s, args, 0, "PROFILE_NAME", tui.InputOptions{
				Title:       "Name for the current Codex login",
				Placeholder: "personal",
			}, false)
			if err != nil {
				return err
			}
			result, err := s.Import(name)
			if err != nil {
				return err
			}
			a.finish(cmd, s, result, say("Imported the active login as profile %s", profileName(result.Profile)))
			return nil
		}),
	}
}

func (a *app) newUseCommand() *cobra.Command {
	return &cobra.Command{
		Use:               "use [PROFILE_NAME]",
		Short:             "Activate a saved profile",
		Args:              cobra.MaximumNArgs(1),
		ValidArgsFunction: a.completeProfiles,
		RunE: a.withStore(func(cmd *cobra.Command, s *store.Store, args []string) error {
			name, err := a.profileArg(cmd, s, args, "Switch to which profile?", false)
			if err != nil {
				return err
			}
			result, err := s.Use(name)
			if err != nil {
				return err
			}
			a.finish(cmd, s, result, say("Now using profile %s", profileName(result.Profile)).withHint(restartClients))
			return nil
		}),
	}
}

func (a *app) newListCommand() *cobra.Command {
	var verbose, asJSON bool
	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List saved profiles",
		Args:    cobra.NoArgs,
		RunE: a.withStore(func(cmd *cobra.Command, s *store.Store, _ []string) error {
			profiles, err := s.Profiles()
			if err != nil {
				return err
			}
			return a.renderProfiles(cmd, profiles, verbose, asJSON)
		}),
	}
	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "show account details")
	addJSONFlag(cmd, &asJSON)
	return cmd
}

func (a *app) newCurrentCommand() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "current",
		Short: "Print the selected profile",
		Args:  cobra.NoArgs,
		RunE: a.withStore(func(cmd *cobra.Command, s *store.Store, _ []string) error {
			current, matches, err := s.Current()
			if err != nil {
				return err
			}
			if current == "" {
				return errors.New("no profile has been selected")
			}
			return a.renderCurrent(cmd, s, current, matches, s.Daemon(), asJSON)
		}),
	}
	addJSONFlag(cmd, &asJSON)
	return cmd
}

func (a *app) newShowCommand() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:               "show [PROFILE_NAME]",
		Short:             "Show a profile's account details",
		Args:              cobra.MaximumNArgs(1),
		ValidArgsFunction: a.completeProfiles,
		RunE: a.withStore(func(cmd *cobra.Command, s *store.Store, args []string) error {
			name, err := a.profileArg(cmd, s, args, "Show which profile?", true)
			if err != nil {
				return err
			}
			p, err := s.Show(name)
			if err != nil {
				return err
			}
			return a.renderProfile(cmd, p, asJSON)
		}),
	}
	addJSONFlag(cmd, &asJSON)
	return cmd
}

func (a *app) newSyncCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "sync",
		Short: "Save refreshed credentials into the selected profile",
		Args:  cobra.NoArgs,
		RunE: a.withStore(func(cmd *cobra.Command, s *store.Store, _ []string) error {
			result, err := s.Sync()
			if err != nil {
				return err
			}
			a.finish(cmd, s, result, say("Saved the active credentials into profile %s", profileName(result.Profile)))
			return nil
		}),
	}
}

func (a *app) newRenameCommand() *cobra.Command {
	return &cobra.Command{
		Use:               "rename [OLD_NAME [NEW_NAME]]",
		Short:             "Rename a saved profile",
		Args:              cobra.MaximumNArgs(2),
		ValidArgsFunction: a.completeProfiles,
		RunE: a.withStore(func(cmd *cobra.Command, s *store.Store, args []string) error {
			oldName, err := a.profileArg(cmd, s, args, "Rename which profile?", true)
			if err != nil {
				return err
			}
			newName, err := a.newNameArg(cmd, s, args, 1, "NEW_NAME", tui.InputOptions{
				Title:       fmt.Sprintf("New name for %s", oldName),
				Placeholder: oldName,
			}, false)
			if err != nil {
				return err
			}
			result, err := s.Rename(oldName, newName)
			if err != nil {
				return err
			}
			a.finish(cmd, s, result, say("Renamed profile %s to %s", profileName(oldName), profileName(result.Profile)))
			return nil
		}),
	}
}

func (a *app) newRemoveCommand() *cobra.Command {
	return &cobra.Command{
		Use:               "remove [PROFILE_NAME...]",
		Aliases:           []string{"rm"},
		Short:             "Delete saved profiles",
		Args:              cobra.ArbitraryArgs,
		ValidArgsFunction: a.completeProfiles,
		RunE: a.withStore(func(cmd *cobra.Command, s *store.Store, args []string) error {
			names, err := a.profileArgs(cmd, s, args, "Remove which profiles?")
			if err != nil {
				return err
			}
			// Names typed on the command line are deliberate; ones checked
			// in a list get a second look.
			if len(args) == 0 {
				if err := confirmDanger(cmd, fmt.Sprintf("Remove %s?", countNoun(len(names), "profile")), "Remove",
					strings.Join(names, ", "),
					"Saved logins cannot be recovered. The active auth.json is left in place."); err != nil {
					return err
				}
			}
			authChanged := false
			for _, name := range names {
				result, err := s.Remove(name)
				if err != nil {
					return err
				}
				authChanged = authChanged || result.AuthChanged
				a.report(cmd, result, say("Removed profile %s", profileName(result.Profile)))
			}
			a.offerDaemonRestart(cmd, s, authChanged)
			return nil
		}),
	}
}

func (a *app) newLogoutCommand() *cobra.Command {
	return &cobra.Command{
		Use:               "logout [PROFILE_NAME]",
		Short:             "Log out of a profile's account and delete the profile",
		Long:              "Run 'codex logout' for the profile's account in an isolated directory, then delete the profile.\nUnlike 'remove', this ends the session itself.",
		Args:              cobra.MaximumNArgs(1),
		ValidArgsFunction: a.completeProfiles,
		RunE: a.withStore(func(cmd *cobra.Command, s *store.Store, args []string) error {
			c, err := a.findCodex()
			if err != nil {
				return err
			}
			name, err := a.profileArg(cmd, s, args, "Log out of which profile?", false)
			if err != nil {
				return err
			}
			if len(args) == 0 {
				if err := confirmDanger(cmd, fmt.Sprintf("Log out of %s?", name), "Log out",
					"This ends the account's session and deletes the profile."); err != nil {
					return err
				}
			}
			result, err := a.runLogout(cmd, s, c, name)
			if err != nil {
				return err
			}
			a.finish(cmd, s, result, say("Logged out and removed profile %s", profileName(result.Profile)))
			return nil
		}),
	}
}

func (a *app) newDoctorCommand() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Check configuration and profile state",
		Args:  cobra.NoArgs,
		RunE: a.withStore(func(cmd *cobra.Command, s *store.Store, _ []string) error {
			var checks []store.Check
			if err := a.busy(cmd, "Checking configuration and profiles", func() error {
				checks = s.Doctor()
				return nil
			}); err != nil {
				return err
			}
			return a.renderChecks(cmd, checks, asJSON)
		}),
	}
	addJSONFlag(cmd, &asJSON)
	return cmd
}

func newCompletionCommand(root *cobra.Command) *cobra.Command {
	return &cobra.Command{
		Use:       "completion [bash|zsh|fish|powershell]",
		Short:     "Generate shell completion",
		ValidArgs: []string{"bash", "zsh", "fish", "powershell"},
		Args:      cobra.MatchAll(cobra.ExactArgs(1), cobra.OnlyValidArgs),
		RunE: func(cmd *cobra.Command, args []string) error {
			switch args[0] {
			case "bash":
				return root.GenBashCompletion(cmd.OutOrStdout())
			case "zsh":
				return root.GenZshCompletion(cmd.OutOrStdout())
			case "fish":
				return root.GenFishCompletion(cmd.OutOrStdout(), true)
			case "powershell":
				return root.GenPowerShellCompletion(cmd.OutOrStdout())
			default:
				return fmt.Errorf("unsupported shell %q", args[0])
			}
		},
	}
}

// completeProfiles offers saved profile names for a command's first argument.
func (a *app) completeProfiles(_ *cobra.Command, args []string, _ string) ([]string, cobra.ShellCompDirective) {
	if len(args) != 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	s, err := a.openStore()
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	names, _, err := s.List()
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	return names, cobra.ShellCompDirectiveNoFileComp
}

func addJSONFlag(cmd *cobra.Command, v *bool) {
	cmd.Flags().BoolVar(v, "json", false, "print machine-readable JSON")
}
