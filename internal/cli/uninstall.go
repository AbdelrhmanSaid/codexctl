package cli

import (
	"errors"
	"fmt"

	"github.com/AbdelrhmanSaid/codexctl/internal/store"
	"github.com/AbdelrhmanSaid/codexctl/internal/tui"
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
			path, method, err := a.executable()
			if err != nil && !keepBinary {
				return err
			}
			packaged := err == nil && method == update.MethodPackage
			if a.tui && !purge && !keepBinary && !yes {
				if purge, keepBinary, err = a.chooseUninstall(cmd, s, path, packaged); err != nil {
					return err
				}
			}
			exe := ""
			if !keepBinary {
				if packaged {
					hint := ""
					if purge {
						hint = "; run 'codexctl uninstall --purge --keep-binary' first to delete saved profiles"
					}
					return fmt.Errorf("codexctl at %s was installed by a package manager; remove it with that package manager%s", path, hint)
				}
				exe = path
			}

			if a.tui {
				if !yes {
					if ok, err := a.confirmUninstall(cmd, s, exe, purge); err != nil || !ok {
						return tui.ErrCancelled
					}
				}
			} else {
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
			if purge {
				if err := s.Purge(removeBinary); err != nil {
					return err
				}
			} else if err := removeBinary(); err != nil {
				return err
			}
			if exe != "" {
				a.success(cmd, "Removed "+displayPath(exe), "", fmt.Sprintf("Removed %s.", exe))
			}
			if purge {
				a.success(cmd, "Deleted saved profiles and codexctl state in "+displayPath(s.StateHome), "",
					fmt.Sprintf("Deleted saved profiles and codexctl state in %s.", s.StateHome))
			} else {
				a.success(cmd, "Kept saved profiles in "+displayPath(s.StateHome), "Delete that directory, or run 'codexctl uninstall --purge', to remove them.",
					fmt.Sprintf("Kept saved profiles in %s; delete that directory to remove them.", s.StateHome))
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&purge, "purge", false, "also delete every saved profile and all codexctl state")
	cmd.Flags().BoolVar(&keepBinary, "keep-binary", false, "with --purge, delete only the state and keep the executable")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "do not ask for confirmation")
	return cmd
}

// chooseUninstall asks which parts to remove with a checkbox list and
// returns the equivalent --purge and --keep-binary flags.
func (a *app) chooseUninstall(cmd *cobra.Command, s *store.Store, exe string, packaged bool) (purge, keepBinary bool, err error) {
	binary := tui.Item{Label: "codexctl executable", Detail: displayPath(exe), Checked: true}
	if exe == "" {
		binary.Disabled = "cannot locate the executable"
	} else if packaged {
		binary.Disabled = "installed by a package manager; remove it with that"
	}
	detail := displayPath(s.StateHome)
	if names, _, err := s.List(); err == nil {
		detail += fmt.Sprintf(" · %d saved profile(s)", len(names))
	}
	state := tui.Item{Label: "Saved profiles and state", Detail: detail}
	chosen, err := tui.MultiSelect(env(cmd), tui.MultiSelectOptions{
		Title: "What should be removed?",
		Items: []tui.Item{binary, state},
		Min:   1,
	})
	if err != nil {
		return false, false, err
	}
	removeBinary, removeState := false, false
	for _, i := range chosen {
		removeBinary = removeBinary || i == 0
		removeState = removeState || i == 1
	}
	return removeState, !removeBinary, nil
}

// confirmUninstall asks for a final yes before anything is removed.
func (a *app) confirmUninstall(cmd *cobra.Command, s *store.Store, exe string, purge bool) (bool, error) {
	lines := []string{}
	if exe != "" {
		lines = append(lines, "Removes "+displayPath(exe))
	}
	if purge {
		lines = append(lines, "Deletes every saved profile in "+displayPath(s.StateHome)+"; saved logins cannot be recovered")
	}
	lines = append(lines, "Codex stays logged in with the active "+displayPath(s.AuthPath()))
	return tui.Confirm(env(cmd), tui.ConfirmOptions{
		Title:       "Uninstall codexctl?",
		Description: lines,
		Affirmative: "Uninstall",
		Negative:    "Cancel",
		Danger:      true,
	})
}
