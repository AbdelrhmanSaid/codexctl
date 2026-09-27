package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/AbdelrhmanSaid/codexctl/internal/store"
	"github.com/AbdelrhmanSaid/codexctl/internal/tui"

	"github.com/charmbracelet/x/term"
	"github.com/spf13/cobra"
)

// detectTerminals decides which streams are terminals. Setting
// CODEXCTL_NO_TUI, or TERM=dumb, keeps every command on plain text and
// line-based prompts.
func (a *app) detectTerminals(stdin io.Reader, stdout, stderr io.Writer) {
	a.interactive = isTerminal(stdin)
	allowed := styleAllowed()
	a.styledOut = allowed && isTerminal(stdout)
	a.styledErr = allowed && isTerminal(stderr)
	a.tui = a.interactive && a.styledErr
}

// styleAllowed reports whether the environment permits colors and the
// terminal UI at all.
func styleAllowed() bool {
	return os.Getenv("CODEXCTL_NO_TUI") == "" && os.Getenv("TERM") != "dumb"
}

// PrintError reports a command's error on w: a red line on a terminal, a
// "codexctl:" prefixed line otherwise.
func PrintError(w io.Writer, err error) {
	if styleAllowed() && isTerminal(w) {
		fmt.Fprint(w, tui.NewTheme(w).Failure(capitalize(err.Error())))
		return
	}
	fmt.Fprintf(w, "codexctl: %v\n", err)
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

// profileName and filePath are message arguments that a terminal and plain
// text show differently: a profile name is colored or quoted, and a path is
// shortened under the home directory or given whole.
type (
	profileName string
	filePath    string
)

// message is what a finished action reports. It is written once and drawn
// for a terminal or as the plain sentence scripts rely on.
type message struct {
	format string // a sentence without its full stop
	args   []any
	hint   string // advice that follows, as a sentence of its own
}

func say(format string, args ...any) message {
	return message{format: format, args: args}
}

func (m message) withHint(hint string) message {
	m.hint = hint
	return m
}

// text fills in the message, showing each argument with show.
func (m message) text(show func(any) any) string {
	args := make([]any, len(m.args))
	for i, arg := range m.args {
		args[i] = show(arg)
	}
	return fmt.Sprintf(m.format, args...)
}

// plain is the message as one line of plain text.
func (m message) plain() string {
	line := m.text(func(arg any) any {
		if name, ok := arg.(profileName); ok {
			return strconv.Quote(string(name))
		}
		return arg
	}) + "."
	if m.hint != "" {
		line += " " + m.hint
	}
	return line
}

// styled is the message's sentence for a terminal.
func (m message) styled(t *tui.Theme) string {
	return m.text(func(arg any) any {
		switch arg := arg.(type) {
		case profileName:
			return t.Name(string(arg))
		case filePath:
			return displayPath(string(arg))
		}
		return arg
	})
}

// success reports a finished action on stdout: a check mark and a hint line
// on a terminal, the plain sentence otherwise.
func (a *app) success(cmd *cobra.Command, m message) {
	out := cmd.OutOrStdout()
	if !a.styledOut {
		line := m.plain()
		fmt.Fprintln(out, line)
		a.record(tui.LevelOK, line)
		return
	}
	t := outTheme(cmd)
	line := m.styled(t)
	fmt.Fprint(out, t.Success(line))
	a.record(tui.LevelOK, line)
	if m.hint != "" {
		fmt.Fprint(out, t.Hint(m.hint))
		a.record(tui.LevelInfo, m.hint)
	}
}

// report says how a store operation went: its warnings, then the message.
func (a *app) report(cmd *cobra.Command, result store.Result, m message) {
	for _, warning := range result.Warnings {
		a.warn(cmd, warning)
	}
	a.success(cmd, m)
}

// finish reports a store operation and, if it changed the active
// auth.json, offers to restart a daemon that still uses the old one.
func (a *app) finish(cmd *cobra.Command, s *store.Store, result store.Result, m message) {
	a.report(cmd, result, m)
	a.offerDaemonRestart(cmd, s, result.AuthChanged)
}

// record keeps a line of what an action reported for the dashboard, which
// shows it once the action is over. Without the dashboard it does nothing.
func (a *app) record(level tui.Level, text string) {
	if a.dashboard {
		a.results = append(a.results, tui.Note{Text: text, Level: level})
	}
}

// warn prints a warning on stderr.
func (a *app) warn(cmd *cobra.Command, warning string) {
	if warning == "" {
		return
	}
	a.record(tui.LevelWarn, capitalize(warning))
	if a.styledErr {
		fmt.Fprint(cmd.ErrOrStderr(), errTheme(cmd).Warning(capitalize(warning)))
		return
	}
	fmt.Fprintf(cmd.ErrOrStderr(), "warning: %s\n", warning)
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

// countNoun renders "1 profile" or "3 profiles".
func countNoun(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

// capitalize upper-cases the first letter of a message that is shown on its
// own rather than after a "warning:" prefix.
func capitalize(message string) string {
	if message == "" {
		return message
	}
	r, size := utf8.DecodeRuneInString(message)
	return string(unicode.ToUpper(r)) + message[size:]
}

// busy runs work behind a spinner on a terminal, or directly otherwise.
func (a *app) busy(cmd *cobra.Command, title string, work func() error) error {
	if !a.tui {
		return work()
	}
	return tui.Spin(env(cmd), title, work)
}

// confirmDanger asks a yes/no question with a red yes button and returns
// ErrCancelled unless the user agrees.
func confirmDanger(cmd *cobra.Command, title, affirmative string, description ...string) error {
	ok, err := tui.Confirm(env(cmd), tui.ConfirmOptions{
		Title:       title,
		Description: description,
		Affirmative: affirmative,
		Negative:    "Cancel",
		Danger:      true,
	})
	if err != nil {
		return err
	}
	if !ok {
		return tui.ErrCancelled
	}
	return nil
}
