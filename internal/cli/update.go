package cli

import (
	"fmt"

	"github.com/AbdelrhmanSaid/codexctl/internal/tui"
	"github.com/AbdelrhmanSaid/codexctl/internal/update"

	"github.com/spf13/cobra"
)

// steps runs steps behind spinners on a terminal, or one after another
// without output otherwise.
func (a *app) steps(cmd *cobra.Command, steps ...tui.Step) error {
	if a.tui {
		return tui.RunSteps(env(cmd), steps...)
	}
	for _, step := range steps {
		if err := step.Run(&tui.Reporter{}); err != nil {
			return err
		}
	}
	return nil
}

func (a *app) newUpdateCommand() *cobra.Command {
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
			var release *update.Release
			lookup := "Checking for the latest release"
			if target != "" {
				lookup = "Looking up codexctl " + target
			}
			if err := a.steps(cmd, tui.Step{Title: lookup, Run: func(r *tui.Reporter) error {
				var err error
				if target == "" {
					release, err = client.Latest(r.Context())
				} else {
					release, err = client.Version(r.Context(), target)
				}
				if err == nil {
					r.Result(fmt.Sprintf("Found codexctl %s with a valid signature", release.Version))
				}
				return err
			}}); err != nil {
				return err
			}

			cmp := update.CompareVersions(release.Version, current)
			if current == "dev" {
				cmp = 1
			}
			if check {
				a.reportUpdateCheck(cmd, current, release.Version, cmp)
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
					a.success(cmd, "codexctl "+current+" is already installed", "", fmt.Sprintf("codexctl %s is already installed.", current))
					return nil
				}
				if cmp < 0 {
					if target == "" {
						a.success(cmd, fmt.Sprintf("codexctl %s is installed and is newer than release %s", current, release.Version),
							fmt.Sprintf("Pass --to %s --force to downgrade.", release.Version),
							fmt.Sprintf("codexctl %s is installed and is newer than release %s. Pass --to %s --force to downgrade.", current, release.Version, release.Version))
						return nil
					}
					return fmt.Errorf("codexctl %s is newer than %s; pass --force to downgrade", current, release.Version)
				}
			}

			asset := update.AssetName(release.Version)
			var archive, binary []byte
			err = a.steps(cmd,
				tui.Step{Title: "Downloading " + asset, Run: func(r *tui.Reporter) error {
					// Report whole percents only, so a fast download does not
					// flood the screen with redraws.
					shown := -1
					client.Progress = func(done, total int64) {
						if percent := int(done * 100 / max(total, 1)); total > 0 && percent != shown {
							shown = percent
							r.Progress(float64(done) / float64(total))
						}
					}
					defer func() { client.Progress = nil }()
					var err error
					archive, err = client.Download(r.Context(), release)
					if err == nil {
						r.Result(fmt.Sprintf("Downloaded %s (%s) and verified its checksum", asset, byteSize(len(archive))))
					}
					return err
				}},
				tui.Step{Title: "Extracting the executable", Run: func(*tui.Reporter) error {
					var err error
					binary, err = update.ExtractBinary(archive, asset)
					return err
				}},
				tui.Step{Title: "Installing to " + displayPath(exe), Run: func(*tui.Reporter) error {
					return update.Apply(exe, binary)
				}},
			)
			if err != nil {
				return err
			}
			a.success(cmd, fmt.Sprintf("Updated codexctl from %s to %s", current, release.Version), "",
				fmt.Sprintf("Updated codexctl from %s to %s at %s.", current, release.Version, exe))
			return nil
		},
	}
	cmd.Flags().BoolVar(&check, "check", false, "report whether an update is available without installing it")
	cmd.Flags().StringVar(&target, "to", "", "install this version instead of the latest release")
	cmd.Flags().BoolVar(&force, "force", false, "replace the executable even for package, go install, or development builds, or to downgrade")
	return cmd
}

// reportUpdateCheck prints the result of update --check.
func (a *app) reportUpdateCheck(cmd *cobra.Command, current, latest string, cmp int) {
	var styled, hint, plain string
	switch {
	case current == "dev":
		styled = "The latest release is codexctl " + latest
		hint = "This is a development build, so it cannot be compared."
		plain = fmt.Sprintf("The latest release is codexctl %s. This is a development build, so it cannot be compared.", latest)
	case cmp > 0:
		styled = fmt.Sprintf("codexctl %s is available (installed %s)", latest, current)
		hint = "Run 'codexctl update' to install it."
		plain = fmt.Sprintf("codexctl %s is available (installed %s). Run 'codexctl update' to install it.", latest, current)
	case cmp < 0:
		styled = fmt.Sprintf("codexctl %s is installed and is newer than release %s", current, latest)
		plain = styled + "."
	default:
		styled = fmt.Sprintf("codexctl %s is up to date", current)
		plain = styled + "."
	}
	a.success(cmd, styled, hint, plain)
}

// byteSize renders a size such as "4.2 MB".
func byteSize(n int) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	value, suffix := float64(n)/unit, "KB"
	for _, next := range []string{"MB", "GB"} {
		if value < unit {
			break
		}
		value, suffix = value/unit, next
	}
	return fmt.Sprintf("%.1f %s", value, suffix)
}
