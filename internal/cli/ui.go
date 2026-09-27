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

// CODEXCTL_NO_TUI or TERM=dumb keeps every command on plain text.
func (a *app) detectTerminals(stdin io.Reader, stdout, stderr io.Writer) {
	a.interactive = isTerminal(stdin)

	allowed := styleAllowed()
	a.styledOut = allowed && isTerminal(stdout)
	a.styledErr = allowed && isTerminal(stderr)
	a.tui = a.interactive && a.styledErr
}

func styleAllowed() bool {
	return os.Getenv("CODEXCTL_NO_TUI") == "" && os.Getenv("TERM") != "dumb"
}

func PrintError(w io.Writer, err error) {
	if styleAllowed() && isTerminal(w) {
		fmt.Fprint(w, tui.NewTheme(w).Failure(capitalize(err.Error())))
		return
	}

	fmt.Fprintf(w, "codexctl: %v\n", err)
}

func isTerminal(stream any) bool {
	file, ok := stream.(*os.File)
	return ok && term.IsTerminal(file.Fd())
}

// Prompts draw on stderr, so stdout stays clean for results.
func env(cmd *cobra.Command) tui.Env {
	return tui.Env{In: cmd.InOrStdin(), Out: cmd.ErrOrStderr()}
}

func outTheme(cmd *cobra.Command) *tui.Theme { return tui.NewTheme(cmd.OutOrStdout()) }
func errTheme(cmd *cobra.Command) *tui.Theme { return tui.NewTheme(cmd.ErrOrStderr()) }

// Message arguments that a terminal and plain text show differently.
type (
	profileName string
	filePath    string
)

type message struct {
	format string // a sentence without its full stop
	args   []any
	hint   string
}

func say(format string, args ...any) message {
	return message{format: format, args: args}
}

func (m message) withHint(hint string) message {
	m.hint = hint
	return m
}

func (m message) text(show func(any) any) string {
	args := make([]any, len(m.args))
	for i, arg := range m.args {
		args[i] = show(arg)
	}

	return fmt.Sprintf(m.format, args...)
}

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

func (m message) styled(theme *tui.Theme) string {
	return m.text(func(arg any) any {
		switch arg := arg.(type) {
		case profileName:
			return theme.Name(string(arg))
		case filePath:
			return displayPath(string(arg))
		}

		return arg
	})
}

func (a *app) success(cmd *cobra.Command, m message) {
	out := cmd.OutOrStdout()
	if !a.styledOut {
		line := m.plain()
		fmt.Fprintln(out, line)
		a.record(tui.LevelOK, line)
		return
	}

	theme := outTheme(cmd)
	line := m.styled(theme)
	fmt.Fprint(out, theme.Success(line))
	a.record(tui.LevelOK, line)

	if m.hint != "" {
		fmt.Fprint(out, theme.Hint(m.hint))
		a.record(tui.LevelInfo, m.hint)
	}
}

func (a *app) report(cmd *cobra.Command, result store.Result, m message) {
	for _, warning := range result.Warnings {
		a.warn(cmd, warning)
	}

	a.success(cmd, m)
}

func (a *app) finish(cmd *cobra.Command, profileStore *store.Store, result store.Result, m message) {
	a.report(cmd, result, m)
	a.offerDaemonRestart(cmd, profileStore, result.AuthChanged)
}

func (a *app) record(level tui.Level, text string) {
	if a.dashboard {
		a.results = append(a.results, tui.Note{Text: text, Level: level})
	}
}

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

// For on-screen text only.
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

func countNoun(count int, noun string) string {
	if count == 1 {
		return "1 " + noun
	}

	return fmt.Sprintf("%d %ss", count, noun)
}

func capitalize(message string) string {
	if message == "" {
		return message
	}

	first, size := utf8.DecodeRuneInString(message)
	return string(unicode.ToUpper(first)) + message[size:]
}

func (a *app) busy(cmd *cobra.Command, title string, work func() error) error {
	if !a.tui {
		return work()
	}

	return tui.Spin(env(cmd), title, work)
}

func confirmDanger(cmd *cobra.Command, title, affirmative string, description ...string) error {
	confirmed, err := tui.Confirm(env(cmd), tui.ConfirmOptions{
		Title:       title,
		Description: description,
		Affirmative: affirmative,
		Negative:    "Cancel",
		Danger:      true,
	})
	if err != nil {
		return err
	}

	if !confirmed {
		return tui.ErrCancelled
	}

	return nil
}
