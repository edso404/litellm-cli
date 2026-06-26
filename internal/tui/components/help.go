package components

import (
	"strings"

	"github.com/mattn/go-runewidth"
)

// HelpKey represents a single shortcut key and its description.
type HelpKey struct {
	Key  string // E.g. "Tab", "q"
	Desc string // E.g. "切换视图", "退出"
}

// Help manages a collection of shortcuts displayed at the bottom of the TUI.
type Help struct {
	Keys []HelpKey
}

// NewHelp creates a new Help component.
func NewHelp(keys []HelpKey) *Help {
	return &Help{Keys: keys}
}

// View renders the help items inline, adapting spacing to the terminal width.
func (h *Help) View(width int) string {
	var parts []string
	for _, k := range h.Keys {
		parts = append(parts, k.Key+": "+k.Desc)
	}

	// Try standard spaced layout first
	separator := "  |  "
	joined := "  " + strings.Join(parts, separator)
	joinedWidth := runewidth.StringWidth(joined)

	// If too wide, fallback to compact space separator
	if joinedWidth > width {
		separator = "   "
		joined = "  " + strings.Join(parts, separator)
		joinedWidth = runewidth.StringWidth(joined)
	}

	// If still too wide, truncate gracefully with an ellipsis
	if joinedWidth > width {
		return HelpStyle.Render(runewidth.Truncate(joined, width-3, "") + "...")
	}

	return HelpStyle.Render(joined)
}
