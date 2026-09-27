// Package tui draws codexctl's interactive prompts and styled output with
// Bubble Tea and Lip Gloss. It knows nothing about profiles or Codex; the
// cli package decides when a terminal is interactive and adapts its data.
package tui

import (
	"errors"
	"io"

	"github.com/charmbracelet/lipgloss"
)

// ErrCancelled is returned when the user leaves a prompt with Esc or Ctrl-C.
var ErrCancelled = errors.New("cancelled")

var (
	accent = lipgloss.AdaptiveColor{Light: "#5B4BDB", Dark: "#A594FF"}
	green  = lipgloss.AdaptiveColor{Light: "#1A7F37", Dark: "#3FB950"}
	yellow = lipgloss.AdaptiveColor{Light: "#9A6700", Dark: "#D29922"}
	red    = lipgloss.AdaptiveColor{Light: "#CF222E", Dark: "#F85149"}
	muted  = lipgloss.AdaptiveColor{Light: "#6E7781", Dark: "#8B949E"}
	faint  = lipgloss.AdaptiveColor{Light: "#D0D7DE", Dark: "#30363D"}
)

// Glyphs used across screens.
const (
	glyphOK        = "✓"
	glyphFail      = "✗"
	glyphWarn      = "!"
	glyphPending   = "·"
	glyphPrompt    = "?"
	glyphCursor    = "›"
	glyphRadioOn   = "◉"
	glyphRadioOff  = "○"
	glyphBoxOn     = "[x]"
	glyphBoxOff    = "[ ]"
	glyphActive    = "●"
	glyphSeparator = " · "
)

// Theme is a set of styles bound to one output stream. Colors are chosen
// for that stream, so a pipe, a dumb terminal or NO_COLOR gets plain text.
type Theme struct {
	r *lipgloss.Renderer

	Title    lipgloss.Style
	Text     lipgloss.Style
	Muted    lipgloss.Style
	Accent   lipgloss.Style
	OK       lipgloss.Style
	Warn     lipgloss.Style
	Err      lipgloss.Style
	Key      lipgloss.Style
	Cursor   lipgloss.Style
	Selected lipgloss.Style
	Header   lipgloss.Style
	Box      lipgloss.Style
	Badge    lipgloss.Style
	Button   lipgloss.Style
	ButtonOn lipgloss.Style
	Danger   lipgloss.Style
}

// NewTheme returns styles for output written to w.
func NewTheme(w io.Writer) *Theme {
	r := lipgloss.NewRenderer(w)
	button := r.NewStyle().Padding(0, 2).Foreground(muted)
	return &Theme{
		r:        r,
		Title:    r.NewStyle().Bold(true),
		Text:     r.NewStyle(),
		Muted:    r.NewStyle().Foreground(muted),
		Accent:   r.NewStyle().Foreground(accent),
		OK:       r.NewStyle().Foreground(green),
		Warn:     r.NewStyle().Foreground(yellow),
		Err:      r.NewStyle().Foreground(red),
		Key:      r.NewStyle().Foreground(muted).Bold(true),
		Cursor:   r.NewStyle().Foreground(accent).Bold(true),
		Selected: r.NewStyle().Foreground(accent).Bold(true),
		Header:   r.NewStyle().Foreground(muted).Bold(true),
		Box: r.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(faint).
			Padding(0, 1),
		Badge:    r.NewStyle().Foreground(green).Bold(true),
		Button:   button,
		ButtonOn: button.Foreground(lipgloss.AdaptiveColor{Light: "#FFFFFF", Dark: "#0D1117"}).Background(accent).Bold(true),
		Danger:   button.Foreground(lipgloss.AdaptiveColor{Light: "#FFFFFF", Dark: "#0D1117"}).Background(red).Bold(true),
	}
}

// Env is where an interactive program reads keys and draws. Out is normally
// stderr, so a command's stdout stays clean for its result.
type Env struct {
	In  io.Reader
	Out io.Writer
}

func (e Env) theme() *Theme { return NewTheme(e.Out) }

// help renders a key hint line such as "enter confirm · esc cancel".
func (t *Theme) help(pairs ...string) string {
	out := ""
	for i := 0; i+1 < len(pairs); i += 2 {
		if i > 0 {
			out += t.Muted.Render(glyphSeparator)
		}
		out += t.Key.Render(pairs[i]) + " " + t.Muted.Render(pairs[i+1])
	}
	return out
}

// question renders the "? Title" line every prompt starts with.
func (t *Theme) question(title string) string {
	return t.Accent.Render(glyphPrompt) + " " + t.Title.Render(title)
}

// answered renders a finished prompt as one line that stays in scrollback.
func (t *Theme) answered(title, answer string) string {
	return t.OK.Render(glyphOK) + " " + t.Title.Render(title) + " " + t.Accent.Render(answer) + "\n"
}

// abandoned renders a prompt the user cancelled.
func (t *Theme) abandoned(title string) string {
	return t.Muted.Render(glyphFail+" "+title) + " " + t.Muted.Render("cancelled") + "\n"
}
