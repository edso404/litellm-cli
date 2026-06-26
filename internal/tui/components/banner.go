package components

import (
	"github.com/charmbracelet/lipgloss"
)

// ErrorBanner renders a prominent error box with a rounded border.
type ErrorBanner struct {
	Message string
}

// NewErrorBanner creates a new ErrorBanner component.
func NewErrorBanner(msg string) *ErrorBanner {
	return &ErrorBanner{Message: msg}
}

// View renders the error banner fitting the terminal width.
func (e *ErrorBanner) View(width int) string {
	// Calculate dynamic width, ensuring we don't go negative on extremely narrow screens.
	boxWidth := width - 4
	if boxWidth < 20 {
		boxWidth = 20
	}

	style := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ErrorColor).
		Foreground(ErrorColor).
		Padding(1, 2).
		Width(boxWidth)

	return style.Render("❌ 出错了:\n" + e.Message)
}
