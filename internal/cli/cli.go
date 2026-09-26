package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime/debug"
	"strings"
	"text/tabwriter"

	"github.com/AbdelrhmanSaid/codexctl/internal/codex"
	"github.com/AbdelrhmanSaid/codexctl/internal/store"
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
	// interactive is whether stdin is a terminal, so commands may ask
	// before restarting the daemon.
	interactive bool
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
	}
	if f, ok := stdin.(*os.File); ok {
		a.interactive = isTerminal(f)
	}
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
		newUpdateCommand(),
		newCompletionCommand(root),
	)
	return root
}

func (a *app) newLoginCommand() *cobra.Command {
	var opts codex.LoginOptions
	cmd := &cobra.Command{
		Use:               "login PROFILE_NAME",
		Short:             "Log in and save a named profile",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: cobra.NoFileCompletions,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Look for codex first so a missing install fails before any
			// state is created.
			c, err := a.findCodex()
			if err != nil {
				return err
			}
			s, err := a.openStore()
			if err != nil {
				return err
			}
			stdio := codex.Stdio{In: cmd.InOrStdin(), Out: cmd.OutOrStdout(), Err: cmd.ErrOrStderr()}
			warning, err := s.Login(args[0], func(home string) error {
				return c.Login(home, opts, stdio)
			})
			if err != nil {
				return err
			}
			printWarning(cmd, warning)
			fmt.Fprintf(cmd.OutOrStdout(), "Saved and activated profile %q. Restart running Codex clients to pick it up.\n", args[0])
			a.offerDaemonRestart(cmd, s)
			return nil
		},
	}
	cmd.Flags().BoolVar(&opts.DeviceAuth, "device-auth", false, "use Codex device authentication")
	cmd.Flags().BoolVar(&opts.APIKey, "with-api-key", false, "read an API key from stdin")
	cmd.Flags().BoolVar(&opts.AccessToken, "with-access-token", false, "read an access token from stdin")
	cmd.MarkFlagsMutuallyExclusive("device-auth", "with-api-key", "with-access-token")
	return cmd
}

func (a *app) newImportCommand() *cobra.Command {
	return &cobra.Command{
		Use:               "import PROFILE_NAME",
		Short:             "Save the active auth.json as a profile",
		Long:              "Save the credentials Codex is currently using as a named profile and select it.\nUse this for an account that was logged in with plain 'codex login'.",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: cobra.NoFileCompletions,
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := a.openStore()
			if err != nil {
				return err
			}
			if err := s.Import(args[0]); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Imported the active login as profile %q.\n", args[0])
			a.offerDaemonRestart(cmd, s)
			return nil
		},
	}
}

func (a *app) newUseCommand() *cobra.Command {
	return &cobra.Command{
		Use:               "use PROFILE_NAME",
		Short:             "Activate a saved profile",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: a.completeProfiles,
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := a.openStore()
			if err != nil {
				return err
			}
			warning, err := s.Use(args[0])
			if err != nil {
				return err
			}
			printWarning(cmd, warning)
			fmt.Fprintf(cmd.OutOrStdout(), "Now using profile %q. Restart running Codex clients to pick it up.\n", args[0])
			a.offerDaemonRestart(cmd, s)
			return nil
		},
	}
}

func (a *app) newListCommand() *cobra.Command {
	var verbose, asJSON bool
	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List saved profiles",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			s, err := a.openStore()
			if err != nil {
				return err
			}
			profiles, err := s.Profiles()
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if asJSON {
				return writeJSON(out, profiles)
			}
			if !verbose {
				for _, p := range profiles {
					fmt.Fprintln(out, marker(p.Selected)+p.Name)
				}
				return nil
			}
			w := tabwriter.NewWriter(out, 2, 0, 2, ' ', 0)
			fmt.Fprintln(w, "  NAME\tAUTH\tEMAIL\tPLAN\tLAST REFRESH")
			for _, p := range profiles {
				auth := p.AuthMode
				if !p.Valid {
					auth = "invalid"
				}
				fmt.Fprintf(w, "%s%s\t%s\t%s\t%s\t%s\n", marker(p.Selected), p.Name, auth, p.Email, p.Plan, p.LastRefresh)
			}
			return w.Flush()
		},
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
		RunE: func(cmd *cobra.Command, _ []string) error {
			s, err := a.openStore()
			if err != nil {
				return err
			}
			current, matches, err := s.Current()
			if err != nil {
				return err
			}
			if current == "" {
				return errors.New("no profile has been selected")
			}
			daemonState := s.Daemon()
			if asJSON {
				return writeJSON(cmd.OutOrStdout(), struct {
					Name        string `json:"name"`
					Matches     bool   `json:"matches"`
					DaemonStale bool   `json:"daemon_stale"`
				}{current, matches, daemonState.Stale})
			}
			fmt.Fprintln(cmd.OutOrStdout(), current)
			if !matches {
				printWarning(cmd, "the active auth.json no longer matches the selected profile")
			}
			if daemonState.Stale {
				printWarning(cmd, fmt.Sprintf("the Codex app-server daemon (pid %d) started before this profile was selected and still uses the previous credentials; run 'codexctl restart-daemon' to reload it (this interrupts active Codex sessions)", daemonState.PID))
			}
			return nil
		},
	}
	addJSONFlag(cmd, &asJSON)
	return cmd
}

func (a *app) newShowCommand() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:               "show PROFILE_NAME",
		Short:             "Show a profile's account details",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: a.completeProfiles,
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := a.openStore()
			if err != nil {
				return err
			}
			p, err := s.Show(args[0])
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if asJSON {
				return writeJSON(out, p)
			}
			w := tabwriter.NewWriter(out, 0, 0, 1, ' ', 0)
			fmt.Fprintf(w, "Name:\t%s\n", p.Name)
			fmt.Fprintf(w, "Selected:\t%s\n", yesNo(p.Selected))
			fmt.Fprintf(w, "Auth mode:\t%s\n", orDash(p.AuthMode))
			fmt.Fprintf(w, "Account ID:\t%s\n", orDash(p.AccountID))
			fmt.Fprintf(w, "Email:\t%s\n", orDash(p.Email))
			fmt.Fprintf(w, "Plan:\t%s\n", orDash(p.Plan))
			fmt.Fprintf(w, "Last refresh:\t%s\n", orDash(p.LastRefresh))
			return w.Flush()
		},
	}
	addJSONFlag(cmd, &asJSON)
	return cmd
}

func (a *app) newSyncCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "sync",
		Short: "Save refreshed credentials into the selected profile",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			s, err := a.openStore()
			if err != nil {
				return err
			}
			name, err := s.Sync()
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Saved the active credentials into profile %q.\n", name)
			a.offerDaemonRestart(cmd, s)
			return nil
		},
	}
}

func (a *app) newRenameCommand() *cobra.Command {
	return &cobra.Command{
		Use:               "rename OLD_NAME NEW_NAME",
		Short:             "Rename a saved profile",
		Args:              cobra.ExactArgs(2),
		ValidArgsFunction: a.completeProfiles,
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := a.openStore()
			if err != nil {
				return err
			}
			warning, err := s.Rename(args[0], args[1])
			if err != nil {
				return err
			}
			printWarning(cmd, warning)
			fmt.Fprintf(cmd.OutOrStdout(), "Renamed profile %q to %q.\n", args[0], args[1])
			a.offerDaemonRestart(cmd, s)
			return nil
		},
	}
}

func (a *app) newRemoveCommand() *cobra.Command {
	return &cobra.Command{
		Use:               "remove PROFILE_NAME",
		Aliases:           []string{"rm"},
		Short:             "Delete a saved profile",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: a.completeProfiles,
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := a.openStore()
			if err != nil {
				return err
			}
			warning, err := s.Remove(args[0])
			if err != nil {
				return err
			}
			printWarning(cmd, warning)
			fmt.Fprintf(cmd.OutOrStdout(), "Removed profile %q.\n", args[0])
			a.offerDaemonRestart(cmd, s)
			return nil
		},
	}
}

func (a *app) newLogoutCommand() *cobra.Command {
	return &cobra.Command{
		Use:               "logout PROFILE_NAME",
		Short:             "Log out of a profile's account and delete the profile",
		Long:              "Run 'codex logout' for the profile's account in an isolated directory, then delete the profile.\nUnlike 'remove', this ends the session itself.",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: a.completeProfiles,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := a.findCodex()
			if err != nil {
				return err
			}
			s, err := a.openStore()
			if err != nil {
				return err
			}
			stdio := codex.Stdio{In: cmd.InOrStdin(), Out: cmd.OutOrStdout(), Err: cmd.ErrOrStderr()}
			warning, err := s.Logout(args[0], func(home string) error {
				return c.Logout(home, stdio)
			})
			if err != nil {
				return err
			}
			printWarning(cmd, warning)
			fmt.Fprintf(cmd.OutOrStdout(), "Logged out and removed profile %q.\n", args[0])
			a.offerDaemonRestart(cmd, s)
			return nil
		},
	}
}

func (a *app) newDoctorCommand() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Check configuration and profile state",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			s, err := a.openStore()
			if err != nil {
				return err
			}
			type result struct {
				Message string `json:"message"`
				OK      bool   `json:"ok"`
			}
			failed := false
			results := []result{}
			for _, check := range s.Doctor() {
				if check.Warning {
					failed = true
				}
				results = append(results, result{check.Message, !check.Warning})
			}
			if asJSON {
				if err := writeJSON(cmd.OutOrStdout(), results); err != nil {
					return err
				}
			} else {
				for _, r := range results {
					status := "ok"
					if !r.OK {
						status = "warn"
					}
					fmt.Fprintf(cmd.OutOrStdout(), "%-4s %s\n", status, r.Message)
				}
			}
			if failed {
				return errors.New("doctor found one or more problems")
			}
			return nil
		},
	}
	addJSONFlag(cmd, &asJSON)
	return cmd
}

func newUpdateCommand() *cobra.Command {
	var check, force bool
	var target string
	cmd := &cobra.Command{
		Use:   "update",
		Short: "Update codexctl to the latest release",
		Long: "Download the latest GitHub release, verify its Ed25519 signature and checksum, and replace this executable.\n" +
			"Installs made with a package manager or 'go install' are told to update the same way they were installed.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			current := buildVersion()
			releaseBuild := version != "dev"
			exe, err := update.Executable()
			if err != nil {
				return fmt.Errorf("locate executable: %w", err)
			}
			client, err := update.NewClient(current)
			if err != nil {
				return err
			}
			ctx := cmd.Context()
			var release *update.Release
			if target == "" {
				release, err = client.Latest(ctx)
			} else {
				release, err = client.Version(ctx, target)
			}
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			cmp := update.CompareVersions(release.Version, current)
			if current == "dev" {
				cmp = 1
			}
			if check {
				switch {
				case current == "dev":
					fmt.Fprintf(out, "The latest release is codexctl %s. This is a development build, so it cannot be compared.\n", release.Version)
				case cmp > 0:
					fmt.Fprintf(out, "codexctl %s is available (installed %s). Run 'codexctl update' to install it.\n", release.Version, current)
				case cmp < 0:
					fmt.Fprintf(out, "codexctl %s is installed and is newer than release %s.\n", current, release.Version)
				default:
					fmt.Fprintf(out, "codexctl %s is up to date.\n", current)
				}
				return nil
			}
			if !force {
				switch update.DetectInstall(releaseBuild, exe) {
				case update.MethodGoInstall:
					return fmt.Errorf("codexctl was installed with 'go install'; run 'go install github.com/%s@latest' instead, or pass --force to replace %s", update.Repo, exe)
				case update.MethodPackage:
					return fmt.Errorf("codexctl at %s was installed by a package manager; update it with that package manager, or pass --force to overwrite it", exe)
				case update.MethodDev:
					return fmt.Errorf("this is a development build; install a release from https://github.com/%s/releases, or pass --force to replace %s", update.Repo, exe)
				}
				if cmp == 0 {
					fmt.Fprintf(out, "codexctl %s is already installed.\n", current)
					return nil
				}
				if cmp < 0 {
					if target == "" {
						fmt.Fprintf(out, "codexctl %s is installed and is newer than release %s. Pass --to %s --force to downgrade.\n", current, release.Version, release.Version)
						return nil
					}
					return fmt.Errorf("codexctl %s is newer than %s; pass --force to downgrade", current, release.Version)
				}
			}
			archive, err := client.Download(ctx, release)
			if err != nil {
				return err
			}
			binary, err := update.ExtractBinary(archive, update.AssetName(release.Version))
			if err != nil {
				return err
			}
			if err := update.Apply(exe, binary); err != nil {
				return err
			}
			fmt.Fprintf(out, "Updated codexctl from %s to %s at %s.\n", current, release.Version, exe)
			return nil
		},
	}
	cmd.Flags().BoolVar(&check, "check", false, "report whether an update is available without installing it")
	cmd.Flags().StringVar(&target, "to", "", "install this version instead of the latest release")
	cmd.Flags().BoolVar(&force, "force", false, "replace the executable even for package, go install, or development builds, or to downgrade")
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

func printWarning(cmd *cobra.Command, warning string) {
	if warning != "" {
		fmt.Fprintf(cmd.ErrOrStderr(), "warning: %s\n", warning)
	}
}
