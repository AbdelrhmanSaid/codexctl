package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Success renders "✓ message".
func (t *Theme) Success(message string) string {
	return t.OK.Render(glyphOK) + " " + message + "\n"
}

// Warning renders "! message".
func (t *Theme) Warning(message string) string {
	return t.Warn.Render(glyphWarn+" "+message) + "\n"
}

// Failure renders "✗ message".
func (t *Theme) Failure(message string) string {
	return t.Err.Render(glyphFail) + " " + message + "\n"
}

// Hint renders a muted line of advice.
func (t *Theme) Hint(message string) string {
	return t.Muted.Render("  "+message) + "\n"
}

// Name renders a profile or other name in the accent color.
func (t *Theme) Name(name string) string {
	return t.Accent.Render(name)
}

// Row is one line of a Table. Active rows get a dot and the accent color;
// Faded rows are muted.
type Row struct {
	Cells  []string
	Active bool
	Faded  bool
}

// Table renders rows under a header with aligned columns and no borders.
func (t *Theme) Table(header []string, rows []Row) string {
	widths := make([]int, len(header))
	for i, h := range header {
		widths[i] = lipgloss.Width(h)
	}
	for _, row := range rows {
		for i, cell := range row.Cells {
			widths[i] = max(widths[i], lipgloss.Width(cell))
		}
	}
	pad := func(s string, w int) string { return s + strings.Repeat(" ", w-lipgloss.Width(s)) }
	var b strings.Builder
	cells := make([]string, len(header))
	for i, h := range header {
		cells[i] = pad(h, widths[i])
	}
	fmt.Fprintf(&b, "  %s\n", t.Header.Render(strings.TrimRight(strings.Join(cells, "   "), " ")))
	for _, row := range rows {
		marker := "  "
		for i, cell := range row.Cells {
			if cell == "" {
				cell = "-"
			}
			cells[i] = pad(cell, widths[i])
		}
		line := strings.TrimRight(strings.Join(cells, "   "), " ")
		switch {
		case row.Active:
			marker = t.OK.Render(glyphActive) + " "
			first, rest, _ := strings.Cut(line, "   ")
			line = t.Selected.Render(first) + "   " + rest
		case row.Faded:
			line = t.Muted.Render(line)
		}
		fmt.Fprintf(&b, "%s%s\n", marker, line)
	}
	return b.String()
}

// Field is one label and value in a Card.
type Field struct {
	Label string
	Value string
}

// Card renders a titled box of fields, with badges beside the title.
func (t *Theme) Card(title string, badges []string, fields []Field) string {
	width := 0
	for _, f := range fields {
		width = max(width, lipgloss.Width(f.Label))
	}
	var b strings.Builder
	b.WriteString(t.Selected.Render(title))
	for _, badge := range badges {
		fmt.Fprintf(&b, "  %s", t.Badge.Render(badge))
	}
	b.WriteString("\n")
	for _, f := range fields {
		value := f.Value
		if value == "" {
			value = t.Muted.Render("-")
		}
		fmt.Fprintf(&b, "\n%s  %s", t.Muted.Render(f.Label+strings.Repeat(" ", width-lipgloss.Width(f.Label))), value)
	}
	return t.Box.Render(b.String()) + "\n"
}

// Check is one line of a Checklist.
type Check struct {
	Message string
	OK      bool
}

// Checklist renders checks as ✓ and ! lines followed by a summary.
func (t *Theme) Checklist(checks []Check) string {
	var b strings.Builder
	problems := 0
	for _, c := range checks {
		if c.OK {
			fmt.Fprintf(&b, "%s %s\n", t.OK.Render(glyphOK), c.Message)
		} else {
			problems++
			fmt.Fprintf(&b, "%s %s\n", t.Warn.Render(glyphWarn), t.Warn.Render(c.Message))
		}
	}
	b.WriteString("\n")
	passed := len(checks) - problems
	if problems == 0 {
		fmt.Fprintf(&b, "%s\n", t.OK.Render(fmt.Sprintf("All %d checks passed.", passed)))
	} else {
		fmt.Fprintf(&b, "%s%s\n", t.Muted.Render(fmt.Sprintf("%d passed, ", passed)), t.Warn.Render(fmt.Sprintf("%d need attention.", problems)))
	}
	return b.String()
}
