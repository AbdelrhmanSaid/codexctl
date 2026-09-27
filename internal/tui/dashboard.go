package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// DashboardRow is one profile on the dashboard.
type DashboardRow struct {
	Name    string
	Detail  string // account summary shown beside the name
	Extra   string // more detail shown under the list for the focused row
	Active  bool
	Invalid bool
}

// Note is a status line under the dashboard title.
type Note struct {
	Text string
	Warn bool
}

// Action is a key the dashboard answers to.
type Action struct {
	Key      string
	Label    string
	NeedsRow bool // only offered when a profile is focused
}

// DashboardOptions describes the dashboard.
type DashboardOptions struct {
	Title    string
	Subtitle string // muted text beside the title, such as a version
	Notes    []Note
	Rows     []DashboardRow
	Actions  []Action
	Cursor   int
}

// DashboardChoice is what the user asked for. Key is empty when they quit.
type DashboardChoice struct {
	Key string
	Row int
}

type dashboardModel struct {
	opts   DashboardOptions
	theme  *Theme
	cursor int
	choice DashboardChoice
	done   bool
}

func (m *dashboardModel) Init() tea.Cmd { return nil }

func (m *dashboardModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	switch k := key.String(); k {
	case "ctrl+c", "esc", "q":
		m.done = true
	case "up", "k":
		m.cursor = max(0, m.cursor-1)
	case "down", "j":
		m.cursor = max(0, min(len(m.opts.Rows)-1, m.cursor+1))
	default:
		if k == "enter" && len(m.opts.Actions) > 0 && len(m.opts.Rows) > 0 {
			k = m.opts.Actions[0].Key
		}
		for _, action := range m.opts.Actions {
			if action.Key == k && (!action.NeedsRow || len(m.opts.Rows) > 0) {
				m.choice = DashboardChoice{Key: k, Row: m.cursor}
				m.done = true
			}
		}
	}
	if m.done {
		return m, tea.Quit
	}
	return m, nil
}

func (m *dashboardModel) View() string {
	// The dashboard clears itself on exit so the scrollback only holds what
	// the chosen action prints.
	if m.done {
		return ""
	}
	t := m.theme
	var b strings.Builder
	fmt.Fprintf(&b, "%s  %s\n", t.Title.Render(m.opts.Title), t.Muted.Render(m.opts.Subtitle))
	for _, note := range m.opts.Notes {
		if note.Warn {
			fmt.Fprintf(&b, "%s\n", t.Warn.Render(glyphWarn+" "+note.Text))
		} else {
			fmt.Fprintf(&b, "%s\n", t.Muted.Render(note.Text))
		}
	}
	b.WriteString("\n")

	if len(m.opts.Rows) == 0 {
		fmt.Fprintf(&b, "  %s\n", t.Muted.Render("No profiles yet. Press n to log in or i to import the current login."))
	}
	width := 0
	for _, row := range m.opts.Rows {
		width = max(width, lipgloss.Width(row.Name))
	}
	for i, row := range m.opts.Rows {
		fmt.Fprintf(&b, "%s\n", m.row(row, i == m.cursor, width))
	}
	if m.cursor < len(m.opts.Rows) && m.opts.Rows[m.cursor].Extra != "" {
		fmt.Fprintf(&b, "\n  %s\n", t.Muted.Render(m.opts.Rows[m.cursor].Extra))
	}

	hints := []string{"↑/↓", "move"}
	for i, action := range m.opts.Actions {
		if action.NeedsRow && len(m.opts.Rows) == 0 {
			continue
		}
		key := action.Key
		if i == 0 && action.NeedsRow {
			key = "enter"
		}
		hints = append(hints, key, action.Label)
	}
	hints = append(hints, "q", "quit")
	fmt.Fprintf(&b, "\n%s\n", t.help(hints...))
	return b.String()
}

func (m *dashboardModel) row(row DashboardRow, focused bool, width int) string {
	t := m.theme
	cursor, marker := "  ", "  "
	if focused {
		cursor = t.Cursor.Render(glyphCursor) + " "
	}
	if row.Active {
		marker = t.OK.Render(glyphActive) + " "
	}
	name := row.Name + strings.Repeat(" ", width-lipgloss.Width(row.Name))
	switch {
	case row.Invalid:
		name = t.Muted.Render(name)
	case focused || row.Active:
		name = t.Selected.Render(name)
	}
	detail := t.Muted.Render(row.Detail)
	if focused && !row.Invalid {
		detail = t.Text.Render(row.Detail)
	}
	return strings.TrimRight(cursor+marker+name+"  "+detail, " ")
}

// Dashboard shows profiles and waits for an action key.
func Dashboard(env Env, opts DashboardOptions) (DashboardChoice, error) {
	m := &dashboardModel{opts: opts, theme: env.theme(), cursor: max(0, min(opts.Cursor, len(opts.Rows)-1))}
	if _, err := run(env, m); err != nil {
		return DashboardChoice{}, err
	}
	return m.choice, nil
}
