package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// Zero until the size is known.
type sized struct {
	width, height int
}

func (s *sized) resize(width, height int) { s.width, s.height = width, height }

type outcome struct {
	done, cancelled bool
}

func (o *outcome) cancel()            { o.done, o.cancelled = true, true }
func (o *outcome) wasCancelled() bool { return o.cancelled }

type fitted struct {
	tea.Model
	sizer interface{ resize(width, height int) }
}

func (f fitted) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if sizeMsg, ok := msg.(tea.WindowSizeMsg); ok {
		f.sizer.resize(sizeMsg.Width, sizeMsg.Height)
	}

	// Models change in place and return themselves.
	_, cmd := f.Model.Update(msg)
	return f, cmd
}

// Inline, never the alternate screen, so a finished prompt stays in the
// scrollback.
func run(env Env, model tea.Model) error {
	if sizer, ok := model.(interface{ resize(width, height int) }); ok {
		sizer.resize(size(env.Out))
		model = fitted{Model: model, sizer: sizer}
	}

	_, err := tea.NewProgram(model, tea.WithInput(env.In), tea.WithOutput(env.Out)).Run()

	return err
}

func ask(env Env, model interface {
	tea.Model
	wasCancelled() bool
}) error {
	if err := run(env, model); err != nil {
		return err
	}

	if model.wasCancelled() {
		return ErrCancelled
	}

	return nil
}

func writeLines(b *strings.Builder, lines []string) {
	for _, line := range lines {
		fmt.Fprintf(b, "  %s\n", line)
	}
}

func (t *Theme) describe(texts []string, width int) []string {
	var lines []string
	for _, text := range texts {
		for _, line := range wrap(text, width) {
			lines = append(lines, t.Muted.Render(line))
		}
	}

	return lines
}

func (t *Theme) problem(text string, width int) []string {
	if text == "" {
		return nil
	}

	return t.note(Note{Text: text, Level: LevelWarn}, width, false)
}
