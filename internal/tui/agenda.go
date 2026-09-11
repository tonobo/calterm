package tui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"charm.land/lipgloss/v2"

	"github.com/tonobo/calterm/internal/model"
)

// RenderAgenda draws a chronological list grouped under day headers.
//
// It is a pure function of its arguments so the layout can be asserted without
// driving a terminal.
// calendarsHidden tells RenderAgenda whether the empty state, if reached, is
// because the user has deselected every calendar -- as opposed to the index
// genuinely holding nothing -- so the message names the actual cause instead
// of blaming the cache for what the user just did in the selector.
func RenderAgenda(occs []model.Occurrence, cursor, width, height int, now time.Time, loc *time.Location, st Styles, names map[string]string, calendarsHidden bool) string {
	if loc == nil {
		loc = time.UTC
	}
	if len(occs) == 0 {
		if calendarsHidden {
			return st.Dim.Render("No events — all calendars are hidden (press c to choose).")
		}
		return st.Dim.Render("No events in the indexed window.")
	}

	if height < 1 {
		height = 1
	}
	occs = orderAgendaOccurrences(occs, now, loc)

	// Build every line first, remembering which one the cursor landed on, then
	// window the result. Rendering all lines and clipping would walk the cursor
	// off-screen as soon as the list is longer than the terminal.
	var lines []string
	cursorLine := 0
	lastDay := ""
	today := dayKey(now, loc)

	for i, o := range occs {
		day := agendaDay(o, now, loc)
		dayKey := day.Format("2006-01-02")
		if dayKey != lastDay {
			if lastDay != "" {
				lines = append(lines, "")
			}
			lines = append(lines, renderDayHeader(day, dayKey == today, width, st))
			lastDay = dayKey
		}
		if i == cursor {
			cursorLine = len(lines)
		}
		lines = append(lines, renderAgendaRow(o, i == cursor, width, loc, st, names))
	}
	return strings.Join(scrollWindow(lines, cursorLine, height), "\n")
}

// orderAgendaOccurrences returns the order the agenda presents to the user.
// An event which began on an earlier date but is still active belongs in
// today's agenda block; leaving it at its original start date makes a long
// absence disappear above the scroll window even though the week and day
// views correctly show it on today. The stable sort keeps the index's Start
// order within each displayed day, with carried-over events first.
func orderAgendaOccurrences(occs []model.Occurrence, now time.Time, loc *time.Location) []model.Occurrence {
	ordered := append([]model.Occurrence(nil), occs...)
	sort.SliceStable(ordered, func(i, j int) bool {
		return agendaDay(ordered[i], now, loc).Before(agendaDay(ordered[j], now, loc))
	})
	return ordered
}

// agendaDay is the day heading under which an occurrence is displayed. Dates
// are normalized to UTC only so time.Time comparisons are reliable; all-day
// start/end values themselves remain calendar dates and are never converted
// to the viewer's timezone.
func agendaDay(o model.Occurrence, now time.Time, loc *time.Location) time.Time {
	if loc == nil {
		loc = time.UTC
	}
	var y int
	var m time.Month
	var d int
	if o.AllDay {
		y, m, d = o.Start.UTC().Date()
	} else {
		y, m, d = o.Start.In(loc).Date()
	}
	startDay := time.Date(y, m, d, 0, 0, 0, 0, time.UTC)

	ny, nm, nd := now.In(loc).Date()
	today := time.Date(ny, nm, nd, 0, 0, 0, 0, time.UTC)
	if startDay.Before(today) && !occurrencePast(o, now, loc) {
		return today
	}
	return startDay
}

// occurrencePast reports whether an occurrence has finished. All-day End is
// an exclusive calendar date, so it is compared with today's date rather than
// as a midnight instant (which would drift at non-UTC offsets).
func occurrencePast(o model.Occurrence, now time.Time, loc *time.Location) bool {
	if !o.AllDay {
		return !o.End.After(now)
	}
	if loc == nil {
		loc = time.UTC
	}
	ny, nm, nd := now.In(loc).Date()
	today := time.Date(ny, nm, nd, 0, 0, 0, 0, time.UTC)
	ey, em, ed := o.End.UTC().Date()
	end := time.Date(ey, em, ed, 0, 0, 0, 0, time.UTC)
	return !today.Before(end)
}

// scrollWindow returns at most height lines, positioned so that focus stays
// visible with a third of the screen of context above it.
func scrollWindow(lines []string, focus, height int) []string {
	if len(lines) <= height {
		return lines
	}
	start := focus - height/3
	if start < 0 {
		start = 0
	}
	if start+height > len(lines) {
		start = len(lines) - height
	}
	return lines[start : start+height]
}

func renderDayHeader(day time.Time, isToday bool, width int, st Styles) string {
	_, week := day.ISOWeek()
	label := fmt.Sprintf("%s · Wk %d", day.Format("Mon 2 January"), week)
	style := st.DayHeader
	if isToday {
		label += " · Today"
		style = st.TodayHeader
	}
	return truncate(style.Render(label), width)
}

// timeColumnWidth is the fixed left column: "09:00–09:30" plus the cursor
// marker and a space.
const timeColumnWidth = 14

func renderAgendaRow(o model.Occurrence, selected bool, width int, loc *time.Location, st Styles, names map[string]string) string {
	marker := "  "
	if selected {
		marker = cursorMarker + " "
	}

	when := agendaAllDayLabel(o)
	if !o.AllDay {
		when = o.Start.In(loc).Format("15:04") + "–" + o.End.In(loc).Format("15:04")
	}
	when = pad(when, timeColumnWidth-lipgloss.Width(marker))

	summary := participationSummary(o)
	if summary == "" {
		summary = "(no title)"
	}

	summaryStyle := st.Summary
	if selected {
		summaryStyle = st.Selected
	}

	dot := st.Calendar(o.CalendarKey()).Render("●") + " "

	// Reserve room for the marker, time column, and calendar dot.
	remaining := width - lipgloss.Width(marker) - lipgloss.Width(when) - 2
	if remaining < 8 {
		remaining = 8
	}
	body := truncatePlain(summary, remaining)
	if o.Location != "" {
		withLoc := summary + " · " + o.Location
		if lipgloss.Width(withLoc) <= remaining {
			body = withLoc
		}
	}

	line := marker + st.Time.Render(when) + dot + summaryStyle.Render(body)
	return truncate(line, width)
}

// agendaAllDayLabel keeps a multi-day event from looking like a one-day item
// merely because the agenda groups it under its start date. All-day DTEND is
// exclusive, so the visible endpoint is the preceding calendar day.
func agendaAllDayLabel(o model.Occurrence) string {
	label := "all day"
	if !o.AllDay {
		return label
	}
	start := o.Start.UTC()
	end := o.End.UTC().AddDate(0, 0, -1)
	if end.After(start) {
		label += "→" + end.Format("Mon")
	}
	return label
}

func dayKey(t time.Time, loc *time.Location) string {
	return t.In(loc).Format("2006-01-02")
}

// dayKeyOf keys an occurrence by its calendar day. All-day occurrences are
// keyed in UTC, where their dates are anchored, so they never slide a day when
// the viewer is east or west of UTC.
func dayKeyOf(o model.Occurrence, loc *time.Location) string {
	if o.AllDay {
		return o.Start.UTC().Format("2006-01-02")
	}
	return dayKey(o.Start, loc)
}

func pad(s string, w int) string {
	for lipgloss.Width(s) < w {
		s += " "
	}
	return s
}

// truncate clips a possibly-styled string to width cells.
func truncate(s string, width int) string {
	if lipgloss.Width(s) <= width {
		return s
	}
	return lipgloss.NewStyle().MaxWidth(width).Render(s)
}

// truncatePlain clips unstyled text, adding an ellipsis when it does not fit.
func truncatePlain(s string, width int) string {
	if lipgloss.Width(s) <= width {
		return s
	}
	if width <= 1 {
		return "…"
	}
	runes := []rune(s)
	for len(runes) > 0 && lipgloss.Width(string(runes))+1 > width {
		runes = runes[:len(runes)-1]
	}
	return string(runes) + "…"
}
