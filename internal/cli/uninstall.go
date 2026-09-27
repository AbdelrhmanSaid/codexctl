package cli

import (
	"errors"
	"fmt"

	"github.com/AbdelrhmanSaid/codexctl/internal/store"
	"github.com/AbdelrhmanSaid/codexctl/internal/tui"
	"github.com/AbdelrhmanSaid/codexctl/internal/update"

	"github.com/spf13/cobra"
)

func findExecutable() (string, update.Method, error) {
	executablePath, err := update.Executable()
	if err != nil {
		return "", 0, fmt.Errorf("locate executable: %w", err)
	}

	return executablePath, update.DetectInstall(version != "dev", executablePath), nil
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

			profileStore, err := a.openStore()
			if err != nil {
				return err
			}

			installedPath, method, err := a.executable()
			if err != nil && !keepBinary {
				return err
			}

			packaged := err == nil && method == update.MethodPackage
			if a.tui && !purge && !keepBinary && !yes {
				if purge, keepBinary, err = a.chooseUninstall(cmd, profileStore, installedPath, packaged); err != nil {
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

					return fmt.Errorf("codexctl at %s was installed by a package manager; remove it with that package manager%s", installedPath, hint)
				}

				exe = installedPath
			}

			if a.tui {
				if !yes {
					if err := confirmUninstall(cmd, profileStore, exe, purge); err != nil {
						return err
					}
				}
			} else {
				errOut := cmd.ErrOrStderr()

				fmt.Fprintln(errOut, "This will remove:")
				if exe != "" {
					fmt.Fprintf(errOut, "  the codexctl executable %s\n", exe)
				}
				if purge {
					fmt.Fprintf(errOut, "  every saved profile and all codexctl state in %s (saved logins cannot be recovered)\n", profileStore.StateHome)
				}

				fmt.Fprintf(errOut, "Codex stays logged in with the active %s.\n", profileStore.AuthPath())

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
				if err := profileStore.Purge(removeBinary); err != nil {
					return err
				}
			} else if err := removeBinary(); err != nil {
				return err
			}

			if exe != "" {
				a.success(cmd, say("Removed %s", filePath(exe)))
			}

			if purge {
				a.success(cmd, say("Deleted saved profiles and codexctl state in %s", filePath(profileStore.StateHome)))
			} else {
				a.success(cmd, say("Kept saved profiles in %s", filePath(profileStore.StateHome)).
					withHint("Delete that directory, or run 'codexctl uninstall --purge', to remove them."))
			}

			return nil
		},
	}

	cmd.Flags().BoolVar(&purge, "purge", false, "also delete every saved profile and all codexctl state")
	cmd.Flags().BoolVar(&keepBinary, "keep-binary", false, "with --purge, delete only the state and keep the executable")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "do not ask for confirmation")

	return cmd
}

func (a *app) chooseUninstall(cmd *cobra.Command, profileStore *store.Store, exe string, packaged bool) (purge, keepBinary bool, err error) {
	binary := tui.Item{Label: "codexctl executable", Detail: displayPath(exe), Checked: true}
	if packaged {
		binary.Disabled = "installed by a package manager; remove it with that"
	}

	detail := displayPath(profileStore.StateHome)
	if names, _, listErr := profileStore.List(); listErr == nil {
		detail += " · " + countNoun(len(names), "saved profile")
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
	for _, index := range chosen {
		removeBinary = removeBinary || index == 0
		removeState = removeState || index == 1
	}

	return removeState, !removeBinary, nil
}

func confirmUninstall(cmd *cobra.Command, profileStore *store.Store, exe string, purge bool) error {
	lines := []string{}
	if exe != "" {
		lines = append(lines, "Removes "+displayPath(exe))
	}
	if purge {
		lines = append(lines, "Deletes every saved profile in "+displayPath(profileStore.StateHome)+"; saved logins cannot be recovered")
	}

	lines = append(lines, "Codex stays logged in with the active "+displayPath(profileStore.AuthPath()))
	return confirmDanger(cmd, "Uninstall codexctl?", "Uninstall", lines...)
}
