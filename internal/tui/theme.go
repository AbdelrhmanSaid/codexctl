// Package tui draws codexctl's prompts and styled output. It knows nothing
// about profiles or Codex.
package tui

import (
	"errors"
	"io"
	"os"
	"strings"
	"sync"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/term"
)

var ErrCancelled = errors.New("cancelled")

var (
	accent = lipgloss.AdaptiveColor{Light: "#5B4BDB", Dark: "#A594FF"}
	green  = lipgloss.AdaptiveColor{Light: "#1A7F37", Dark: "#3FB950"}
	yellow = lipgloss.AdaptiveColor{Light: "#9A6700", Dark: "#D29922"}
	red    = lipgloss.AdaptiveColor{Light: "#CF222E", Dark: "#F85149"}
	muted  = lipgloss.AdaptiveColor{Light: "#6E7781", Dark: "#8B949E"}
	faint  = lipgloss.AdaptiveColor{Light: "#D0D7DE", Dark: "#30363D"}
)

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

// Theme is bound to one output stream, so a pipe or NO_COLOR gets plain text.
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

var (
	darkOnce sync.Once
	dark     bool
)

func NewTheme(w io.Writer) *Theme {
	r := lipgloss.NewRenderer(w)
	// Asking for the background color can swallow typed keys, so ask once.
	darkOnce.Do(func() { dark = lipgloss.HasDarkBackground() })
	r.SetHasDarkBackground(dark)
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

// Env is where a program reads keys and draws; Out is normally stderr.
type Env struct {
	In  io.Reader
	Out io.Writer
}

func (e Env) theme() *Theme { return NewTheme(e.Out) }

// Zeros when w is not a terminal.
func size(w io.Writer) (width, height int) {
	f, ok := w.(*os.File)
	if !ok {
		return 0, 0
	}
	width, height, err := term.GetSize(f.Fd())
	if err != nil {
		return 0, 0
	}
	return width, height
}

// A width of zero or less means unknown and leaves the text alone.
func wrap(text string, width int) []string {
	if width > 0 {
		text = ansi.Wrap(text, width, "")
	}
	return strings.Split(text, "\n")
}

func fit(line string, width int) string {
	if width <= 0 {
		return line
	}
	return ansi.Truncate(line, width, "…")
}

func indent(prefix string, lines []string) []string {
	out := make([]string, len(lines))
	for i, line := range lines {
		out[i] = prefix + line
	}
	return out
}

func (t *Theme) help(width int, pairs ...string) []string {
	separator := t.Muted.Render(glyphSeparator)
	var lines []string
	line, used := "", 0
	for i := 0; i+1 < len(pairs); i += 2 {
		hint := t.Key.Render(pairs[i]) + " " + t.Muted.Render(pairs[i+1])
		w := lipgloss.Width(hint)
		switch {
		case line == "":
			line, used = hint, w
		case width > 0 && used+lipgloss.Width(glyphSeparator)+w > width:
			lines = append(lines, line)
			line, used = hint, w
		default:
			line += separator + hint
			used += lipgloss.Width(glyphSeparator) + w
		}
	}
	if line != "" {
		lines = append(lines, line)
	}
	return lines
}

// A plain note has no glyph and starts at the edge unless gutter is set.
func (t *Theme) note(n Note, width int, gutter bool) []string {
	style, glyph := t.Muted, ""
	switch n.Level {
	case LevelOK:
		style, glyph = t.Text, t.OK.Render(glyphOK)
	case LevelWarn:
		style, glyph = t.Warn, t.Warn.Render(glyphWarn)
	case LevelFail:
		style, glyph = t.Text, t.Err.Render(glyphFail)
	}
	margin := ""
	if glyph != "" || gutter {
		margin = "  "
	}
	lines := wrap(n.Text, width-len(margin))
	for i, line := range lines {
		lead := margin
		if i == 0 && glyph != "" {
			lead = glyph + " "
		}
		lines[i] = lead + style.Render(line)
	}
	return lines
}

func (t *Theme) question(title string) string {
	return t.Accent.Render(glyphPrompt) + " " + t.Title.Render(title)
}

func (t *Theme) answered(title, answer string) string {
	if !strings.HasSuffix(title, "?") {
		title += ":"
	}
	return t.OK.Render(glyphOK) + " " + t.Title.Render(title) + " " + t.Accent.Render(answer) + "\n"
}

func (t *Theme) abandoned(title string) string {
	return t.Muted.Render(glyphFail+" "+title) + " " + t.Muted.Render("cancelled") + "\n"
}
