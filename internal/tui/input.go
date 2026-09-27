package tui

import (
	"errors"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

// InputOptions describes a one-line text question.
type InputOptions struct {
	Title       string
	Description []string
	Placeholder string
	Value       string
	Secret      bool               // mask what is typed, for API keys
	Validate    func(string) error // checked on every key; enter needs nil
}

type inputModel struct {
	opts      InputOptions
	theme     *Theme
	input     textinput.Model
	width     int
	problem   string
	done      bool
	cancelled bool
}

func newInput(t *Theme, opts InputOptions) *inputModel {
	in := textinput.New()
	in.Prompt = t.Cursor.Render(glyphCursor) + " "
	in.Placeholder = opts.Placeholder
	in.PlaceholderStyle = t.Muted
	in.TextStyle = t.Accent
	in.Cursor.Style = t.Accent
	in.SetValue(opts.Value)
	if opts.Secret {
		in.EchoMode = textinput.EchoPassword
		in.EchoCharacter = '•'
	}
	in.Focus()
	return &inputModel{opts: opts, theme: t, input: in}
}

func (m *inputModel) Init() tea.Cmd { return textinput.Blink }

func (m *inputModel) resize(width, _ int) { m.width = width }

func (m *inputModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if size, ok := msg.(tea.WindowSizeMsg); ok {
		m.resize(size.Width, size.Height)
		return m, nil
	}
	if key, ok := msg.(tea.KeyMsg); ok {
		switch key.String() {
		case "ctrl+c", "esc":
			m.done, m.cancelled = true, true
			return m, tea.Quit
		case "enter":
			value := strings.TrimSpace(m.input.Value())
			if err := m.check(value); err != nil {
				m.problem = err.Error()
				return m, nil
			}
			m.input.SetValue(value)
			m.done = true
			return m, tea.Quit
		}
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	m.problem = ""
	if value := strings.TrimSpace(m.input.Value()); value != "" {
		if err := m.check(value); err != nil {
			m.problem = err.Error()
		}
	}
	return m, cmd
}

func (m *inputModel) check(value string) error {
	if value == "" {
		return errEmpty
	}
	if m.opts.Validate != nil {
		return m.opts.Validate(value)
	}
	return nil
}

var errEmpty = errors.New("a value is required")

func (m *inputModel) View() string {
	t := m.theme
	if m.done {
		if m.cancelled {
			return t.abandoned(m.opts.Title)
		}
		answer := m.input.Value()
		if m.opts.Secret {
			answer = strings.Repeat("•", min(len([]rune(answer)), 12))
		}
		return t.answered(m.opts.Title, answer)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n", t.question(m.opts.Title))
	for _, text := range m.opts.Description {
		for _, line := range wrap(text, m.width-2) {
			fmt.Fprintf(&b, "  %s\n", t.Muted.Render(line))
		}
	}
	fmt.Fprintf(&b, "  %s\n", m.input.View())
	if m.problem != "" {
		for _, line := range t.note(Note{Text: m.problem, Level: LevelWarn}, m.width-2, false) {
			fmt.Fprintf(&b, "  %s\n", line)
		}
	}
	b.WriteString("\n")
	for _, line := range t.help(m.width-2, "enter", "confirm", "esc", "cancel") {
		fmt.Fprintf(&b, "  %s\n", line)
	}
	return b.String()
}

// Input asks for one line of text and returns it without surrounding space.
func Input(env Env, opts InputOptions) (string, error) {
	m := newInput(env.theme(), opts)
	if _, err := run(env, m); err != nil {
		return "", err
	}
	if m.cancelled {
		return "", ErrCancelled
	}
	return m.input.Value(), nil
}
