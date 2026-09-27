package tui

import (
	"fmt"
	"io"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

type DashboardRow struct {
	Name    string
	Detail  string // account summary shown beside the name
	Extra   string // more detail shown under the list for the focused row
	Active  bool
	Invalid bool
}

type Level int

const (
	LevelInfo Level = iota
	LevelOK
	LevelWarn
	LevelFail
)

type Note struct {
	Text  string
	Level Level
}

type Action struct {
	Key      string
	Label    string
	NeedsRow bool // only offered when a profile is focused
}

type DashboardOptions struct {
	Title    string
	Subtitle string
	Notes    []Note // shown under the title
	Rows     []DashboardRow
	Results  []Note // what the last action reported
	Actions  []Action
	Cursor   int
}

// DashboardChoice has an empty Key when the user quit.
type DashboardChoice struct {
	Key string
	Row int
}

// Profiles kept in view when results compete for a short terminal.
const minRows = 3

// Lines of a scrolling list that say how many profiles are out of view.
const scrollLines = 2

type dashboardModel struct {
	sized
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
	last := max(0, len(m.opts.Rows)-1)
	switch k := key.String(); k {
	case "ctrl+c", "esc", "q":
		m.done = true
	case "up", "k":
		m.cursor = max(0, m.cursor-1)
	case "down", "j":
		m.cursor = min(last, m.cursor+1)
	case "home", "g":
		m.cursor = 0
	case "end", "G":
		m.cursor = last
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

// Results fill blank space between the profiles and the key hints, so nothing
// moves.
func (m *dashboardModel) View() string {
	if m.done && m.choice.Key == "" {
		return ""
	}
	head, detail, results, foot := m.head(), m.detail(), m.results(), m.foot()
	room := 0
	if m.height > 0 {
		fixed := len(head) + len(detail) + len(foot)
		keep := max(1, len(m.opts.Rows))
		if keep > minRows {
			keep = minRows + scrollLines
		}
		results = m.clip(results, m.height-fixed-keep)
		room = max(1, m.height-fixed-len(results))
	}
	lines := head
	lines = append(lines, m.rows(room)...)
	lines = append(lines, detail...)
	if m.done {
		// The profiles stay and the action's prompts get the space under
		// them.
		return strings.Join(lines, "\n") + "\n\n"
	}
	lines = append(lines, results...)
	for len(lines)+len(foot) < m.height {
		lines = append(lines, "")
	}
	return strings.Join(append(lines, foot...), "\n")
}

func (m *dashboardModel) head() []string {
	t := m.theme
	lines := []string{fit("  "+t.Title.Render(m.opts.Title)+"  "+t.Muted.Render(m.opts.Subtitle), m.width)}
	for _, note := range m.opts.Notes {
		lines = append(lines, indent("  ", t.note(note, m.width-2, false))...)
	}
	return append(lines, "")
}

// A room of zero draws every profile.
func (m *dashboardModel) rows(room int) []string {
	t := m.theme
	rows := m.opts.Rows
	if len(rows) == 0 {
		text := "No profiles yet. Press n to log in or i to import the current login."
		return indent("    ", wrap(t.Muted.Render(text), m.width-4))
	}
	width := 0
	for _, row := range rows {
		width = max(width, lipgloss.Width(row.Name))
	}
	first, end := 0, len(rows)
	var lines []string
	if room > 0 && len(rows) > room {
		shown := max(1, room-scrollLines)
		first = max(0, min(m.cursor-shown/2, len(rows)-shown))
		end = first + shown
		lines = append(lines, m.more("↑", first))
	}
	for i := first; i < end; i++ {
		lines = append(lines, m.row(rows[i], i == m.cursor, width))
	}
	if end-first < len(rows) {
		lines = append(lines, m.more("↓", len(rows)-end))
	}
	return lines
}

func (m *dashboardModel) more(arrow string, count int) string {
	if count == 0 {
		return ""
	}
	return m.theme.Muted.Render(fmt.Sprintf("    %s %d more", arrow, count))
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
	return fit(strings.TrimRight(cursor+marker+name+"  "+detail, " "), m.width)
}

// Kept for every profile once any has one, so moving does not shift what
// follows.
func (m *dashboardModel) detail() []string {
	for _, row := range m.opts.Rows {
		if row.Extra != "" {
			return []string{"", fit("    "+m.theme.Muted.Render(m.opts.Rows[m.cursor].Extra), m.width)}
		}
	}
	return nil
}

func (m *dashboardModel) results() []string {
	if len(m.opts.Results) == 0 {
		return nil
	}
	lines := []string{""}
	for _, note := range m.opts.Results {
		lines = append(lines, indent("  ", m.theme.note(note, m.width-2, true))...)
	}
	return lines
}

func (m *dashboardModel) clip(results []string, room int) []string {
	switch {
	case len(results) <= room:
		return results
	case room < 2:
		return nil
	}
	hidden := len(results) - room + 1
	return append(results[:room-1:room-1], m.theme.Muted.Render(fmt.Sprintf("    … %d more lines", hidden)))
}

func (m *dashboardModel) foot() []string {
	focused := []string{"↑/↓", "move"}
	var general []string
	for i, action := range m.opts.Actions {
		switch {
		case !action.NeedsRow:
			general = append(general, action.Key, action.Label)
		case len(m.opts.Rows) == 0:
		case i == 0:
			focused = append(focused, "enter", action.Label)
		default:
			focused = append(focused, action.Key, action.Label)
		}
	}
	general = append(general, "q", "quit")
	lines := []string{""}
	if len(m.opts.Rows) > 0 {
		lines = append(lines, indent("  ", m.theme.help(m.width-2, focused...))...)
	}
	return append(lines, indent("  ", m.theme.help(m.width-2, general...))...)
}

// Screen is the alternate screen, held while the dashboard is open so it
// stays in place.
type Screen struct {
	env Env
}

func OpenScreen(env Env) *Screen {
	_, _ = io.WriteString(env.Out, ansi.SetModeAltScreenSaveCursor)
	return &Screen{env: env}
}

func (s *Screen) Close() {
	_, _ = io.WriteString(s.env.Out, ansi.ResetModeAltScreenSaveCursor)
}

func (s *Screen) Dashboard(opts DashboardOptions) (DashboardChoice, error) {
	m := &dashboardModel{opts: opts, theme: s.env.theme(), cursor: max(0, min(opts.Cursor, len(opts.Rows)-1))}
	_, _ = io.WriteString(s.env.Out, ansi.CursorHomePosition+ansi.EraseScreenBelow)
	if err := run(s.env, m); err != nil {
		return DashboardChoice{}, err
	}
	return m.choice, nil
}
