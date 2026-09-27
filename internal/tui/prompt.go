package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// sized is embedded by a model that lays itself out for the terminal's
// size. Both are zero until the size is known.
type sized struct {
	width, height int
}

func (s *sized) resize(width, height int) { s.width, s.height = width, height }

// outcome is embedded by a prompt to record how it ended.
type outcome struct {
	done, cancelled bool
}

func (o *outcome) cancel()            { o.done, o.cancelled = true, true }
func (o *outcome) wasCancelled() bool { return o.cancelled }

// fitted keeps a model told of the terminal's size.
type fitted struct {
	tea.Model
	sizer interface{ resize(width, height int) }
}

func (f fitted) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if size, ok := msg.(tea.WindowSizeMsg); ok {
		f.sizer.resize(size.Width, size.Height)
	}
	// Models change in place and return themselves.
	_, cmd := f.Model.Update(msg)
	return f, cmd
}

// run starts an inline program, never in the alternate screen, so every
// finished prompt leaves a one-line record in the scrollback. The model is
// told the terminal's size up front, so its first frame already fits.
func run(env Env, m tea.Model) error {
	if sizer, ok := m.(interface{ resize(width, height int) }); ok {
		sizer.resize(size(env.Out))
		m = fitted{Model: m, sizer: sizer}
	}
	_, err := tea.NewProgram(m, tea.WithInput(env.In), tea.WithOutput(env.Out)).Run()
	return err
}

// ask runs a prompt and returns ErrCancelled if the user left it with Esc or
// Ctrl-C.
func ask(env Env, m interface {
	tea.Model
	wasCancelled() bool
}) error {
	if err := run(env, m); err != nil {
		return err
	}
	if m.wasCancelled() {
		return ErrCancelled
	}
	return nil
}

// writeLines writes lines indented under a prompt's title.
func writeLines(b *strings.Builder, lines []string) {
	for _, line := range lines {
		fmt.Fprintf(b, "  %s\n", line)
	}
}

// describe renders the muted text under a prompt's title, wrapped to width
// columns.
func (t *Theme) describe(texts []string, width int) []string {
	var lines []string
	for _, text := range texts {
		for _, line := range wrap(text, width) {
			lines = append(lines, t.Muted.Render(line))
		}
	}
	return lines
}

// problem renders what is wrong with a prompt's answer, if anything.
func (t *Theme) problem(text string, width int) []string {
	if text == "" {
		return nil
	}
	return t.note(Note{Text: text, Level: LevelWarn}, width, false)
}
