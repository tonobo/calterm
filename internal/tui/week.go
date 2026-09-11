package tui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"charm.land/lipgloss/v2"

	"github.com/tonobo/calterm/internal/model"
)

// NarrowWidth is the terminal width below which a seven-day week is
// unreadable and collapses to a single day. It is checked against the
// width RenderWeek itself receives, which -- since app.go's bodyLeftPad
// left-padding sweep -- is bodyLeftPad columns narrower than the actual
// terminal width (render() hands every view bodyWidth = width -
// bodyLeftPad, not the raw width). Subtracting bodyLeftPad here keeps the
// "terminal width" promise in this comment true through the app, rather
// than silently collapsing a 90-column terminal one column earlier than
// it used to, purely as a side effect of the padding sweep having nothing
// to do with how much room a seven-day grid actually needs.
const NarrowWidth = 90 - bodyLeftPad

// Default visible hours, widened as needed to cover the day's events.
const (
	defaultStartHour = 7
	defaultEndHour   = 21
	gutterWidth      = 6 // "09:00 "
)

const weekColumnSeparator = "│"

// AssignColumns lays overlapping occurrences into side-by-side columns using a
// greedy sweep: each occurrence takes the lowest-numbered column not occupied
// by something it overlaps. Columns are reused once an event has ended.
func AssignColumns(occs []model.Occurrence) map[int]int {
	type item struct {
		idx int
		o   model.Occurrence
	}
	items := make([]item, 0, len(occs))
	for i, o := range occs {
		items = append(items, item{i, o})
	}
	sort.SliceStable(items, func(a, b int) bool {
		return items[a].o.Start.Before(items[b].o.Start)
	})

	cols := map[int]int{}
	var columnEnds []time.Time // last end time placed in each column

	for _, it := range items {
		placed := false
		for c, end := range columnEnds {
			if !it.o.Start.Before(end) { // this column is free again
				columnEnds[c] = it.o.End
				cols[it.idx] = c
				placed = true
				break
			}
		}
		if !placed {
			columnEnds = append(columnEnds, it.o.End)
			cols[it.idx] = len(columnEnds) - 1
		}
	}
	return cols
}

// RenderWeek draws an hour-gridded timeline with a pinned all-day row.
func RenderWeek(occs []model.Occurrence, focus time.Time, width, height int, now time.Time, loc *time.Location, st Styles, dayOnly bool, weekStart string) string {
	if loc == nil {
		loc = time.UTC
	}
	if width <= 0 || height <= 0 {
		return ""
	}
	days := weekDays(focus, weekStart)
	if dayOnly || width < NarrowWidth {
		days = []time.Time{time.Date(focus.Year(), focus.Month(), focus.Day(), 0, 0, 0, 0, loc)}
	}

	separatorWidth := len(days) - 1
	colWidth := (width - gutterWidth - separatorWidth) / len(days)
	if colWidth < 5 {
		colWidth = 5
	}
	totalWidth := gutterWidth + colWidth*len(days) + separatorWidth

	byDay := groupByDay(occs, loc)
	today := dayKey(now, loc)
	focusKey := dayKey(focus, loc)

	var lines []string
	lines = append(lines, renderWeekHeader(days, today, focusKey, colWidth, st))
	lines = append(lines, renderAllDayRow(days, byDay, colWidth, st))

	startHour, endHour := visibleHours(days, byDay, loc)
	// Two header lines are already used; fit the rest of the grid in what
	// remains rather than overflowing the terminal.
	available := height - len(lines)
	if available < 1 {
		available = 1
	}
	if endHour-startHour > available {
		endHour = startHour + available
	}

	slots := weekTimeSlots(startHour, endHour, available)
	for _, slot := range slots {
		lines = append(lines, renderTimeSlotRow(slot, slots, days, byDay, colWidth, loc, st))
	}

	for i, line := range lines {
		lines[i] = truncate(line, totalWidth)
	}
	if len(lines) > height {
		lines = lines[:height]
	}
	return strings.Join(lines, "\n")
}

// weekTimeSlot is one adaptive vertical slice of the timeline. Tall terminals
// get multiple slices per hour, providing rows for event metadata; short ones
// retain the compact one-row-per-hour layout.
type weekTimeSlot struct {
	Index       int
	Hour        int
	Subrow      int
	StartMinute int
	EndMinute   int
}

func weekTimeSlots(startHour, endHour, rows int) []weekTimeSlot {
	hours := endHour - startHour
	if hours <= 0 || rows <= 0 {
		return nil
	}
	if rows < hours {
		hours = rows
		endHour = startHour + hours
	}
	heights := distributeWeekRows(rows, hours)
	slots := make([]weekTimeSlot, 0, rows)
	for hourIndex, rowCount := range heights {
		hour := startHour + hourIndex
		for subrow := 0; subrow < rowCount; subrow++ {
			slots = append(slots, weekTimeSlot{
				Index:       len(slots),
				Hour:        hour,
				Subrow:      subrow,
				StartMinute: hour*60 + subrow*60/rowCount,
				EndMinute:   hour*60 + (subrow+1)*60/rowCount,
			})
		}
	}
	return slots
}

// distributeWeekRows spreads remainder rows through the day rather than
// making only the first hours visibly taller.
func distributeWeekRows(rows, hours int) []int {
	if rows <= 0 || hours <= 0 {
		return nil
	}
	base, remainder := rows/hours, rows%hours
	heights := make([]int, hours)
	for i := range heights {
		heights[i] = base
		if (i+1)*remainder/hours > i*remainder/hours {
			heights[i]++
		}
	}
	return heights
}

// isoWeekForDays returns the ISO week number for a slice of consecutive
// calendar days. ISO weeks are Monday-anchored, so for a full seven-day row
// it reports the row's Monday's week -- not days[0]'s -- which is what keeps
// the result correct when week_start = "sunday" makes days[0] a Sunday that
// can belong to a different ISO week (and even a different year) than the
// rest of the row. For anything shorter than a full week (the day view's
// single-day slice), there is no Monday to find, so it falls back to that
// day's own ISO week. An empty slice reports week 0 rather than indexing
// days[0] on faith: every current caller passes a non-empty slice, but nothing
// enforces that, and this project has already paid for exactly this shape of
// "theoretically unreachable" panic once (Styles.Calendar dividing by a
// zero-value palette length via the project's own zero-value test helper).
func isoWeekForDays(days []time.Time) int {
	if len(days) == 0 {
		return 0
	}
	for _, d := range days {
		if d.Weekday() == time.Monday {
			_, w := d.ISOWeek()
			return w
		}
	}
	_, w := days[0].ISOWeek()
	return w
}

func weekDays(focus time.Time, weekStart string) []time.Time {
	startWeekday := time.Monday
	if weekStart == "sunday" {
		startWeekday = time.Sunday
	}
	offset := (int(focus.Weekday()) - int(startWeekday) + 7) % 7
	first := time.Date(focus.Year(), focus.Month(), focus.Day(), 0, 0, 0, 0, focus.Location()).AddDate(0, 0, -offset)

	days := make([]time.Time, 7)
	for i := range days {
		days[i] = first.AddDate(0, 0, i)
	}
	return days
}

func renderWeekHeader(days []time.Time, today, focusKey string, colWidth int, st Styles) string {
	// The full seven-day grid has a gutter to hold the week number; a
	// single-day slice (day view, and the narrow-width collapse) has no
	// column for it, so it's appended to the day's own label instead. These
	// two cases are the only ones any caller produces, and isGrid drives
	// both branches below so that fact is encoded once, not as two
	// independently-chosen literals that merely happen to be complementary.
	isGrid := len(days) == 7

	var b strings.Builder
	if isGrid {
		week := isoWeekForDays(days)
		b.WriteString(truncate(st.Dim.Render(pad(fmt.Sprintf("Wk %2d", week), gutterWidth)), gutterWidth))
	} else {
		b.WriteString(strings.Repeat(" ", gutterWidth))
	}
	// labelWidth reserves at least one gap cell within colWidth so two
	// unfocused columns never touch: at colWidth's own clamped minimum (5),
	// a two-digit day label ("Mon 25", 6 cells) used to fill the column
	// entirely, leaving no separating space at all. The focused cell below
	// still pads to the full colWidth -- its whole-cell background fill is
	// the separator there, not a blank gap cell.
	labelWidth := colWidth - 1
	if labelWidth < 1 {
		labelWidth = 1
	}
	for dayIndex, d := range days {
		if dayIndex > 0 {
			b.WriteString(st.GridLine.Render(weekColumnSeparator))
		}
		key := d.Format("2006-01-02")
		label := d.Format("Mon 2")
		if !isGrid {
			_, week := d.ISOWeek()
			label = fmt.Sprintf("%s · Wk %d", label, week)
		}
		label = truncatePlain(label, labelWidth)

		if key == focusKey {
			// Full-cell inversion: pad the label to the column's entire
			// width before styling, so the highlight covers the padding
			// too, not just the visible glyph -- otherwise a background
			// fill shorter than the column reads as a ragged
			// half-highlighted cell instead of a solid block. focusedCellStyle
			// (month.go) also swaps in the accent foreground when the column
			// is also today, so that fact survives regardless of a theme's
			// SelectedFg choice.
			b.WriteString(truncate(focusedCellStyle(st, key == today).Render(pad(label, colWidth)), colWidth))
			continue
		}

		style := st.DayHeader
		if key == today {
			style = st.TodayHeader
		}
		b.WriteString(pad(style.Render(label), colWidth))
	}
	return b.String()
}

func renderAllDayRow(days []time.Time, byDay map[string][]model.Occurrence, colWidth int, st Styles) string {
	var b strings.Builder
	b.WriteString(pad(st.Dim.Render("all"), gutterWidth))
	for dayIndex, d := range days {
		if dayIndex > 0 {
			b.WriteString(st.GridLine.Render(weekColumnSeparator))
		}
		var allDay []model.Occurrence
		for _, o := range byDay[d.Format("2006-01-02")] {
			if o.AllDay {
				allDay = append(allDay, o)
			}
		}
		if len(allDay) == 0 {
			b.WriteString(strings.Repeat(" ", colWidth))
			continue
		}
		calendarStyle := st.Calendar(allDay[0].CalendarKey())
		marker := calendarStyle.Render("● ")
		var rendered string
		if len(allDay) > 1 {
			// Mirror month.go's "+N" overflow badge rather than silently
			// dropping the other same-day all-day events.
			badge := fmt.Sprintf(" +%d", len(allDay)-1)
			room := colWidth - lipgloss.Width(marker) - lipgloss.Width(badge)
			if room < 0 {
				room = 0
			}
			label := truncatePlain(participationSummary(allDay[0]), room)
			rendered = marker + calendarStyle.Render(label) + st.Dim.Render(badge)
		} else {
			label := truncatePlain(participationSummary(allDay[0]), colWidth-lipgloss.Width(marker))
			rendered = marker + calendarStyle.Render(label)
		}
		// truncate as well as pad: the "+N" badge is itself unbounded (a day
		// with 1000 all-day events in a narrow column renders " +999"), so
		// padding alone does not give the exactly-colWidth guarantee that
		// renderTimeSlotCell has, and one oversized cell shifts every later day
		// column out of alignment.
		b.WriteString(truncate(pad(rendered, colWidth), colWidth))
	}
	return b.String()
}

// visibleHours widens the default window to cover whatever the days actually
// contain, so an 06:00 flight or a 23:00 call is never hidden.
func visibleHours(days []time.Time, byDay map[string][]model.Occurrence, loc *time.Location) (int, int) {
	start, end := defaultStartHour, defaultEndHour
	for _, d := range days {
		for _, o := range byDay[d.Format("2006-01-02")] {
			if o.AllDay {
				continue
			}
			if h := o.Start.In(loc).Hour(); h < start {
				start = h
			}
			endHour := o.End.In(loc).Hour()
			if o.End.In(loc).Minute() > 0 {
				endHour++
			}
			if endHour > end {
				end = endHour
			}
		}
	}
	if start < 0 {
		start = 0
	}
	if end > 24 {
		end = 24
	}
	if end <= start {
		end = start + 1
	}
	return start, end
}

func renderTimeSlotRow(slot weekTimeSlot, slots []weekTimeSlot, days []time.Time, byDay map[string][]model.Occurrence, colWidth int, loc *time.Location, st Styles) string {
	var b strings.Builder
	if slot.Subrow == 0 {
		b.WriteString(pad(st.Time.Render(fmt.Sprintf("%02d:00", slot.Hour)), gutterWidth))
	} else {
		b.WriteString(strings.Repeat(" ", gutterWidth))
	}

	for dayIndex, d := range days {
		if dayIndex > 0 {
			b.WriteString(st.GridLine.Render(weekColumnSeparator))
		}
		b.WriteString(renderTimeSlotCell(slot, slots, d, byDay[d.Format("2006-01-02")], colWidth, loc, st))
	}
	return b.String()
}

// renderTimeSlotCell draws one day's slice of one adaptive time row. It always returns
// exactly colWidth display cells: every lane is capped so the lanes drawn
// can never exceed colWidth in total, and the result is forced to colWidth
// with pad/truncate before it is handed back. That makes it structurally
// impossible for one day's content to bleed into the next day's column,
// regardless of how the lane arithmetic above it comes out.
func renderTimeSlotCell(slot weekTimeSlot, slots []weekTimeSlot, d time.Time, dayOccs []model.Occurrence, colWidth int, loc *time.Location, st Styles) string {
	timed := timedEvents(dayOccs)
	cols := AssignColumns(timed)
	// Split the day only while events actually overlap in this slot. Using the
	// maximum lane count of the entire day made a lone morning meeting half as
	// wide merely because two unrelated afternoon meetings overlapped.
	lanes := 1
	for i, o := range timed {
		if coversTimeSlot(o, d, slot, loc) && cols[i]+1 > lanes {
			lanes = cols[i] + 1
		}
	}

	laneWidth := colWidth / lanes
	if laneWidth < 1 {
		laneWidth = 1
	}
	// More lanes may exist than colWidth has room for at this laneWidth;
	// only draw as many as actually fit.
	drawn := lanes
	if drawn*laneWidth > colWidth {
		drawn = colWidth / laneWidth
	}
	if drawn < 1 {
		drawn = 1
	}

	overflow := lanes > drawn

	cell := make([]string, drawn)
	for i, o := range timed {
		if !coversTimeSlot(o, d, slot, loc) {
			continue
		}
		lane := cols[i]
		if lane >= drawn {
			continue
		}
		label := ""
		style := st.Calendar(o.CalendarKey())
		startRow := eventStartSlotIndex(o, d, slots, loc)
		switch slot.Index - startRow {
		case 0:
			label = participationSummary(o)
		case 1:
			label = weekEventMetadata(o, loc)
			style = st.Dim
		}
		labelWidth := laneWidth - 1
		if labelWidth > 0 {
			label = truncatePlain(label, labelWidth)
		} else {
			label = ""
		}
		cell[lane] = st.Calendar(o.CalendarKey()).Render("▏") + style.Render(pad(label, labelWidth))
	}

	var b strings.Builder
	for i, c := range cell {
		// Signal the lanes that didn't fit rather than dropping them
		// silently: overwrite the last drawn lane with an overflow glyph.
		if overflow && i == drawn-1 {
			b.WriteString(truncate(st.Dim.Render("…"), laneWidth))
			continue
		}
		if c == "" {
			b.WriteString(strings.Repeat(" ", laneWidth))
			continue
		}
		b.WriteString(truncate(c, laneWidth))
	}

	// Force exactly colWidth display cells, no matter what the lane
	// arithmetic above produced, so a later day's column can never shift.
	return truncate(pad(b.String(), colWidth), colWidth)
}

func eventStartSlotIndex(o model.Occurrence, day time.Time, slots []weekTimeSlot, loc *time.Location) int {
	start := o.Start.In(loc)
	if start.Year() != day.Year() || start.YearDay() < day.YearDay() {
		return 0 // an overnight event carried into this day
	}
	minute := start.Hour()*60 + start.Minute()
	for _, slot := range slots {
		if minute >= slot.StartMinute && minute < slot.EndMinute {
			return slot.Index
		}
	}
	if len(slots) > 0 && minute < slots[0].StartMinute {
		return 0
	}
	return -1
}

func weekEventMetadata(o model.Occurrence, loc *time.Location) string {
	start, end := o.Start.In(loc), o.End.In(loc)
	label := start.Format("15:04") + "–" + end.Format("15:04")
	if location := strings.Join(strings.Fields(o.Location), " "); location != "" {
		label += " · " + location
	}
	return label
}

func timedEvents(occs []model.Occurrence) []model.Occurrence {
	var out []model.Occurrence
	for _, o := range occs {
		if !o.AllDay {
			out = append(out, o)
		}
	}
	return out
}

func laneCount(cols map[int]int) int {
	max := 0
	for _, c := range cols {
		if c+1 > max {
			max = c + 1
		}
	}
	if max == 0 {
		return 1
	}
	return max
}

func coversTimeSlot(o model.Occurrence, day time.Time, slot weekTimeSlot, loc *time.Location) bool {
	dayStart := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, loc)
	slotStart := dayStart.Add(time.Duration(slot.StartMinute) * time.Minute)
	slotEnd := dayStart.Add(time.Duration(slot.EndMinute) * time.Minute)
	return o.Start.Before(slotEnd) && o.End.After(slotStart)
}

func sameDay(a, b time.Time) bool {
	ay, am, ad := a.Date()
	by, bm, bd := b.Date()
	return ay == by && am == bm && ad == bd
}
