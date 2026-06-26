package components

import (
	"strings"

	"github.com/mattn/go-runewidth"
)

// Header represents the top title and status bar of the TUI.
type Header struct {
	Title      string // Left-aligned title (e.g. "📊 用量统计")
	StatusText string // Right-aligned status (e.g. "刷新: 5s | Ctrl+C 退出")
}

// NewHeader creates a new Header component.
func NewHeader(title, statusText string) *Header {
	return &Header{
		Title:      title,
		StatusText: statusText,
	}
}

// View renders the header aligned to the given terminal width.
func (h *Header) View(width int) string {
	left := " " + h.Title + " "
	right := " " + h.StatusText + " "

	leftWidth := runewidth.StringWidth(left)
	rightWidth := runewidth.StringWidth(right)

	// If the terminal is too narrow, simply combine them and truncate.
	if leftWidth+rightWidth >= width {
		combined := left + right
		if runewidth.StringWidth(combined) > width {
			return HeaderStyle.Render(runewidth.Truncate(combined, width, ""))
		}
		return HeaderStyle.Render(combined)
	}

	// Calculate spacing for side-by-side alignment.
	spaces := width - leftWidth - rightWidth
	middle := strings.Repeat(" ", spaces)

	return HeaderStyle.Render(left + middle + right)
}
