package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

func (t *Theme) Success(message string) string {
	return t.OK.Render(glyphOK) + " " + message + "\n"
}

func (t *Theme) Warning(message string) string {
	return t.Warn.Render(glyphWarn+" "+message) + "\n"
}

func (t *Theme) Failure(message string) string {
	return t.Err.Render(glyphFail) + " " + message + "\n"
}

func (t *Theme) Hint(message string) string {
	return t.Muted.Render("  "+message) + "\n"
}

func (t *Theme) Name(name string) string {
	return t.Accent.Render(name)
}

// Meter is a bar of width cells, filled to the percent left and colored by
// how little that is.
func (t *Theme) Meter(left float64, width int) string {
	filled := min(width, max(0, int(left/100*float64(width)+0.5)))

	style := t.OK
	switch {
	case left < 10:
		style = t.Err
	case left < 30:
		style = t.Warn
	}

	return style.Render(strings.Repeat(glyphMeter, filled)) + t.Faint.Render(strings.Repeat(glyphMeter, width-filled))
}

type Row struct {
	Cells  []string
	Active bool
	Faded  bool
}

func (t *Theme) Table(header []string, rows []Row) string {
	widths := make([]int, len(header))
	for i, heading := range header {
		widths[i] = lipgloss.Width(heading)
	}

	for _, row := range rows {
		for i, cell := range row.Cells {
			widths[i] = max(widths[i], lipgloss.Width(cell))
		}
	}

	pad := func(text string, width int) string { return text + strings.Repeat(" ", width-lipgloss.Width(text)) }

	var b strings.Builder
	cells := make([]string, len(header))
	for i, heading := range header {
		cells[i] = pad(heading, widths[i])
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

type Field struct {
	Label string
	Value string
}

func (t *Theme) Card(title string, badges []string, fields []Field) string {
	labelWidth := 0
	for _, field := range fields {
		labelWidth = max(labelWidth, lipgloss.Width(field.Label))
	}

	var b strings.Builder
	b.WriteString(t.Selected.Render(title))
	for _, badge := range badges {
		fmt.Fprintf(&b, "  %s", t.Badge.Render(badge))
	}

	b.WriteString("\n")

	for _, field := range fields {
		value := field.Value
		if value == "" {
			value = t.Muted.Render("-")
		}

		fmt.Fprintf(&b, "\n%s  %s", t.Muted.Render(field.Label+strings.Repeat(" ", labelWidth-lipgloss.Width(field.Label))), value)
	}

	return t.Box.Render(b.String()) + "\n"
}

type Check struct {
	Message string
	OK      bool
}

func (t *Theme) Checklist(checks []Check) string {
	var b strings.Builder
	problems := 0

	for _, check := range checks {
		if check.OK {
			fmt.Fprintf(&b, "%s %s\n", t.OK.Render(glyphOK), check.Message)
		} else {
			problems++
			fmt.Fprintf(&b, "%s %s\n", t.Warn.Render(glyphWarn), t.Warn.Render(check.Message))
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
