package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// ConfirmOptions describes a yes/no question.
type ConfirmOptions struct {
	Title       string
	Description []string // lines shown under the title
	Affirmative string   // label of the yes button; "Yes" when empty
	Negative    string   // label of the no button; "No" when empty
	Default     bool     // which button is focused first
	Danger      bool     // draw the yes button in red
}

type confirmModel struct {
	opts      ConfirmOptions
	theme     *Theme
	yes       bool
	done      bool
	cancelled bool
}

func newConfirm(t *Theme, opts ConfirmOptions) *confirmModel {
	if opts.Affirmative == "" {
		opts.Affirmative = "Yes"
	}
	if opts.Negative == "" {
		opts.Negative = "No"
	}
	return &confirmModel{opts: opts, theme: t, yes: opts.Default}
}

func (m *confirmModel) Init() tea.Cmd { return nil }

func (m *confirmModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	switch key.String() {
	case "left", "right", "h", "l", "tab", "shift+tab":
		m.yes = !m.yes
	case "y", "Y":
		m.yes, m.done = true, true
	case "n", "N":
		m.yes, m.done = false, true
	case "enter", " ":
		m.done = true
	case "esc", "ctrl+c", "q":
		m.yes, m.done, m.cancelled = false, true, true
	}
	if m.done {
		return m, tea.Quit
	}
	return m, nil
}

func (m *confirmModel) View() string {
	t := m.theme
	if m.done {
		if m.cancelled {
			return t.abandoned(m.opts.Title)
		}
		answer := m.opts.Negative
		if m.yes {
			answer = m.opts.Affirmative
		}
		return t.answered(m.opts.Title, answer)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n", t.question(m.opts.Title))
	for _, line := range m.opts.Description {
		fmt.Fprintf(&b, "  %s\n", t.Muted.Render(line))
	}
	yes, no := t.Button, t.ButtonOn
	if m.yes {
		yes, no = t.ButtonOn, t.Button
		if m.opts.Danger {
			yes = t.Danger
		}
	}
	fmt.Fprintf(&b, "\n  %s %s\n\n", yes.Render(m.opts.Affirmative), no.Render(m.opts.Negative))
	fmt.Fprintf(&b, "  %s\n", t.help("←/→", "switch", "y/n", "choose", "enter", "confirm", "esc", "cancel"))
	return b.String()
}

// Confirm asks a yes/no question. Esc and Ctrl-C return ErrCancelled.
func Confirm(env Env, opts ConfirmOptions) (bool, error) {
	m := newConfirm(env.theme(), opts)
	if _, err := run(env, m); err != nil {
		return false, err
	}
	if m.cancelled {
		return false, ErrCancelled
	}
	return m.yes, nil
}

// run starts an inline program, never in the alternate screen, so every
// finished prompt leaves a one-line record in the scrollback.
func run(env Env, m tea.Model) (tea.Model, error) {
	return tea.NewProgram(m, tea.WithInput(env.In), tea.WithOutput(env.Out)).Run()
}
