package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type Item struct {
	Label    string
	Detail   string
	Badge    string // short tag such as "active"
	Disabled string // why the row cannot be chosen
	Checked  bool
}

type SelectOptions struct {
	Title  string
	Items  []Item
	Cursor int
}

type MultiSelectOptions struct {
	Title string
	Items []Item
	Min   int // rows that must be checked
}

// Rows shown before the list scrolls.
const maxRows = 8

type listModel struct {
	sized
	outcome
	theme   *Theme
	title   string
	items   []Item
	multi   bool
	min     int
	checked []bool
	filter  string
	visible []int // indexes into items that match the filter
	cursor  int   // index into visible
	offset  int   // first visible row drawn
	problem string
}

func newList(t *Theme, title string, items []Item, multi bool) *listModel {
	m := &listModel{theme: t, title: title, items: items, multi: multi, checked: make([]bool, len(items))}
	for i, item := range items {
		m.checked[i] = item.Checked && item.Disabled == ""
	}
	m.applyFilter()
	return m
}

func (m *listModel) applyFilter() {
	m.visible = m.visible[:0]
	needle := strings.ToLower(m.filter)
	for i, item := range m.items {
		if needle == "" || strings.Contains(strings.ToLower(item.Label+" "+item.Detail), needle) {
			m.visible = append(m.visible, i)
		}
	}
	m.moveTo(0)
}

func (m *listModel) moveTo(cursor int) {
	m.cursor = max(0, min(cursor, len(m.visible)-1))
	if m.cursor < m.offset {
		m.offset = m.cursor
	}
	if m.cursor >= m.offset+maxRows {
		m.offset = m.cursor - maxRows + 1
	}
	m.offset = max(0, min(m.offset, len(m.visible)-maxRows))
}

// -1 when nothing matches.
func (m *listModel) current() int {
	if len(m.visible) == 0 {
		return -1
	}
	return m.visible[m.cursor]
}

func (m *listModel) chosen() []int {
	var out []int
	for i, on := range m.checked {
		if on {
			out = append(out, i)
		}
	}
	return out
}

func (m *listModel) Init() tea.Cmd { return nil }

func (m *listModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	m.problem = ""
	switch key.String() {
	case "ctrl+c":
		m.cancel()
	case "esc":
		if m.filter != "" {
			m.filter = ""
			m.applyFilter()
		} else {
			m.cancel()
		}
	case "up", "ctrl+p", "shift+tab":
		m.moveTo(m.cursor - 1)
	case "down", "ctrl+n", "tab":
		m.moveTo(m.cursor + 1)
	case "home":
		m.moveTo(0)
	case "end":
		m.moveTo(len(m.visible) - 1)
	case "pgup":
		m.moveTo(m.cursor - maxRows)
	case "pgdown":
		m.moveTo(m.cursor + maxRows)
	case "backspace":
		if m.filter != "" {
			m.filter = string([]rune(m.filter)[:len([]rune(m.filter))-1])
			m.applyFilter()
		}
	case " ":
		if m.multi {
			m.toggle(m.current())
		} else {
			m.pick()
		}
	case "ctrl+a":
		if m.multi {
			m.toggleAll()
		}
	case "enter":
		if m.multi {
			if n := len(m.chosen()); n < m.min {
				m.problem = fmt.Sprintf("select at least %d", m.min)
			} else {
				m.done = true
			}
		} else {
			m.pick()
		}
	default:
		if key.Type == tea.KeyRunes {
			m.filter += string(key.Runes)
			m.applyFilter()
		}
	}
	if m.done {
		return m, tea.Quit
	}
	return m, nil
}

func (m *listModel) pick() {
	i := m.current()
	switch {
	case i < 0:
		m.problem = "nothing matches the filter"
	case m.items[i].Disabled != "":
		m.problem = m.items[i].Disabled
	default:
		m.done = true
	}
}

func (m *listModel) toggle(i int) {
	if i < 0 {
		return
	}
	if m.items[i].Disabled != "" {
		m.problem = m.items[i].Disabled
		return
	}
	m.checked[i] = !m.checked[i]
}

// Rows hidden by the filter are left alone.
func (m *listModel) toggleAll() {
	all := true
	for _, i := range m.visible {
		if m.items[i].Disabled == "" && !m.checked[i] {
			all = false
		}
	}
	for _, i := range m.visible {
		if m.items[i].Disabled == "" {
			m.checked[i] = !all
		}
	}
}

func (m *listModel) View() string {
	t := m.theme
	if m.done {
		if m.cancelled {
			return t.abandoned(m.title)
		}
		if !m.multi {
			return t.answered(m.title, m.items[m.current()].Label)
		}
		labels := []string{}
		for _, i := range m.chosen() {
			labels = append(labels, m.items[i].Label)
		}
		if len(labels) == 0 {
			return t.answered(m.title, "none")
		}
		return t.answered(m.title, strings.Join(labels, ", "))
	}

	var b strings.Builder
	fmt.Fprintf(&b, "%s\n", t.question(m.title))
	if m.filter != "" {
		fmt.Fprintf(&b, "  %s%s\n", t.Muted.Render("filter: "), t.Accent.Render(m.filter))
	}
	width := 0
	for _, item := range m.items {
		width = max(width, lipgloss.Width(item.Label))
	}
	if len(m.visible) == 0 {
		fmt.Fprintf(&b, "  %s\n", t.Muted.Render("nothing matches"))
	}
	if m.offset > 0 {
		fmt.Fprintf(&b, "  %s\n", t.Muted.Render(fmt.Sprintf("  ↑ %d more", m.offset)))
	}
	end := min(len(m.visible), m.offset+maxRows)
	for row := m.offset; row < end; row++ {
		fmt.Fprintf(&b, "%s\n", m.row(m.visible[row], row == m.cursor, width))
	}
	if rest := len(m.visible) - end; rest > 0 {
		fmt.Fprintf(&b, "  %s\n", t.Muted.Render(fmt.Sprintf("  ↓ %d more", rest)))
	}
	b.WriteString("\n")
	writeLines(&b, t.problem(m.problem, m.width-2))
	hints := []string{"↑/↓", "move", "enter", "select", "type", "filter", "esc", "cancel"}
	if m.multi {
		hints = []string{"↑/↓", "move", "space", "toggle", "ctrl+a", "all", "enter", "confirm", "esc", "cancel"}
	}
	writeLines(&b, t.help(m.width-2, hints...))
	return b.String()
}

func (m *listModel) row(i int, focused bool, width int) string {
	t := m.theme
	item := m.items[i]
	cursor := "  "
	if focused {
		cursor = t.Cursor.Render(glyphCursor) + " "
	}
	var mark string
	switch {
	case m.multi && m.checked[i]:
		mark = t.Selected.Render(glyphBoxOn)
	case m.multi:
		mark = t.Muted.Render(glyphBoxOff)
	case focused:
		mark = t.Selected.Render(glyphRadioOn)
	default:
		mark = t.Muted.Render(glyphRadioOff)
	}
	label := item.Label + strings.Repeat(" ", width-lipgloss.Width(item.Label))
	detail := item.Detail
	switch {
	case item.Disabled != "":
		label, detail = t.Muted.Render(label), t.Muted.Render(item.Disabled)
		if item.Detail != "" {
			detail = t.Muted.Render(item.Detail + glyphSeparator + item.Disabled)
		}
	case focused:
		label, detail = t.Selected.Render(label), t.Text.Render(detail)
	default:
		label, detail = t.Text.Render(label), t.Muted.Render(detail)
	}
	line := "  " + cursor + mark + " " + label
	if detail != "" {
		line += "  " + detail
	}
	if item.Badge != "" {
		line += "  " + t.Badge.Render(item.Badge)
	}
	return fit(strings.TrimRight(line, " "), m.width)
}

func Select(env Env, opts SelectOptions) (int, error) {
	m := newList(env.theme(), opts.Title, opts.Items, false)
	for row, i := range m.visible {
		if i == opts.Cursor {
			m.moveTo(row)
		}
	}
	if err := ask(env, m); err != nil {
		return -1, err
	}
	return m.current(), nil
}

func MultiSelect(env Env, opts MultiSelectOptions) ([]int, error) {
	m := newList(env.theme(), opts.Title, opts.Items, true)
	m.min = opts.Min
	if err := ask(env, m); err != nil {
		return nil, err
	}
	return m.chosen(), nil
}
