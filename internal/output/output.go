// Package output provides formatting and rendering utilities for CLI output.
package output

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/warp-run/prysm-cli/internal/style"
)

// Format represents the output format.
type Format string

const (
	FormatTable Format = "table"
	FormatJSON  Format = "json"
	FormatYAML  Format = "yaml"
	FormatQuiet Format = "quiet"
)

// Writer handles formatted output.
type Writer struct {
	format Format
	out    io.Writer
	err    io.Writer
}

// NewWriter creates a new output writer with the given format.
func NewWriter(format string) *Writer {
	f := Format(strings.ToLower(format))
	if f == "" {
		f = FormatTable
	}
	return &Writer{
		format: f,
		out:    os.Stdout,
		err:    os.Stderr,
	}
}

// Format returns the current output format.
func (w *Writer) Format() Format {
	return w.format
}

// IsJSON returns true if the output format is JSON.
func (w *Writer) IsJSON() bool {
	return w.format == FormatJSON
}

// JSON outputs data as formatted JSON.
func (w *Writer) JSON(data interface{}) error {
	enc := json.NewEncoder(w.out)
	enc.SetIndent("", "  ")
	return enc.Encode(data)
}

// Table creates a new tabwriter for aligned table output.
func (w *Writer) Table() *tabwriter.Writer {
	return tabwriter.NewWriter(w.out, 0, 0, 2, ' ', 0)
}

// Success prints a success message with a green checkmark.
func (w *Writer) Success(format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	fmt.Fprintln(w.out, style.Success.Render("✅ "+msg))
}

// Warning prints a warning message in yellow.
func (w *Writer) Warning(format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	fmt.Fprintln(w.err, style.Warning.Render("⚠️  "+msg))
}

// Error prints an error message in red.
func (w *Writer) Error(format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	fmt.Fprintln(w.err, style.Error.Render("❌ "+msg))
}

// Info prints an info message in cyan.
func (w *Writer) Info(format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	fmt.Fprintln(w.out, style.Info.Render("ℹ️  "+msg))
}

// Debug prints a debug message in gray (only if debug is enabled).
func (w *Writer) Debug(enabled bool, format string, args ...interface{}) {
	if !enabled {
		return
	}
	msg := fmt.Sprintf(format, args...)
	fmt.Fprintln(w.err, style.MutedStyle.Render("[debug] "+msg))
}

// Print writes to stdout.
func (w *Writer) Print(format string, args ...interface{}) {
	fmt.Fprintf(w.out, format, args...)
}

// Println writes a line to stdout.
func (w *Writer) Println(args ...interface{}) {
	fmt.Fprintln(w.out, args...)
}

// StatusColor returns a colored status string.
func StatusColor(status string) string {
	lower := strings.ToLower(status)
	switch lower {
	case "connected", "healthy", "active", "running", "success":
		return style.Success.Render(status)
	case "disconnected", "unhealthy", "failed", "error", "critical":
		return style.Error.Render(status)
	case "pending", "warning", "degraded":
		return style.Warning.Render(status)
	default:
		return status
	}
}
