package cli

import (
	"errors"
	"fmt"

	"github.com/AbdelrhmanSaid/codexctl/internal/update"

	"github.com/spf13/cobra"
)

// findExecutable locates the running codexctl binary and how it was
// installed.
func findExecutable() (string, update.Method, error) {
	exe, err := update.Executable()
	if err != nil {
		return "", 0, fmt.Errorf("locate executable: %w", err)
	}
	return exe, update.DetectInstall(version != "dev", exe), nil
}

func (a *app) newUninstallCommand() *cobra.Command {
	var purge, keepBinary, yes bool
	cmd := &cobra.Command{
		Use:   "uninstall",
		Short: "Remove codexctl from this machine",
		Long: "Delete the codexctl executable. With --purge, also delete every saved profile and all\n" +
			"codexctl state. The Codex home is never touched: the active auth.json stays in place,\n" +
			"so Codex stays logged in with the currently selected account.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if keepBinary && !purge {
				return errors.New("--keep-binary without --purge has nothing to remove")
			}
			s, err := a.openStore()
			if err != nil {
				return err
			}
			exe := ""
			if !keepBinary {
				path, method, err := a.executable()
				if err != nil {
					return err
				}
				if method == update.MethodPackage {
					hint := ""
					if purge {
						hint = "; run 'codexctl uninstall --purge --keep-binary' first to delete saved profiles"
					}
					return fmt.Errorf("codexctl at %s was installed by a package manager; remove it with that package manager%s", path, hint)
				}
				exe = path
			}

			errOut := cmd.ErrOrStderr()
			fmt.Fprintln(errOut, "This will remove:")
			if exe != "" {
				fmt.Fprintf(errOut, "  the codexctl executable %s\n", exe)
			}
			if purge {
				fmt.Fprintf(errOut, "  every saved profile and all codexctl state in %s (saved logins cannot be recovered)\n", s.StateHome)
			}
			fmt.Fprintf(errOut, "Codex stays logged in with the active %s.\n", s.AuthPath())
			if !yes {
				if !a.interactive {
					return errors.New("refusing to uninstall without confirmation; pass --yes")
				}
				if !confirm(cmd, "Continue? [y/N] ") {
					fmt.Fprintln(errOut, "Nothing was removed.")
					return nil
				}
			}

			removeBinary := func() error {
				if exe == "" {
					return nil
				}
				if err := update.Remove(exe); err != nil {
					return fmt.Errorf("remove %s: %w", exe, err)
				}
				return nil
			}
			out := cmd.OutOrStdout()
			if purge {
				if err := s.Purge(removeBinary); err != nil {
					return err
				}
			} else if err := removeBinary(); err != nil {
				return err
			}
			if exe != "" {
				fmt.Fprintf(out, "Removed %s.\n", exe)
			}
			if purge {
				fmt.Fprintf(out, "Deleted saved profiles and codexctl state in %s.\n", s.StateHome)
			} else {
				fmt.Fprintf(out, "Kept saved profiles in %s; delete that directory to remove them.\n", s.StateHome)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&purge, "purge", false, "also delete every saved profile and all codexctl state")
	cmd.Flags().BoolVar(&keepBinary, "keep-binary", false, "with --purge, delete only the state and keep the executable")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "do not ask for confirmation")
	return cmd
}
