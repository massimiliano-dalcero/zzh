// Package table prints zzh's listings in two layouts:
//
//   - boxed, for people at a terminal: borders, a bold header, colored
//     cells and, optionally, alternate row backgrounds to follow long rows;
//   - plain, for grep, cut and awk: one line per row, fields separated by a
//     tab, no header, no colors, "-" for empty fields (so that awk, which
//     splits on runs of blanks, never shifts columns).
//
// The plain layout is used with --grep and whenever the output is not a
// terminal, so "zzh ls | grep prod" works without options.
//
// text/tabwriter is not used because it counts the bytes of color codes as
// visible characters and would misalign colored columns.
package table

import (
	"fmt"
	"io"
	"strings"
	"unicode/utf8"

	"zzh/internal/core/style"
)

// Cell is one value with the function that colors it (nil: no color). The
// width of a column is computed on Text, before coloring.
type Cell struct {
	Text  string
	Paint func(string) string
}

// Plain is a cell without color.
func Plain(text string) Cell { return Cell{Text: text} }

// Table is a header and its rows.
type Table struct {
	Headers []string
	Rows    [][]Cell
}

// Add appends a row.
func (t *Table) Add(cells ...Cell) {
	t.Rows = append(t.Rows, cells)
}

// Render writes the table to w: boxed on a terminal unless grep is true,
// plain otherwise.
func (t Table) Render(w io.Writer, grep bool) error {
	if grep || !style.IsTerminal(w) {
		return t.renderPlain(w)
	}
	return t.renderBoxed(w, style.For(w))
}

func (t Table) renderPlain(w io.Writer) error {
	for _, row := range t.Rows {
		fields := make([]string, len(row))
		for i, cell := range row {
			fields[i] = cell.Text
			if strings.TrimSpace(cell.Text) == "" {
				fields[i] = "-"
			}
		}
		if _, err := fmt.Fprintln(w, strings.Join(fields, "\t")); err != nil {
			return err
		}
	}
	return nil
}

// renderBoxed draws the table with box-drawing characters. The background
// of a striped row covers the whole line, borders included, so the band is
// continuous; cell colors only change the foreground and leave it intact.
func (t Table) renderBoxed(w io.Writer, s style.Styler) error {
	widths := make([]int, len(t.Headers))
	for i, header := range t.Headers {
		widths[i] = utf8.RuneCountInString(header)
	}
	for _, row := range t.Rows {
		for i, cell := range row {
			if i < len(widths) {
				widths[i] = max(widths[i], utf8.RuneCountInString(cell.Text))
			}
		}
	}

	rule := func(left, middle, right string) string {
		parts := make([]string, len(widths))
		for i, width := range widths {
			parts[i] = strings.Repeat("─", width+2)
		}
		return s.Dim(left + strings.Join(parts, middle) + right)
	}
	bar := s.Dim("│")

	line := func(cells []Cell, background string, backgroundOff string) string {
		var b strings.Builder
		b.WriteString(background)
		b.WriteString(bar)
		for i, width := range widths {
			var cell Cell
			if i < len(cells) {
				cell = cells[i]
			}
			text := cell.Text
			if cell.Paint != nil {
				text = cell.Paint(text)
			}
			b.WriteString(" ")
			b.WriteString(text)
			b.WriteString(strings.Repeat(" ", width-utf8.RuneCountInString(cell.Text)))
			b.WriteString(" ")
			b.WriteString(bar)
		}
		b.WriteString(backgroundOff)
		return b.String()
	}

	headerCells := make([]Cell, len(t.Headers))
	for i, header := range t.Headers {
		headerCells[i] = Cell{Text: header, Paint: s.Name}
	}

	lines := []string{rule("┌", "┬", "┐"), line(headerCells, "", ""), rule("├", "┼", "┤")}
	stripeOn, stripeOff := s.StripeBackground()
	for index, row := range t.Rows {
		if index%2 == 1 {
			lines = append(lines, line(row, stripeOn, stripeOff))
		} else {
			lines = append(lines, line(row, "", ""))
		}
	}
	lines = append(lines, rule("└", "┴", "┘"))

	_, err := fmt.Fprintln(w, strings.Join(lines, "\n"))
	return err
}

// RenderBoxedForTest exposes the boxed layout with a chosen styler, so that
// the commands can test their colored output without a terminal.
func (t Table) RenderBoxedForTest(w io.Writer, s style.Styler) error {
	return t.renderBoxed(w, s)
}
