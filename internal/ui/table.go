package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/prysmsh/cli/internal/style"
)

// PrintTable renders a table to stdout with bold headers and auto-sized columns.
// Row cells may contain ANSI-styled strings; column widths are calculated correctly.
func PrintTable(headers []string, rows [][]string) {
	widths := make([]int, len(headers))
	for i, h := range headers {
		widths[i] = len(h) // headers are always plain text
	}
	for _, row := range rows {
		for i := 0; i < len(headers) && i < len(row); i++ {
			if w := ansi.StringWidth(row[i]); w > widths[i] {
				widths[i] = w
			}
		}
	}

	hdr := make([]string, len(headers))
	for i, h := range headers {
		hdr[i] = style.Bold.Render(padRight(h, widths[i]))
	}
	fmt.Println(strings.Join(hdr, "  "))

	for _, row := range rows {
		cells := make([]string, len(headers))
		for i := range headers {
			cell := ""
			if i < len(row) {
				cell = row[i]
			}
			cells[i] = padRightVisual(cell, widths[i])
		}
		fmt.Println(strings.Join(cells, "  "))
	}
}

// padRight pads a plain string to at least width characters.
func padRight(s string, width int) string {
	if n := width - len(s); n > 0 {
		return s + strings.Repeat(" ", n)
	}
	return s
}

// padRightVisual pads s to at least width visual columns (ANSI-aware).
func padRightVisual(s string, width int) string {
	if n := width - ansi.StringWidth(s); n > 0 {
		return s + strings.Repeat(" ", n)
	}
	return s
}
