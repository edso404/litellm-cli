package components

import (
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// EnsureColorProfile forces Lipgloss color profile to TrueColor to prevent drifts in CI/testing environments.
func EnsureColorProfile() {
	lipgloss.SetColorProfile(termenv.TrueColor)
}

// Global UI Palette & Styles
var (
	// Theme colors
	HeaderBgColor = lipgloss.Color("236")
	HeaderFgColor = lipgloss.Color("86")  // Cyan/Teal
	MutedColor    = lipgloss.Color("240") // Dark gray for help text

	// Highlight & Status colors
	SuccessColor = lipgloss.Color("76")  // Green
	ErrorColor   = lipgloss.Color("196") // Red
	WarningColor = lipgloss.Color("220") // Yellow
	InfoColor    = lipgloss.Color("39")  // Light blue

	// Styles
	HeaderStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(HeaderFgColor).
			Background(HeaderBgColor)

	HelpStyle = lipgloss.NewStyle().
			Foreground(MutedColor)

	SuccessStyle = lipgloss.NewStyle().Foreground(SuccessColor)
	ErrorStyle   = lipgloss.NewStyle().Foreground(ErrorColor)
	WarningStyle = lipgloss.NewStyle().Foreground(WarningColor)
	InfoStyle    = lipgloss.NewStyle().Foreground(InfoColor)

	// Bold variants
	BoldStyle = lipgloss.NewStyle().Bold(true)
)
