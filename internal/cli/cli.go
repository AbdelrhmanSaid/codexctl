package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sort"
	"strings"

	"codexctl/internal/store"

	"github.com/spf13/cobra"
)

const version = "0.1.0"

func Run(args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	root := newRootCommand(stdin, stdout, stderr)
	root.SetArgs(args)
	return root.Execute()
}

func newRootCommand(stdin io.Reader, stdout, stderr io.Writer) *cobra.Command {
	root := &cobra.Command{
		Use:           "codexctl",
		Short:         "Manage named Codex login profiles",
		Version:       version,
		SilenceErrors: true,
		SilenceUsage:  true,
	}
	root.SetIn(stdin)
	root.SetOut(stdout)
	root.SetErr(stderr)
	root.SetHelpCommand(&cobra.Command{Hidden: true})
	root.AddCommand(
		newLoginCommand(),
		newUseCommand(),
		newListCommand(),
		newCurrentCommand(),
		newDoctorCommand(),
		newCompletionCommand(root),
	)
	return root
}

func newLoginCommand() *cobra.Command {
	var deviceAuth, apiKey, accessToken bool
	cmd := &cobra.Command{
		Use:   "login PROFILE_NAME",
		Short: "Log in and save a named profile",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			methods := 0
			for _, enabled := range []bool{deviceAuth, apiKey, accessToken} {
				if enabled {
					methods++
				}
			}
			if methods > 1 {
				return errors.New("choose only one login method")
			}

			codex, err := exec.LookPath("codex")
			if err != nil {
				return errors.New("codex executable was not found in PATH")
			}
			loginArgs := []string{"login"}
			if deviceAuth {
				loginArgs = append(loginArgs, "--device-auth")
			}
			if apiKey {
				loginArgs = append(loginArgs, "--with-api-key")
			}
			if accessToken {
				loginArgs = append(loginArgs, "--with-access-token")
			}

			s, err := store.NewFromEnvironment()
			if err != nil {
				return err
			}
			warning, err := s.Login(args[0], func(home string) error {
				child := exec.Command(codex, loginArgs...)
				child.Env = replaceEnv(os.Environ(), "CODEX_HOME", home)
				child.Env = replaceEnv(child.Env, "CODEX_SQLITE_HOME", home)
				child.Stdin, child.Stdout, child.Stderr = cmd.InOrStdin(), cmd.OutOrStdout(), cmd.ErrOrStderr()
				return child.Run()
			})
			if err != nil {
				return err
			}
			printWarning(cmd, warning)
			fmt.Fprintf(cmd.OutOrStdout(), "Saved and activated profile %q. Restart running Codex clients to pick it up.\n", args[0])
			return nil
		},
	}
	cmd.Flags().BoolVar(&deviceAuth, "device-auth", false, "use Codex device authentication")
	cmd.Flags().BoolVar(&apiKey, "with-api-key", false, "read an API key from stdin")
	cmd.Flags().BoolVar(&accessToken, "with-access-token", false, "read an access token from stdin")
	return cmd
}

func newUseCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "use PROFILE_NAME",
		Short: "Activate a saved profile",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := store.NewFromEnvironment()
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

func newListCommand() *cobra.Command {
	return &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List saved profiles",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			s, err := store.NewFromEnvironment()
			if err != nil {
				return err
			}
			profiles, current, err := s.List()
			if err != nil {
				return err
			}
			sort.Strings(profiles)
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

func newCurrentCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "current",
		Short: "Print the selected profile",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			s, err := store.NewFromEnvironment()
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

func newDoctorCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Check configuration and profile state",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			s, err := store.NewFromEnvironment()
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

func replaceEnv(env []string, key, value string) []string {
	prefix := key + "="
	result := make([]string, 0, len(env)+1)
	for _, item := range env {
		if !strings.HasPrefix(item, prefix) {
			result = append(result, item)
		}
	}
	return append(result, prefix+value)
}
