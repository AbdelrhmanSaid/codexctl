package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/AbdelrhmanSaid/codexctl/internal/tui"

	"github.com/charmbracelet/x/term"
	"github.com/spf13/cobra"
)

// detectTerminals decides which streams are terminals. Setting
// CODEXCTL_NO_TUI, or TERM=dumb, keeps every command on plain text and
// line-based prompts.
func (a *app) detectTerminals(stdin io.Reader, stdout, stderr io.Writer) {
	a.interactive = isTerminal(stdin)
	allowed := os.Getenv("CODEXCTL_NO_TUI") == "" && os.Getenv("TERM") != "dumb"
	a.styledOut = allowed && isTerminal(stdout)
	a.styledErr = allowed && isTerminal(stderr)
	a.tui = a.interactive && a.styledErr
}

// isTerminal reports whether v is an interactive terminal, in which case a
// command may ask questions.
func isTerminal(v any) bool {
	f, ok := v.(*os.File)
	return ok && term.IsTerminal(f.Fd())
}

// env is where interactive prompts read keys and draw: stderr, so stdout
// stays clean for results.
func env(cmd *cobra.Command) tui.Env {
	return tui.Env{In: cmd.InOrStdin(), Out: cmd.ErrOrStderr()}
}

func outTheme(cmd *cobra.Command) *tui.Theme { return tui.NewTheme(cmd.OutOrStdout()) }
func errTheme(cmd *cobra.Command) *tui.Theme { return tui.NewTheme(cmd.ErrOrStderr()) }

// success reports a finished action on stdout: a check mark and a hint line
// on a terminal, the plain sentence otherwise.
func (a *app) success(cmd *cobra.Command, styled, hint, plain string) {
	if !a.styledOut {
		fmt.Fprintln(cmd.OutOrStdout(), plain)
		return
	}
	t := outTheme(cmd)
	fmt.Fprint(cmd.OutOrStdout(), t.Success(styled))
	if hint != "" {
		fmt.Fprint(cmd.OutOrStdout(), t.Hint(hint))
	}
}

// warn prints a warning on stderr.
func (a *app) warn(cmd *cobra.Command, warning string) {
	if warning == "" {
		return
	}
	if a.styledErr {
		fmt.Fprint(cmd.ErrOrStderr(), errTheme(cmd).Warning(warning))
		return
	}
	printWarning(cmd, warning)
}

// displayPath shortens a path under the home directory to ~/..., for
// on-screen text only.
func displayPath(path string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return path
	}
	if path == home {
		return "~"
	}
	if rest, ok := strings.CutPrefix(path, home+string(filepath.Separator)); ok {
		return "~" + string(filepath.Separator) + rest
	}
	return path
}
