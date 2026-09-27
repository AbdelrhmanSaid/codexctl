package tui

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func plainTheme() *Theme { return NewTheme(&bytes.Buffer{}) }

func keys(m tea.Model, presses ...string) tea.Model {
	for _, p := range presses {
		var msg tea.KeyMsg
		switch p {
		case "enter":
			msg = tea.KeyMsg{Type: tea.KeyEnter}
		case "esc":
			msg = tea.KeyMsg{Type: tea.KeyEsc}
		case "ctrl+c":
			msg = tea.KeyMsg{Type: tea.KeyCtrlC}
		case "ctrl+a":
			msg = tea.KeyMsg{Type: tea.KeyCtrlA}
		case "up":
			msg = tea.KeyMsg{Type: tea.KeyUp}
		case "down":
			msg = tea.KeyMsg{Type: tea.KeyDown}
		case "left":
			msg = tea.KeyMsg{Type: tea.KeyLeft}
		case "backspace":
			msg = tea.KeyMsg{Type: tea.KeyBackspace}
		case "space":
			msg = tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}}
		default:
			msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(p)}
		}
		m, _ = m.Update(msg)
	}
	return m
}

func items(labels ...string) []Item {
	out := make([]Item, len(labels))
	for i, l := range labels {
		out[i] = Item{Label: l}
	}
	return out
}

func TestConfirm(t *testing.T) {
	tests := []struct {
		name     string
		def      bool
		presses  []string
		yes      bool
		finished string
	}{
		{"enter takes default no", false, []string{"enter"}, false, "No"},
		{"enter takes default yes", true, []string{"enter"}, true, "Yes"},
		{"y answers yes", false, []string{"y"}, true, "Yes"},
		{"arrow then enter", false, []string{"left", "enter"}, true, "Yes"},
		{"esc is no", true, []string{"esc"}, false, "cancelled"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := keys(newConfirm(plainTheme(), ConfirmOptions{Title: "Restart?", Default: tt.def}), tt.presses...).(*confirmModel)
			if !m.done || m.yes != tt.yes {
				t.Fatalf("done=%v yes=%v, want done yes=%v", m.done, m.yes, tt.yes)
			}
			if !strings.Contains(m.View(), tt.finished) {
				t.Fatalf("final view %q does not contain %q", m.View(), tt.finished)
			}
		})
	}
}

func TestSelectMovesAndFilters(t *testing.T) {
	m := keys(newList(plainTheme(), "Pick", items("alpha", "beta", "gamma"), false), "down", "enter").(*listModel)
	if !m.done || m.current() != 1 {
		t.Fatalf("picked %d, want 1", m.current())
	}

	m = keys(newList(plainTheme(), "Pick", items("alpha", "beta", "gamma"), false), "g", "enter").(*listModel)
	if m.current() != 2 {
		t.Fatalf("filtered pick %d, want 2", m.current())
	}

	m = keys(newList(plainTheme(), "Pick", items("alpha", "beta"), false), "z", "enter").(*listModel)
	if m.done {
		t.Fatal("picked with nothing matching the filter")
	}
	m = keys(m, "esc", "enter").(*listModel)
	if !m.done || m.current() != 0 {
		t.Fatalf("esc did not clear the filter: done=%v current=%d", m.done, m.current())
	}

	m = keys(newList(plainTheme(), "Pick", items("alpha"), false), "esc").(*listModel)
	if !m.cancelled {
		t.Fatal("esc without a filter did not cancel")
	}
}

func TestSelectSkipsDisabled(t *testing.T) {
	list := []Item{{Label: "a", Disabled: "broken"}, {Label: "b"}}
	m := keys(newList(plainTheme(), "Pick", list, false), "enter").(*listModel)
	if m.done {
		t.Fatal("picked a disabled item")
	}
	if !strings.Contains(m.View(), "broken") {
		t.Fatal("reason for the disabled item is not shown")
	}
}

func TestMultiSelect(t *testing.T) {
	list := items("a", "b", "c")
	list[2].Checked = true
	m := newList(plainTheme(), "Pick", list, true)
	m.min = 1
	m = keys(m, "space", "down", "down", "space").(*listModel)
	if got := m.chosen(); len(got) != 1 || got[0] != 0 {
		t.Fatalf("chosen = %v, want [0]", got)
	}
	if !strings.Contains(m.View(), glyphBoxOn+" a") || !strings.Contains(m.View(), glyphBoxOff+" c") {
		t.Fatalf("checkboxes not drawn:\n%s", m.View())
	}
	m = keys(m, "ctrl+a").(*listModel)
	if len(m.chosen()) != 3 {
		t.Fatalf("ctrl+a checked %v", m.chosen())
	}
	m = keys(m, "ctrl+a", "enter").(*listModel)
	if m.done || !strings.Contains(m.View(), "select at least 1") {
		t.Fatal("finished with fewer than Min checked")
	}
	m = keys(m, "space", "enter").(*listModel)
	if !m.done || !strings.Contains(m.View(), "c") {
		t.Fatalf("did not finish: %s", m.View())
	}
}

func TestInputValidates(t *testing.T) {
	bad := errors.New("taken")
	m := newInput(plainTheme(), InputOptions{Title: "Name", Validate: func(v string) error {
		if v == "work" {
			return bad
		}
		return nil
	}})
	m = keys(m, "enter").(*inputModel)
	if m.done {
		t.Fatal("accepted an empty value")
	}
	m = keys(m, "work", "enter").(*inputModel)
	if m.done || !strings.Contains(m.View(), "taken") {
		t.Fatalf("accepted an invalid value: %s", m.View())
	}
	m = keys(m, "backspace", "backspace", "backspace", "backspace", "desk", "enter").(*inputModel)
	if !m.done || m.input.Value() != "desk" {
		t.Fatalf("value = %q, done = %v", m.input.Value(), m.done)
	}
}

func TestRunStepsReportsFailure(t *testing.T) {
	var out bytes.Buffer
	boom := errors.New("boom")
	ran := []string{}
	err := RunSteps(Env{In: strings.NewReader(""), Out: &out},
		Step{Title: "first", Run: func(r *Reporter) error {
			ran = append(ran, "first")
			r.Progress(0.5)
			r.Result("first done")
			return nil
		}},
		Step{Title: "second", Run: func(*Reporter) error { ran = append(ran, "second"); return boom }},
		Step{Title: "third", Run: func(*Reporter) error { ran = append(ran, "third"); return nil }},
	)
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want boom", err)
	}
	if strings.Join(ran, ",") != "first,second" {
		t.Fatalf("ran %v", ran)
	}
	final := out.String()
	for _, want := range []string{"✓ first done", "✗ second", "· third"} {
		if !strings.Contains(final, want) {
			t.Fatalf("output does not contain %q:\n%s", want, final)
		}
	}
}

func TestRenderers(t *testing.T) {
	th := plainTheme()
	table := th.Table([]string{"NAME", "EMAIL"}, []Row{{Cells: []string{"work", "a@b"}, Active: true}, {Cells: []string{"home", ""}}})
	for _, want := range []string{"NAME", "● work", "  home   -"} {
		if !strings.Contains(table, want) {
			t.Fatalf("table does not contain %q:\n%s", want, table)
		}
	}
	card := th.Card("work", []string{"active"}, []Field{{"Email", "a@b"}, {"Plan", ""}})
	for _, want := range []string{"work  active", "Email  a@b", "Plan   -"} {
		if !strings.Contains(card, want) {
			t.Fatalf("card does not contain %q:\n%s", want, card)
		}
	}
	list := th.Checklist([]Check{{"good", true}, {"bad", false}})
	for _, want := range []string{"✓ good", "! bad", "1 passed, 1 need attention."} {
		if !strings.Contains(list, want) {
			t.Fatalf("checklist does not contain %q:\n%s", want, list)
		}
	}
}

func TestSpinClearsItsLineOnSuccess(t *testing.T) {
	var out bytes.Buffer
	if err := Spin(Env{In: strings.NewReader(""), Out: &out}, "working", func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	final := out.String()
	if i := strings.LastIndex(final, "working"); i >= 0 && strings.Contains(final[i:], glyphOK) {
		t.Fatalf("spinner left a finished line:\n%q", final)
	}
}

func TestDashboard(t *testing.T) {
	opts := DashboardOptions{
		Title: "codexctl",
		Rows:  []DashboardRow{{Name: "home", Active: true}, {Name: "work"}},
		Actions: []Action{
			{Key: "u", Label: "use", NeedsRow: true},
			{Key: "n", Label: "log in"},
		},
	}
	choose := func(o DashboardOptions, presses ...string) *dashboardModel {
		m := &dashboardModel{opts: o, theme: plainTheme()}
		return keys(m, presses...).(*dashboardModel)
	}
	if m := choose(opts, "down", "enter"); m.choice != (DashboardChoice{Key: "u", Row: 1}) {
		t.Fatalf("enter chose %+v, want use on row 1", m.choice)
	}
	if m := choose(opts, "n"); m.choice.Key != "n" {
		t.Fatalf("n chose %+v", m.choice)
	}
	if m := choose(opts, "x"); m.done {
		t.Fatal("an unknown key closed the dashboard")
	}
	if m := choose(opts, "q"); !m.done || m.choice.Key != "" || m.View() != "" {
		t.Fatalf("q did not quit cleanly: %+v", m.choice)
	}

	empty := opts
	empty.Rows = nil
	m := choose(empty, "u")
	if m.done {
		t.Fatal("a profile action ran with no profiles")
	}
	view := m.View()
	if strings.Contains(view, "use") || !strings.Contains(view, "No profiles yet") {
		t.Fatalf("empty dashboard view:\n%s", view)
	}
}

func TestSelectAllOnlyTouchesFilteredRows(t *testing.T) {
	m := newList(plainTheme(), "Pick", items("work", "workshop", "home"), true)
	m = keys(m, "work", "ctrl+a").(*listModel)
	if got := m.chosen(); len(got) != 2 || got[0] != 0 || got[1] != 1 {
		t.Fatalf("chosen = %v, want only the filtered rows [0 1]", got)
	}
}

func TestRunStepsWaitsForCancelledStep(t *testing.T) {
	var out bytes.Buffer
	finished := false
	err := RunSteps(Env{In: strings.NewReader("\x03"), Out: &out},
		Step{Title: "slow", Run: func(r *Reporter) error {
			<-r.Context().Done()
			finished = true
			return r.Context().Err()
		}},
		Step{Title: "never", Run: func(*Reporter) error {
			t.Error("a step ran after cancelling")
			return nil
		}},
	)
	if !errors.Is(err, ErrCancelled) {
		t.Fatalf("err = %v, want ErrCancelled", err)
	}
	if !finished {
		t.Fatal("RunSteps returned before the cancelled step finished")
	}
}

func TestCancelledStepThatFinishesCountsAsDone(t *testing.T) {
	var out bytes.Buffer
	err := RunSteps(Env{In: strings.NewReader("\x03"), Out: &out},
		Step{Title: "stubborn", Run: func(r *Reporter) error {
			<-r.Context().Done()
			return nil
		}},
	)
	if err != nil {
		t.Fatalf("err = %v, want nil for a step that completed", err)
	}
}

func TestDashboardFillsTheTerminal(t *testing.T) {
	opts := DashboardOptions{
		Title:    "codexctl",
		Subtitle: "1.0.0",
		Notes:    []Note{{Text: "The Codex daemon (pid 42) still uses the previous credentials; press R to restart it.", Level: LevelWarn}},
		Rows: []DashboardRow{
			{Name: "home", Detail: "someone@example.com · Plus", Extra: []string{"Account 123"}, Active: true},
			{Name: "work", Detail: "someone.else@example.com · Team"},
		},
		Results: []Note{{Text: "Now using profile home", Level: LevelOK}, {Text: "Restart running Codex clients to pick it up."}},
		Actions: []Action{
			{Key: "u", Label: "use", NeedsRow: true},
			{Key: "r", Label: "rename", NeedsRow: true},
			{Key: "n", Label: "log in"},
			{Key: "R", Label: "restart daemon"},
			{Key: "D", Label: "doctor"},
		},
	}
	for _, size := range [][2]int{{100, 30}, {40, 20}, {32, 22}} {
		m := &dashboardModel{opts: opts, theme: plainTheme()}
		m.resize(size[0], size[1])
		view := m.View()
		lines := strings.Split(view, "\n")
		if len(lines) != size[1] {
			t.Fatalf("%dx%d: view has %d lines:\n%s", size[0], size[1], len(lines), view)
		}
		for _, line := range lines {
			if w := lipgloss.Width(line); w > size[0] {
				t.Fatalf("%dx%d: line %q is %d wide", size[0], size[1], line, w)
			}
		}
		for _, want := range []string{"restart daemon", "quit", "Now using profile home"} {
			if !strings.Contains(view, want) {
				t.Fatalf("%dx%d: view does not contain %q:\n%s", size[0], size[1], want, view)
			}
		}
		// Results take up room that was blank, so the rest stays put.
		bare := &dashboardModel{opts: opts, theme: plainTheme()}
		bare.opts.Results = nil
		bare.resize(size[0], size[1])
		without := strings.Split(bare.View(), "\n")
		if lines[0] != without[0] || lines[len(lines)-1] != without[len(without)-1] || len(lines) != len(without) {
			t.Fatalf("%dx%d: results moved the dashboard:\n%s", size[0], size[1], view)
		}
	}
}

func TestDashboardScrollsAndClips(t *testing.T) {
	opts := DashboardOptions{Title: "codexctl", Actions: []Action{{Key: "u", Label: "use", NeedsRow: true}}}
	for _, name := range []string{"a", "b", "c", "d", "e", "f", "g", "h"} {
		opts.Rows = append(opts.Rows, DashboardRow{Name: "profile-" + name})
	}
	for i := 0; i < 10; i++ {
		opts.Results = append(opts.Results, Note{Text: "check", Level: LevelOK})
	}
	m := &dashboardModel{opts: opts, theme: plainTheme()}
	m.resize(60, 16)
	m = keys(m, "end").(*dashboardModel)
	view := m.View()
	if got := len(strings.Split(view, "\n")); got != 16 {
		t.Fatalf("view has %d lines, want 16:\n%s", got, view)
	}
	for _, want := range []string{"›   profile-h", "↑ 5 more", "more lines", "enter use"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view does not contain %q:\n%s", want, view)
		}
	}

	// A chosen action keeps the profiles and frees the rest for its prompts.
	m = keys(m, "enter").(*dashboardModel)
	view = m.View()
	if !strings.Contains(view, "profile-h") || strings.Contains(view, "check") || strings.Contains(view, "quit") {
		t.Fatalf("view after choosing an action:\n%s", view)
	}
}

func TestHelpWraps(t *testing.T) {
	th := plainTheme()
	hints := []string{"↑/↓", "move", "enter", "select", "type", "filter", "esc", "cancel"}
	if lines := th.help(0, hints...); len(lines) != 1 {
		t.Fatalf("help without a width = %q", lines)
	}
	lines := th.help(24, hints...)
	if len(lines) < 2 {
		t.Fatalf("help did not wrap: %q", lines)
	}
	for _, line := range lines {
		if lipgloss.Width(line) > 24 {
			t.Fatalf("line %q is wider than 24", line)
		}
	}
	m := newList(th, "Pick", items("alpha"), false)
	m.resize(24, 10)
	for _, line := range strings.Split(m.View(), "\n") {
		if lipgloss.Width(line) > 24 {
			t.Fatalf("list line %q is wider than 24", line)
		}
	}
}

func TestDashboardNarrowKeepsDetailAndUsage(t *testing.T) {
	extra := []string{"5h resets in 3 hours", "weekly resets in 5 days", "Account b7fe8e99-0a81-487f-ba9f", "refreshed 4 days ago"}
	opts := DashboardOptions{
		Rows: []DashboardRow{
			{Name: "main", Detail: "someone@example.com · team", Extra: extra, Active: true, Meters: []Meter{{Label: "5h", Left: 84}, {Label: "weekly", Left: 94}}},
			{Name: "key", Detail: "API key"},
		},
	}
	m := &dashboardModel{opts: opts, theme: plainTheme()}
	for _, width := range []int{60, 48} {
		m.resize(width, 24)
		view := m.View()
		for _, want := range append(extra, "84%", "94%") {
			if !strings.Contains(view, want) {
				t.Fatalf("%d wide: view does not contain %q:\n%s", width, want, view)
			}
		}
		if strings.Contains(view, glyphMeter) {
			t.Fatalf("%d wide: bars kept on a narrow screen:\n%s", width, view)
		}
	}
	focused := m.detail()
	m.cursor = 1
	if other := m.detail(); len(other) != len(focused) {
		t.Fatalf("detail is %d lines for one row and %d for another", len(focused), len(other))
	}
	m.resize(140, 24)
	if !strings.Contains(m.View(), glyphMeter) {
		t.Fatal("bars dropped on a wide screen")
	}
}
