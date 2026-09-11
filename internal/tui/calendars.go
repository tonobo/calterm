package tui

import (
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/tonobo/calterm/internal/model"
	"github.com/tonobo/calterm/internal/store"
)

// calendarRow is one selectable calendar, flattened out of the per-account
// structure so the cursor can be a single index.
type calendarRow struct {
	Account string
	ID      string
	Name    string
	Color   string
}

// calendarRows flattens meta into the selector's row order: accounts in
// discovery order, calendars in the order each account reported them.
func calendarRows(meta *store.Meta) []calendarRow {
	var rows []calendarRow
	for _, acct := range meta.Accounts {
		for _, cal := range acct.Calendars {
			rows = append(rows, calendarRow{
				Account: acct.Name,
				ID:      cal.ID,
				Name:    cal.Name,
				Color:   cal.Color,
			})
		}
	}
	return rows
}

// RenderCalendars draws the selector: every discovered calendar with a checkbox
// showing whether it is currently displayed.
//
// It is a pure function of its arguments, like the other views, so the layout
// can be asserted without driving a terminal.
func RenderCalendars(meta *store.Meta, hidden map[string]bool, cursor, width, height int, st Styles) string {
	if width <= 0 || height <= 0 {
		return ""
	}

	header := st.DayHeader.Render("Calendars")

	if len(meta.Accounts) == 0 {
		lines := []string{header, "", st.Dim.Render("No calendars discovered — press r to sync.")}
		return clampBlock(lines, width, height)
	}

	footer := st.Dim.Render("enter toggle · esc close and save")

	// Build every scrollable line first, remembering which one the cursor
	// landed on, then window the result -- the same shape RenderAgenda's
	// scrollWindow uses. Rendering every account and row and letting
	// clampBlock truncate from the bottom would, past a screenful of
	// calendars, clip the cursor (and the footer) off entirely: j/k would
	// move an invisible cursor and enter would toggle a calendar the user
	// cannot see.
	var body []string
	cursorLine := 0
	row := 0
	for _, acct := range meta.Accounts {
		body = append(body, "", st.Dim.Render("  "+acct.Name))
		if len(acct.Calendars) == 0 {
			// Show the account anyway: an account that discovered nothing is a
			// problem worth seeing, not a reason to omit it.
			body = append(body, st.Dim.Render("      none discovered"))
			continue
		}
		for _, cal := range acct.Calendars {
			if row == cursor {
				cursorLine = len(body)
			}
			body = append(body, renderCalendarRow(acct.Name, cal, hidden, row == cursor, width, st))
			row++
		}
	}

	// The header and the footer (plus its leading blank line) are reserved
	// from the budget so they are always visible, even when the scrollable
	// middle has to be clipped.
	budget := height - 3 // header, blank, footer
	if budget < 1 {
		budget = 1
	}
	lines := append([]string{header}, scrollWindow(body, cursorLine, budget)...)
	lines = append(lines, "", footer)
	return clampBlock(lines, width, height)
}

func renderCalendarRow(account string, cal store.CalendarMeta, hidden map[string]bool, selected bool, width int, st Styles) string {
	marker := "  "
	if selected {
		marker = cursorMarker + " "
	}

	box := "[x]"
	if model.IsHidden(hidden, account, cal.ID) {
		box = "[ ]"
	}

	name := cal.Name
	if name == "" {
		name = cal.ID
	}

	nameStyle := st.Summary
	if selected {
		nameStyle = st.Selected
	}

	dot := st.Calendar(model.CalendarKey(account, cal.ID)).Render("●")

	// Reserve the fixed columns, then split what is left between the name and
	// the ID, which is shown so a user editing the config by hand can see what
	// a hidden entry refers to.
	fixed := lipgloss.Width(marker) + 4 + 4 + 2 // marker, indent, box+space, dot+space
	remaining := width - fixed
	if remaining < 10 {
		remaining = 10
	}
	nameWidth := remaining / 2
	name = pad(truncatePlain(name, nameWidth), nameWidth)
	id := truncatePlain(cal.ID, remaining-nameWidth)

	line := marker + "  " + box + " " + dot + " " + nameStyle.Render(name) + " " + st.Dim.Render(id)
	return truncate(line, width)
}

// clampBlock enforces the width and height it was given, no matter what the
// arithmetic above produced.
func clampBlock(lines []string, width, height int) string {
	if len(lines) > height {
		lines = lines[:height]
	}
	for i := range lines {
		lines[i] = truncate(lines[i], width)
	}
	return strings.Join(lines, "\n")
}
