// Package style provides consistent terminal styling via Lipgloss.
package style

import (
	"io"
	"os"

	"github.com/charmbracelet/lipgloss"
)

// Palette for Prysm CLI (works in 256-color and true color).
var (
	Green  = lipgloss.AdaptiveColor{Light: "#04B575", Dark: "#04B575"}
	Red    = lipgloss.AdaptiveColor{Light: "#E03C31", Dark: "#E03C31"}
	Yellow = lipgloss.AdaptiveColor{Light: "#D4A00D", Dark: "#D4A00D"}
	Cyan   = lipgloss.AdaptiveColor{Light: "#00A4B4", Dark: "#00A4B4"}
	Muted  = lipgloss.AdaptiveColor{Light: "#6B7280", Dark: "#9CA3AF"}
	Brand  = lipgloss.AdaptiveColor{Light: "#6366F1", Dark: "#818CF8"}
)

// Styles for common CLI output.
var (
	// Title is for the app name / version header.
	Title = lipgloss.NewStyle().
		Bold(true).
		Foreground(Brand).
		MarginRight(1)

	// Success for confirmations and positive outcomes.
	Success = lipgloss.NewStyle().
		Foreground(Green).
		Bold(false)

	// Warning for non-fatal cautions.
	Warning = lipgloss.NewStyle().
		Foreground(Yellow)

	// Error for failures.
	Error = lipgloss.NewStyle().
		Foreground(Red).
		Bold(true)

	// Info for neutral hints and links.
	Info = lipgloss.NewStyle().
		Foreground(Cyan)

	// Muted for secondary text (debug, timestamps).
	MutedStyle = lipgloss.NewStyle().
			Foreground(Muted)

	// VersionBox wraps the version line in a subtle border.
	VersionBox = lipgloss.NewStyle().
			BorderStyle(lipgloss.RoundedBorder()).
			BorderForeground(Muted).
			Padding(0, 2).
			MarginBottom(1)
)

// Renderer returns a Lipgloss renderer for the given writer (for TTY/color detection).
func Renderer(w io.Writer) *lipgloss.Renderer {
	if w == os.Stdout {
		return lipgloss.DefaultRenderer()
	}
	return lipgloss.NewRenderer(w)
}

// RenderVersion returns a styled version string: "prysm version 1.2.3".
func RenderVersion(name, version string) string {
	s := Title.Render(name) + MutedStyle.Render("version "+version)
	return VersionBox.Render(s)
}
