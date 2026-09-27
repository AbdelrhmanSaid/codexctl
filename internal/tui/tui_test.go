package tui

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
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
			m := keys(newConfirm(plainTheme(), ConfirmOptions{Title: "Restart?", Default: tt.def}), tt.presses...).(confirmModel)
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
	m = keys(m, "enter").(inputModel)
	if m.done {
		t.Fatal("accepted an empty value")
	}
	m = keys(m, "work", "enter").(inputModel)
	if m.done || !strings.Contains(m.View(), "taken") {
		t.Fatalf("accepted an invalid value: %s", m.View())
	}
	m = keys(m, "backspace", "backspace", "backspace", "backspace", "desk", "enter").(inputModel)
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
