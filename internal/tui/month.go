package tui

import (
	"fmt"
	"strings"
	"time"

	"charm.land/lipgloss/v2"

	"github.com/tonobo/calterm/internal/model"
)

// weekNumberWidth is the fixed width of the calendar-week column: "Wk" or a
// two-digit week number (2 cells) plus two trailing padding cells --
// mirrors gutterWidth's "09:00 " reasoning in week.go, just with more
// slack since the header label ("Wk") and the widest week number ("53")
// are both narrower than the column.
const weekNumberWidth = 4

// monthGridMinHeight is the compact grid: weekday header plus six week rows.
const monthGridMinHeight = 7

// monthExpandedMinHeight is the point at which every week can have a date
// row, at least one event row, and a separator from the next week. Below it,
// the compact marker grid communicates more than a half-expanded calendar.
const monthExpandedMinHeight = 18

// MonthGrid returns six weeks of seven days covering the month containing
// focus. Six weeks is always enough for any month and any week start, and a
// fixed height keeps the view from jumping as the user pages through months.
func MonthGrid(focus time.Time, weekStart string) [][]time.Time {
	first := time.Date(focus.Year(), focus.Month(), 1, 0, 0, 0, 0, focus.Location())

	startWeekday := time.Monday
	if weekStart == "sunday" {
		startWeekday = time.Sunday
	}
	offset := (int(first.Weekday()) - int(startWeekday) + 7) % 7
	gridStart := first.AddDate(0, 0, -offset)

	grid := make([][]time.Time, 6)
	for w := 0; w < 6; w++ {
		grid[w] = make([]time.Time, 7)
		for d := 0; d < 7; d++ {
			grid[w][d] = gridStart.AddDate(0, 0, w*7+d)
		}
	}
	return grid
}

// RenderMonth draws an adaptive calendar grid. Its compact form is a day
// number plus event markers; additional height becomes preview rows containing
// event times and titles instead of unused whitespace below the calendar.
func RenderMonth(occs []model.Occurrence, focus time.Time, width, height int, now time.Time, loc *time.Location, st Styles, weekStart string) string {
	if loc == nil {
		loc = time.UTC
	}
	if width <= 0 || height <= 0 {
		return ""
	}

	expanded := height >= monthExpandedMinHeight
	separatorWidth := 0
	if expanded {
		separatorWidth = 6 // one vertical rule between each pair of day columns
	}
	cellWidth := (width - weekNumberWidth - separatorWidth) / 7
	if cellWidth < 4 {
		cellWidth = 4
	}
	gridWidth := weekNumberWidth + cellWidth*7 + separatorWidth
	if gridWidth > width {
		gridWidth = width
	}

	byDay := groupByDay(occs, loc)
	grid := MonthGrid(focus, weekStart)
	today := dayKey(now, loc)
	focusKey := dayKey(focus, loc)

	// The focused period is carried by the status bar. Keeping the grid's first
	// row to weekday names avoids reintroducing a second title line.
	var lines []string

	weekdays := []string{"Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun"}
	if weekStart == "sunday" {
		weekdays = []string{"Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"}
	}
	var header strings.Builder
	header.WriteString(pad("Wk", weekNumberWidth))
	for dayIndex, wd := range weekdays {
		if expanded && dayIndex > 0 {
			header.WriteString("│")
		}
		header.WriteString(pad(truncatePlain(wd, cellWidth-1), cellWidth))
	}
	lines = append(lines, truncate(st.Dim.Render(strings.TrimRight(header.String(), " ")), gridWidth))

	weekHeights := monthWeekHeights(height, expanded)
	for weekIndex, week := range grid {
		if expanded && weekIndex > 0 {
			lines = append(lines, renderMonthWeekRule(cellWidth, gridWidth, st))
		}

		var row strings.Builder
		row.WriteString(st.Dim.Render(pad(fmt.Sprintf("%2d", isoWeekForDays(week)), weekNumberWidth)))
		previewRows := weekHeights[weekIndex] - 1
		for dayIndex, day := range week {
			if expanded && dayIndex > 0 {
				row.WriteString(st.GridLine.Render("│"))
			}
			if expanded {
				row.WriteString(renderMonthDateCell(day, focus, today, focusKey, byDay, previewRows, cellWidth, loc, st))
			} else {
				row.WriteString(renderMonthCell(day, focus, today, focusKey, byDay, cellWidth, loc, st))
			}
		}
		lines = append(lines, truncate(strings.TrimRight(row.String(), " "), gridWidth))

		for previewRow := 0; previewRow < weekHeights[weekIndex]-1; previewRow++ {
			var preview strings.Builder
			preview.WriteString(strings.Repeat(" ", weekNumberWidth))
			for dayIndex, day := range week {
				if dayIndex > 0 {
					preview.WriteString(st.GridLine.Render("│"))
				}
				preview.WriteString(renderMonthEventCell(day, focus, byDay, previewRow, cellWidth, loc, st))
			}
			lines = append(lines, truncate(strings.TrimRight(preview.String(), " "), gridWidth))
		}
	}
	// Rendered output must never exceed the caller's height. The compact grid
	// needs seven lines (weekday header, six weeks); unusually short terminals
	// receive its clipped prefix like every other view.
	if height > 0 && len(lines) > height {
		lines = lines[:height]
	}
	return strings.Join(lines, "\n")
}

// monthWeekHeights distributes expanded content evenly across all six weeks.
// Compact mode deliberately stays at seven lines; partially expanding only
// some weeks makes the month harder to scan than leaving the extra rows blank.
func monthWeekHeights(height int, expanded bool) []int {
	const weeks = 6
	heights := make([]int, weeks)
	for i := range heights {
		heights[i] = 1 // the date/marker row
	}
	if !expanded {
		return heights
	}

	// The weekday header and five inter-week rules live outside the bands.
	available := height - 1 - (weeks - 1)
	perWeek, remainder := available/weeks, available%weeks
	for i := range heights {
		heights[i] = perWeek
		if i < remainder {
			heights[i]++
		}
	}
	return heights
}

func renderMonthWeekRule(cellWidth, gridWidth int, st Styles) string {
	var rule strings.Builder
	rule.WriteString(strings.Repeat(" ", weekNumberWidth))
	for dayIndex := 0; dayIndex < 7; dayIndex++ {
		if dayIndex > 0 {
			rule.WriteString("┼")
		}
		rule.WriteString(strings.Repeat("─", cellWidth))
	}
	return truncate(st.GridLine.Render(rule.String()), gridWidth)
}

// renderMonthDateCell keeps date metadata separate from the event rows. When
// a week does not have enough preview rows, +N reports only the hidden events;
// the visible events remain useful instead of spending their last row on an
// overflow label.
func renderMonthDateCell(day, focus time.Time, today, focusKey string, byDay map[string][]model.Occurrence, previewRows, cellWidth int, loc *time.Location, st Styles) string {
	key := day.Format("2006-01-02")
	num := fmt.Sprintf("%2d", day.Day())
	style := st.DayHeader
	switch {
	case key == today:
		style = st.TodayHeader
	case day.Month() != focus.Month():
		style = st.Dim
	}

	hidden := len(byDay[key]) - previewRows
	badge := ""
	if hidden > 0 {
		badge = fmt.Sprintf("+%d", hidden)
	}
	content := num
	if badge != "" {
		content += " " + badge
	}
	if key == focusKey {
		return truncate(focusedCellStyle(st, key == today).Render(pad(content, cellWidth)), cellWidth)
	}

	cell := style.Render(num)
	if badge != "" {
		cell += " " + st.Dim.Render(badge)
	}
	return truncate(pad(cell, cellWidth), cellWidth)
}

func renderMonthEventCell(day, focus time.Time, byDay map[string][]model.Occurrence, eventIndex, cellWidth int, loc *time.Location, st Styles) string {
	events := byDay[day.Format("2006-01-02")]
	if eventIndex < 0 || eventIndex >= len(events) {
		return strings.Repeat(" ", cellWidth)
	}
	o := events[eventIndex]
	summary := o.Summary
	if summary == "" {
		summary = "(no title)"
	}

	prefix := "● "
	if !o.AllDay {
		start := o.Start.In(loc)
		if start.Format("2006-01-02") == day.Format("2006-01-02") {
			prefix = start.Format("15:04") + " "
		} else {
			prefix = "↳ "
		}
	}
	label := truncatePlain(prefix+summary, cellWidth)

	style := st.Calendar(o.CalendarKey())
	if day.Month() != focus.Month() {
		style = st.Dim
	}
	return pad(style.Render(label), cellWidth)
}

func renderMonthCell(day, focus time.Time, today, focusKey string, byDay map[string][]model.Occurrence, cellWidth int, loc *time.Location, st Styles) string {
	key := day.Format("2006-01-02")
	num := fmt.Sprintf("%2d", day.Day())

	style := st.Base
	switch {
	case key == today:
		style = st.TodayHeader
	case day.Month() != focus.Month():
		style = st.Dim // spill days from the neighbouring months
	}

	// The day number and one separating space are always reserved; the rest
	// of the cell is for event markers.
	markerRoom := cellWidth - 3
	if markerRoom < 0 {
		markerRoom = 0
	}
	events := byDay[key]
	shown, overflow := monthMarkerBudget(len(events), markerRoom)

	if key == focusKey {
		// Full-cell inversion: build the plain content first (no per-marker
		// colour -- the whole cell is about to be one solid, readable
		// block), pad it to the entire cell width, then wrap all of it,
		// padding included, in the focused-cell style. Wrapping after
		// padding is what makes the fill reach the cell's edges instead of
		// trailing off around the visible glyphs.
		content := num + " " + plainMonthMarkers(shown, overflow)
		return truncate(focusedCellStyle(st, key == today).Render(pad(content, cellWidth)), cellWidth)
	}

	var markers strings.Builder
	for i := 0; i < shown; i++ {
		markers.WriteString(st.Calendar(events[i].CalendarKey()).Render("●"))
	}
	if overflow > 0 {
		markers.WriteString(st.Dim.Render(fmt.Sprintf("+%d", overflow)))
	}

	cell := style.Render(num) + " " + markers.String()
	return truncate(pad(cell, cellWidth), cellWidth)
}

// focusedCellStyle is the style for a focused cell in the month grid or
// week header: SelectedCell's full inversion (foreground + background),
// with its foreground swapped to the accent colour when the cell is also
// today. That swap is what keeps "this is also today" legible once focus
// moves onto it -- SelectedCell's own foreground alone is not guaranteed to
// be the accent colour; a theme is free to choose SelectedFg independently
// of Accent, and relying on them coinciding would silently lose the "today"
// fact for any theme that makes a different choice.
func focusedCellStyle(st Styles, isToday bool) lipgloss.Style {
	style := st.SelectedCell
	if isToday {
		style = style.Foreground(st.TodayHeader.GetForeground())
	}
	return style
}

// monthMarkerBudget splits a day's event count into how many dot markers fit
// and how many are left over for a "+N" overflow badge, given the room
// available for markers in a cell.
func monthMarkerBudget(n, markerRoom int) (shown, overflow int) {
	if n == 0 || markerRoom == 0 {
		return 0, 0
	}
	if n <= markerRoom {
		return n, 0
	}
	// Leave room for the "+N" badge itself.
	shown = markerRoom - 2
	if shown < 0 {
		shown = 0
	}
	return shown, n - shown
}

// plainMonthMarkers renders the same marker budget as the coloured path, but
// as plain text -- used for the focused cell, whose whole width (dots
// included) is styled by the caller as a single run.
func plainMonthMarkers(shown, overflow int) string {
	s := strings.Repeat("●", shown)
	if overflow > 0 {
		s += fmt.Sprintf("+%d", overflow)
	}
	return s
}

// groupByDay buckets occurrences by calendar day key, preserving order.
func groupByDay(occs []model.Occurrence, loc *time.Location) map[string][]model.Occurrence {
	byDay := map[string][]model.Occurrence{}
	for _, o := range occs {
		// A multi-day event appears on each day it covers.
		for _, key := range dayKeysCovered(o, loc) {
			byDay[key] = append(byDay[key], o)
		}
	}
	return byDay
}

// dayKeysCovered lists every calendar day an occurrence touches, capped so a
// pathological multi-year event cannot blow up the map.
func dayKeysCovered(o model.Occurrence, loc *time.Location) []string {
	const maxDays = 400
	start := o.Start.In(loc)
	end := o.End.In(loc)
	if o.AllDay {
		start = o.Start.UTC()
		end = o.End.UTC()
	}
	if !end.After(start) {
		return []string{start.Format("2006-01-02")}
	}
	// Walk calendar days rather than adding 24h to the start instant: an event
	// that ends earlier in the day than it began (any overnight event) would
	// otherwise never reach its final day.
	zone := start.Location()
	y, m, d := start.Date()
	day := time.Date(y, m, d, 0, 0, 0, 0, zone)
	var keys []string
	for day.Before(end) && len(keys) < maxDays {
		keys = append(keys, day.Format("2006-01-02"))
		day = day.AddDate(0, 0, 1)
	}
	return keys
}
