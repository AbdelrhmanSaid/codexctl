package tui

import (
	"fmt"
	"io"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// DashboardRow is one profile on the dashboard.
type DashboardRow struct {
	Name    string
	Detail  string // account summary shown beside the name
	Extra   string // more detail shown under the list for the focused row
	Active  bool
	Invalid bool
}

// Level is how a Note is marked.
type Level int

const (
	LevelInfo Level = iota // muted text
	LevelOK
	LevelWarn
	LevelFail
)

// Note is a line of status on the dashboard.
type Note struct {
	Text  string
	Level Level
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
	Notes    []Note // standing state, shown under the title
	Rows     []DashboardRow
	Results  []Note // what the last action reported, shown under the profiles
	Actions  []Action
	Cursor   int
}

// DashboardChoice is what the user asked for. Key is empty when they quit.
type DashboardChoice struct {
	Key string
	Row int
}

// minRows is how many profiles stay in view when results compete with them
// for a short terminal.
const minRows = 3

// scrollLines is how many lines of a scrolling list say how many profiles
// are out of view.
const scrollLines = 2

type dashboardModel struct {
	opts          DashboardOptions
	theme         *Theme
	cursor        int
	width, height int // zero until the terminal's size is known
	choice        DashboardChoice
	done          bool
}

func (m *dashboardModel) Init() tea.Cmd { return nil }

func (m *dashboardModel) resize(width, height int) { m.width, m.height = width, height }

func (m *dashboardModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if size, ok := msg.(tea.WindowSizeMsg); ok {
		m.resize(size.Width, size.Height)
	}
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

// View fills the terminal: the title and profiles at the top, the key hints
// on the last lines, and the last action's results in between. Nothing moves
// when results come and go.
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
		// An action was chosen. The profiles stay where they are and the
		// action's prompts get the space under them.
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

// rows draws the profiles in at most room lines, scrolling to keep the
// focused one in view. A room of zero draws them all.
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

// more says how many profiles are out of view in one direction.
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

// detail is the line about the focused profile. When any profile has one
// the line is kept for all of them, so moving between profiles does not
// shift what follows.
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

// clip shortens results to room lines, saying how many were left out.
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

// foot is the key hints: the ones for the focused profile, then the rest.
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

// Screen is the terminal's alternate screen, held for as long as the
// dashboard is open. The dashboard and the prompts of its actions draw on
// it, so the dashboard is always in the same place and the scrollback is
// left as it was.
type Screen struct {
	env Env
}

// OpenScreen switches to the alternate screen. Close switches back.
func OpenScreen(env Env) *Screen {
	_, _ = io.WriteString(env.Out, ansi.SetModeAltScreenSaveCursor)
	return &Screen{env: env}
}

// Close returns to the screen as it was before OpenScreen.
func (s *Screen) Close() {
	_, _ = io.WriteString(s.env.Out, ansi.ResetModeAltScreenSaveCursor)
}

// Dashboard shows profiles and waits for an action key. It replaces
// whatever the last action left on the screen. Once an action is chosen the
// profiles stay in view, and what the action draws goes under them.
func (s *Screen) Dashboard(opts DashboardOptions) (DashboardChoice, error) {
	m := &dashboardModel{opts: opts, theme: s.env.theme(), cursor: max(0, min(opts.Cursor, len(opts.Rows)-1))}
	_, _ = io.WriteString(s.env.Out, ansi.CursorHomePosition+ansi.EraseScreenBelow)
	if _, err := run(s.env, m); err != nil {
		return DashboardChoice{}, err
	}
	return m.choice, nil
}
