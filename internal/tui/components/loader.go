package components

import (
	"github.com/charmbracelet/lipgloss"
)

// Loader displays a standardized loading status.
type Loader struct {
	Message string
}

// NewLoader creates a new Loader component.
func NewLoader(msg string) *Loader {
	return &Loader{Message: msg}
}

// View renders the loading state.
func (l *Loader) View() string {
	return lipgloss.NewStyle().
		Foreground(InfoColor).
		Bold(true).
		Render("⏳ " + l.Message)
}

// Placeholder displays a standardized "no data" placeholder state.
type Placeholder struct {
	Message string
}

// NewPlaceholder creates a new Placeholder component.
func NewPlaceholder(msg string) *Placeholder {
	return &Placeholder{Message: msg}
}

// View renders the empty placeholder state.
func (p *Placeholder) View() string {
	return lipgloss.NewStyle().
		Foreground(MutedColor).
		Render("📦 " + p.Message)
}
