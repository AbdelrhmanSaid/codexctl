package cli

import (
	"errors"
	"fmt"
	"io"
	"runtime/debug"
	"strings"

	"github.com/AbdelrhmanSaid/codexctl/internal/codex"
	"github.com/AbdelrhmanSaid/codexctl/internal/store"

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
}

// app holds what commands need from outside the process. Both are resolved
// lazily so that help and completion never touch the environment.
type app struct {
	openStore func() (*store.Store, error)
	findCodex func() (codexCLI, error)
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
		a.newUseCommand(),
		a.newListCommand(),
		a.newCurrentCommand(),
		a.newDoctorCommand(),
		newCompletionCommand(root),
	)
	return root
}

func (a *app) newLoginCommand() *cobra.Command {
	var opts codex.LoginOptions
	cmd := &cobra.Command{
		Use:   "login PROFILE_NAME",
		Short: "Log in and save a named profile",
		Args:  cobra.ExactArgs(1),
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
			return nil
		},
	}
	cmd.Flags().BoolVar(&opts.DeviceAuth, "device-auth", false, "use Codex device authentication")
	cmd.Flags().BoolVar(&opts.APIKey, "with-api-key", false, "read an API key from stdin")
	cmd.Flags().BoolVar(&opts.AccessToken, "with-access-token", false, "read an access token from stdin")
	cmd.MarkFlagsMutuallyExclusive("device-auth", "with-api-key", "with-access-token")
	return cmd
}

func (a *app) newUseCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "use PROFILE_NAME",
		Short: "Activate a saved profile",
		Args:  cobra.ExactArgs(1),
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
			return nil
		},
	}
}

func (a *app) newListCommand() *cobra.Command {
	return &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List saved profiles",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			s, err := a.openStore()
			if err != nil {
				return err
			}
			profiles, current, err := s.List()
			if err != nil {
				return err
			}
			for _, profile := range profiles {
				marker := "  "
				if profile == current {
					marker = "* "
				}
				fmt.Fprintln(cmd.OutOrStdout(), marker+profile)
			}
			return nil
		},
	}
}

func (a *app) newCurrentCommand() *cobra.Command {
	return &cobra.Command{
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
			fmt.Fprintln(cmd.OutOrStdout(), current)
			if !matches {
				printWarning(cmd, "the active auth.json no longer matches the selected profile")
			}
			return nil
		},
	}
}

func (a *app) newDoctorCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Check configuration and profile state",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			s, err := a.openStore()
			if err != nil {
				return err
			}
			failed := false
			for _, check := range s.Doctor() {
				status := "ok"
				if check.Warning {
					status = "warn"
					failed = true
				}
				fmt.Fprintf(cmd.OutOrStdout(), "%-4s %s\n", status, check.Message)
			}
			if failed {
				return errors.New("doctor found one or more problems")
			}
			return nil
		},
	}
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

func printWarning(cmd *cobra.Command, warning string) {
	if warning != "" {
		fmt.Fprintf(cmd.ErrOrStderr(), "warning: %s\n", warning)
	}
}
