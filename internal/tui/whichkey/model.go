// Package whichkey renders the leader-key popup. It is a pure function of
// dispatcher state -- it owns nothing mutable, so it cannot desync from the
// dispatcher it describes. It deliberately does not import internal/tui: that
// would be an import cycle, which is why it takes its own Styles.
package whichkey

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/tonobo/calterm/internal/tui/keys"
)

// Styles carries only what the popup draws. The application builds it from its
// own palette.
type Styles struct {
	Breadcrumb lipgloss.Style
	Key        lipgloss.Style
	Group      lipgloss.Style
	Command    lipgloss.Style
	Dim        lipgloss.Style
}

// keyLabel renders a key the way a user would say it, so the breadcrumb reads
// "space · v" rather than showing a raw blank.
func keyLabel(k string) string {
	switch k {
	case " ":
		return "space"
	case "backspace":
		return "bksp"
	case "enter", "esc", "tab":
		return k
	}
	return k
}

// Render draws the pending chord as the same bordered, spacious overlay used
// by ginbox. width and height are hard outer bounds, including the border.
func Render(g *keys.Group, path []string, mode keys.Mode, width, height int, st Styles) string {
	if width < 4 || height < 3 {
		return ""
	}

	crumbs := make([]string, 0, len(path))
	for _, k := range path {
		crumbs = append(crumbs, keyLabel(k))
	}
	description := g.Description
	if description != "" && !strings.HasPrefix(description, "+") {
		description = "+" + description
	}
	header := st.Breadcrumb.Render("» "+strings.Join(crumbs, " · ")+"  ") + st.Group.Render(description)
	footer := st.Dim.Render("q/esc ") + st.Group.Render("close") +
		st.Dim.Render("   bksp ") + st.Group.Render("back")

	items := keys.VisibleItems(g, mode)
	maxKey := 0
	groupCount := 0
	for _, item := range items {
		if w := lipgloss.Width(keyLabel(item.Key)); w > maxKey {
			maxKey = w
		}
		if item.IsGroup {
			groupCount++
		}
	}

	// Border and horizontal padding consume four cells in total.
	innerWidth := width - 4
	if innerWidth < 1 {
		innerWidth = 1
	}
	descBudget := innerWidth - maxKey - 5 // key, two gaps, arrow, two gaps
	if descBudget < 1 {
		descBudget = 1
	}

	entryLines := make([]string, 0, len(items)+1)
	for i, item := range items {
		if i == groupCount && groupCount > 0 && groupCount < len(items) {
			entryLines = append(entryLines, "")
		}
		entryLines = append(entryLines, renderEntry(item, maxKey, descBudget, st))
	}

	// The natural shape matches ginbox: breathing room after the breadcrumb,
	// between groups and commands, and before the footer. Very short terminals
	// drop the decorative blanks first and retain a truthful overflow row.
	lines := []string{header, ""}
	lines = append(lines, entryLines...)
	lines = append(lines, "", footer)
	innerHeight := height - 2 // top and bottom border
	if innerHeight < 1 {
		innerHeight = 1
	}
	if len(lines) > innerHeight {
		budget := innerHeight - 2 // breadcrumb and footer
		if budget < 0 {
			budget = 0
		}
		shown := len(items)
		if shown > budget {
			shown = budget - 1 // reserve the overflow row
			if shown < 0 {
				shown = 0
			}
		}
		lines = []string{header}
		for _, item := range items[:shown] {
			lines = append(lines, renderEntry(item, maxKey, descBudget, st))
		}
		if shown < len(items) && len(lines) < innerHeight-1 {
			lines = append(lines, st.Dim.Render(fmt.Sprintf("+%d more", len(items)-shown)))
		}
		if len(lines) < innerHeight {
			lines = append(lines, footer)
		}
	}

	for i := range lines {
		lines[i] = ansi.Truncate(lines[i], innerWidth, "…")
	}

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(st.Group.GetForeground()).
		Padding(0, 1).
		Width(width).
		MaxWidth(width)
	return box.Render(strings.Join(lines, "\n"))
}

func renderEntry(item keys.Item, keyWidth, descBudget int, st Styles) string {
	label := keyLabel(item.Key)
	padded := st.Key.Render(label) + strings.Repeat(" ", keyWidth-lipgloss.Width(label))
	description := item.Description
	arrow := "→"
	style := st.Command
	if item.IsGroup {
		arrow = "↳"
		style = st.Group
		if description == "" {
			description = "+group"
		}
	}
	description = ansi.Truncate(description, descBudget, "…")
	return padded + "  " + st.Group.Render(arrow) + "  " + style.Render(description)
}
