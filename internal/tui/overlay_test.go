package tui

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
)

func TestOverlayBottomRightFloatsAboveStatusBar(t *testing.T) {
	base := strings.Join([]string{
		"title",
		"first body row",
		"second body row",
		"third body row",
		"fourth body row",
		"status bar",
	}, "\n")
	overlay := strings.Join([]string{
		"╭────────╮",
		"│  keys  │",
		"╰────────╯",
	}, "\n")

	got := overlayBottomRight(base, overlay, 1, 1, 40)
	lines := strings.Split(got, "\n")
	if len(lines) != 6 {
		t.Fatalf("overlay changed screen height to %d rows:\n%s", len(lines), got)
	}
	if lines[0] != "title" || lines[5] != "status bar" {
		t.Errorf("overlay covered fixed chrome:\n%s", got)
	}
	for i, line := range lines {
		if width := lipgloss.Width(line); width > 40 {
			t.Errorf("line %d is %d cells, want <= 40: %q", i, width, line)
		}
	}
	if !strings.Contains(lines[2], "╭────────╮") || !strings.Contains(lines[4], "╰────────╯") {
		t.Errorf("popup is not bottom-right above the status row:\n%s", got)
	}
}
