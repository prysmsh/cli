// Package style provides consistent terminal styling via Lipgloss.
package style

import (
	"io"
	"os"

	"github.com/charmbracelet/lipgloss"
)

// Style is a Lipgloss style (exported so callers can store style references).
type Style = lipgloss.Style

// Palette for Prysm CLI (works in 256-color and true color).
var (
	Green   = lipgloss.AdaptiveColor{Light: "#04B575", Dark: "#04B575"}
	Red     = lipgloss.AdaptiveColor{Light: "#A46A5A", Dark: "#C96A57"}
	Yellow  = lipgloss.AdaptiveColor{Light: "#D4A00D", Dark: "#D4A00D"}
	Cyan    = lipgloss.AdaptiveColor{Light: "#00A4B4", Dark: "#00A4B4"}
	Blue    = lipgloss.AdaptiveColor{Light: "#2563EB", Dark: "#3B82F6"}
	Magenta = lipgloss.AdaptiveColor{Light: "#9333EA", Dark: "#A855F7"}
	Muted   = lipgloss.AdaptiveColor{Light: "#6B7280", Dark: "#9CA3AF"}
	Brand   = lipgloss.AdaptiveColor{Light: "#6366F1", Dark: "#818CF8"}
	Bright  = lipgloss.AdaptiveColor{Light: "#FAFAFA", Dark: "#F5F5F5"}
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
		Bold(false)

	// Info for neutral hints and links.
	Info = lipgloss.NewStyle().
		Foreground(Cyan)

	// Muted for secondary text (debug, timestamps).
	MutedStyle = lipgloss.NewStyle().
			Foreground(Muted)

	// Bold for table headers and section titles (no color).
	Bold = lipgloss.NewStyle().
		Bold(true)

	// Code for highlighted text (e.g. device code in login).
	Code = lipgloss.NewStyle().
		Bold(true).
		Foreground(Bright)

	// Blue for DERP/log messages.
	BlueStyle = lipgloss.NewStyle().
			Foreground(Blue)

	// Magenta for DERP/log messages.
	MagentaStyle = lipgloss.NewStyle().
			Foreground(Magenta)

	// VersionBox wraps the version line in a subtle border.
	VersionBox = lipgloss.NewStyle().
			BorderStyle(lipgloss.RoundedBorder()).
			BorderForeground(Muted).
			Padding(0, 2).
			MarginBottom(1)

	// WelcomeBox wraps the app name and tagline (e.g. default prysm view).
	WelcomeBox = lipgloss.NewStyle().
			BorderStyle(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.AdaptiveColor{Light: "#6366F1", Dark: "#818CF8"}).
			Padding(0, 2).
			MarginBottom(1)

	// Tagline for one-liner under the product name.
	Tagline = lipgloss.NewStyle().
		Foreground(Muted).
		Bold(false)

	// SectionHeader for uppercase group labels in the help menu.
	SectionHeader = lipgloss.NewStyle().
			Foreground(Brand).
			Bold(true)

	// HintKey for the left column in footer hints (e.g. "prysm login").
	HintKey = lipgloss.NewStyle().
		Foreground(Bright).
		Bold(false)
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
