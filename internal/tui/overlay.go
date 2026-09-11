package tui

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// overlayBottomRight ports ginbox's which-key placement: the popup floats
// above the existing screen instead of becoming another block in the layout.
// The final hard cap protects the right border from any over-wide base row.
func overlayBottomRight(base, overlay string, marginBottom, marginRight, termWidth int) string {
	if overlay == "" || termWidth <= 0 {
		return base
	}
	overlayLines := strings.Split(overlay, "\n")
	popupWidth := 0
	for _, line := range overlayLines {
		if width := lipgloss.Width(line); width > popupWidth {
			popupWidth = width
		}
	}
	if popupWidth > termWidth {
		popupWidth = termWidth
		marginRight = 0
	}

	baseLines := strings.Split(base, "\n")
	start := len(baseLines) - len(overlayLines) - marginBottom
	for i, popupLine := range overlayLines {
		target := start + i
		if target < 0 || target >= len(baseLines) {
			continue
		}
		keep := termWidth - popupWidth - marginRight
		if keep < 0 {
			keep = 0
		}
		prefix := ansi.Truncate(baseLines[target], keep, "")
		if padding := keep - lipgloss.Width(prefix); padding > 0 {
			prefix += strings.Repeat(" ", padding)
		}
		line := prefix + strings.Repeat(" ", marginRight) + popupLine
		baseLines[target] = ansi.Truncate(line, termWidth, "")
	}
	return strings.Join(baseLines, "\n")
}
