package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
)

// Step is one unit of work shown with a spinner while it runs.
type Step struct {
	Title string
	Run   func(r *Reporter) error
}

// Reporter lets a running step update its line. It is safe to call from the
// step's goroutine. The zero Reporter discards everything, for running a
// step without a terminal.
type Reporter struct {
	index int
	send  func(tea.Msg)
}

// Progress reports how much of the step is done, from 0 to 1. The first call
// turns the spinner line into a progress bar.
func (r *Reporter) Progress(fraction float64) {
	if r.send == nil {
		return
	}
	r.send(progressMsg{r.index, max(0, min(fraction, 1))})
}

// Result replaces the step's title once it has finished, for example with
// what it found.
func (r *Reporter) Result(text string) {
	if r.send == nil {
		return
	}
	r.send(resultMsg{r.index, text})
}

type stepState int

const (
	stepPending stepState = iota
	stepRunning
	stepDone
	stepFailed
)

type (
	stepDoneMsg struct {
		index int
		err   error
	}
	progressMsg struct {
		index    int
		fraction float64
	}
	resultMsg struct {
		index int
		text  string
	}
)

type stepsModel struct {
	theme     *Theme
	steps     []Step
	titles    []string
	states    []stepState
	progress  []float64 // negative until a step reports progress
	started   time.Time
	spinner   spinner.Model
	bar       progress.Model
	current   int
	err       error
	cancelled bool
	finished  bool
	transient bool // clear the lines once every step has succeeded
	send      func(tea.Msg)
}

func newSteps(t *Theme, steps []Step) *stepsModel {
	m := &stepsModel{
		theme:    t,
		steps:    steps,
		titles:   make([]string, len(steps)),
		states:   make([]stepState, len(steps)),
		progress: make([]float64, len(steps)),
		spinner:  spinner.New(spinner.WithSpinner(spinner.MiniDot), spinner.WithStyle(t.Accent)),
		bar:      progress.New(progress.WithGradient("#7D6BF0", "#A594FF"), progress.WithWidth(28), progress.WithoutPercentage(), progress.WithColorProfile(t.r.ColorProfile())),
	}
	for i, step := range steps {
		m.titles[i] = step.Title
		m.progress[i] = -1
	}
	return m
}

func (m *stepsModel) Init() tea.Cmd {
	return tea.Batch(m.spinner.Tick, m.start(0))
}

func (m *stepsModel) start(i int) tea.Cmd {
	if i >= len(m.steps) {
		m.finished = true
		return tea.Quit
	}
	m.current = i
	m.states[i] = stepRunning
	m.started = time.Now()
	step := m.steps[i]
	reporter := &Reporter{index: i, send: m.send}
	return func() tea.Msg {
		return stepDoneMsg{i, step.Run(reporter)}
	}
}

func (m *stepsModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			m.cancelled = true
			m.states[m.current] = stepFailed
			return m, tea.Quit
		}
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	case progressMsg:
		m.progress[msg.index] = msg.fraction
	case resultMsg:
		m.titles[msg.index] = msg.text
	case stepDoneMsg:
		if msg.err != nil {
			m.states[msg.index] = stepFailed
			m.err = msg.err
			return m, tea.Quit
		}
		m.states[msg.index] = stepDone
		return m, m.start(msg.index + 1)
	}
	return m, nil
}

func (m *stepsModel) View() string {
	if m.transient && m.finished {
		return ""
	}
	t := m.theme
	var b strings.Builder
	for i, title := range m.titles {
		switch m.states[i] {
		case stepPending:
			fmt.Fprintf(&b, "%s\n", t.Muted.Render(glyphPending+" "+title))
		case stepDone:
			fmt.Fprintf(&b, "%s %s\n", t.OK.Render(glyphOK), title)
		case stepFailed:
			fmt.Fprintf(&b, "%s %s\n", t.Err.Render(glyphFail), title)
		case stepRunning:
			line := m.spinner.View() + " " + title
			if p := m.progress[i]; p >= 0 {
				line += "  " + m.bar.ViewAs(p) + " " + t.Muted.Render(fmt.Sprintf("%3.0f%%", p*100))
			} else if elapsed := time.Since(m.started); elapsed > 3*time.Second {
				line += " " + t.Muted.Render(fmt.Sprintf("%ds", int(elapsed.Seconds())))
			}
			fmt.Fprintf(&b, "%s\n", line)
		}
	}
	return b.String()
}

// RunSteps runs each step in order behind a spinner and stops at the first
// failure, returning its error. Ctrl-C stops waiting and returns
// ErrCancelled; the interrupted step is not undone.
func RunSteps(env Env, steps ...Step) error {
	return runSteps(env, newSteps(env.theme(), steps))
}

func runSteps(env Env, m *stepsModel) error {
	p := tea.NewProgram(m, tea.WithInput(env.In), tea.WithOutput(env.Out))
	m.send = p.Send
	if _, err := p.Run(); err != nil {
		return err
	}
	if m.cancelled {
		return ErrCancelled
	}
	return m.err
}

// Spin runs one step behind a spinner. The spinner line disappears when the
// step succeeds, since the caller reports the result, and stays marked
// with ✗ when it fails.
func Spin(env Env, title string, run func() error) error {
	m := newSteps(env.theme(), []Step{{Title: title, Run: func(*Reporter) error { return run() }}})
	m.transient = true
	return runSteps(env, m)
}
