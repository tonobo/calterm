package tui

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"charm.land/lipgloss/v2"

	"github.com/tonobo/calterm/internal/model"
	"github.com/tonobo/calterm/internal/tui/theme"
)

func TestMonthGridStartsOnConfiguredWeekday(t *testing.T) {
	focus := time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC)

	mon := MonthGrid(focus, "monday")
	if got := mon[0][0].Weekday(); got != time.Monday {
		t.Errorf("monday grid starts on %v, want Monday", got)
	}
	sun := MonthGrid(focus, "sunday")
	if got := sun[0][0].Weekday(); got != time.Sunday {
		t.Errorf("sunday grid starts on %v, want Sunday", got)
	}
}

func TestMonthGridCoversTheWholeMonth(t *testing.T) {
	focus := time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC)
	grid := MonthGrid(focus, "monday")
	if len(grid) != 6 {
		t.Fatalf("grid has %d weeks, want 6", len(grid))
	}
	seen := map[int]bool{}
	for _, week := range grid {
		if len(week) != 7 {
			t.Fatalf("week has %d days, want 7", len(week))
		}
		for _, d := range week {
			if d.Month() == time.June {
				seen[d.Day()] = true
			}
		}
	}
	for day := 1; day <= 30; day++ {
		if !seen[day] {
			t.Errorf("June %d is missing from the grid", day)
		}
	}
}

// February 2026 starts on a Sunday and has 28 days — the case where a
// six-week grid is visibly larger than the month.
func TestMonthGridHandlesShortMonth(t *testing.T) {
	focus := time.Date(2026, 2, 10, 0, 0, 0, 0, time.UTC)
	grid := MonthGrid(focus, "monday")
	if len(grid) != 6 {
		t.Fatalf("grid has %d weeks, want 6", len(grid))
	}
	if grid[0][0].After(focus) {
		t.Error("the grid starts after the focused date")
	}
}

// The month grid gets a dedicated "Wk" column: a header cell, and one ISO
// week number per row.
func TestRenderMonthShowsWeekNumberColumn(t *testing.T) {
	focus := time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC)
	got := RenderMonth(nil, focus, 80, monthGridMinHeight, focus, time.UTC, NewStyles(true), "monday")
	if !strings.Contains(got, "Wk") {
		t.Errorf("month grid missing the Wk column header:\n%s", got)
	}
}

// Row week numbers are the ISO week of that row's Monday, including the
// year-boundary case: January 2026's first grid row starts 2025-12-29
// (Monday), which is ISO week 1 of 2026, not week 52 of 2025.
func TestRenderMonthWeekNumberColumnUsesRowsMondayAcrossYearBoundary(t *testing.T) {
	focus := time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC)
	got := RenderMonth(nil, focus, 80, monthGridMinHeight, focus, time.UTC, NewStyles(true), "monday")
	plain := stripANSI(got)
	lines := strings.Split(plain, "\n")
	// Line 0: weekday header (RenderMonth carries no title of its own any
	// more -- the status bar owns the period). Line 1: first week row,
	// which covers 2025-12-29 .. 2026-01-04 -- ISO week 1 of 2026 (its
	// Monday, 2025-12-29, is itself in ISO week 1, an unintuitive but
	// correct fact: the first days of January can belong to the PREVIOUS
	// year's week numbering, and vice versa).
	if len(lines) < 2 {
		t.Fatalf("expected at least 2 lines, got %d:\n%s", len(lines), plain)
	}
	// Check the leading Wk cell specifically (not Contains against the whole
	// line): the row also contains day-of-month cells formatted with the same
	// "%2d" scheme, so e.g. "1" (from 2026-01-01) would make a Contains check
	// pass even if the Wk cell itself printed the wrong week.
	if weekNumber := strings.TrimSpace(lines[1][:weekNumberWidth]); weekNumber != "1" {
		t.Errorf("first row's Wk cell = %q, want 1 (2025-12-29 is ISO week 1 of 2026): %q", weekNumber, lines[1])
	}
	// Row 4 covers 2026-01-26 .. 2026-02-01, whose Monday (2026-01-26) is
	// ISO week 5.
	if len(lines) < 6 {
		t.Fatalf("expected at least 6 lines, got %d:\n%s", len(lines), plain)
	}
	if weekNumber := strings.TrimSpace(lines[5][:weekNumberWidth]); weekNumber != "5" {
		t.Errorf("fifth row's Wk cell = %q, want 5: %q", weekNumber, lines[5])
	}
}

// week_start = "sunday" must not change the reported week number: the row
// is still keyed by its Monday's ISO week, not the Sunday it happens to
// start on. Same January 2026 grid, different week_start.
func TestRenderMonthWeekNumberColumnIgnoresWeekStart(t *testing.T) {
	focus := time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC)
	monday := stripANSI(RenderMonth(nil, focus, 80, monthGridMinHeight, focus, time.UTC, NewStyles(true), "monday"))
	sunday := stripANSI(RenderMonth(nil, focus, 80, monthGridMinHeight, focus, time.UTC, NewStyles(true), "sunday"))

	// Index 0: weekday header (RenderMonth carries no title of its own).
	mondayFirstRow := strings.Split(monday, "\n")[1]
	sundayFirstRow := strings.Split(sunday, "\n")[1]
	// Both grids' first row contains 2025-12-29 (Monday) and both must
	// report week 1, even though the sunday-start row begins on
	// 2025-12-28, whose OWN ISO week is 52 of 2025 -- the naive-week-number
	// bug this test exists to catch. Checking only the leading Wk cell (not
	// Contains against the whole line) matters here too: the row also
	// contains 2026-01-01, whose day cell renders "1" the same way a correct
	// Wk cell would.
	if weekNumber := strings.TrimSpace(mondayFirstRow[:weekNumberWidth]); weekNumber != "1" {
		t.Errorf("monday-start first row's Wk cell = %q, want 1: %q", weekNumber, mondayFirstRow)
	}
	if weekNumber := strings.TrimSpace(sundayFirstRow[:weekNumberWidth]); weekNumber != "1" {
		t.Errorf("sunday-start first row's Wk cell = %q, want 1 (the Monday's week), not 52: %q", weekNumber, sundayFirstRow)
	}
}

// December 2026 has a 53rd ISO week (2026-12-28 is a Monday in ISO week 53
// of 2026); the grid must show it, not clamp to 52 or wrap to week 1.
func TestRenderMonthWeekNumberColumnHandles53WeekYear(t *testing.T) {
	focus := time.Date(2026, 12, 15, 0, 0, 0, 0, time.UTC)
	got := stripANSI(RenderMonth(nil, focus, 80, monthGridMinHeight, focus, time.UTC, NewStyles(true), "monday"))
	lines := strings.Split(got, "\n")
	// Row 4 (index 5 overall -- no title line ahead of the weekday header)
	// covers 2026-12-28 .. 2027-01-03, week 53.
	if len(lines) < 6 {
		t.Fatalf("expected at least 6 lines, got %d:\n%s", len(lines), got)
	}
	if !strings.Contains(lines[5], "53") {
		t.Errorf("row covering 2026-12-28 should show week 53, got line: %q", lines[5])
	}
}

// isoWeekForDays finds the row's Monday and reports its ISO week, which is
// what makes the sunday-week_start case above correct: the naive approach
// of taking days[0].ISOWeek() would report the wrong year's week when the
// row starts on a Sunday spanning a year boundary.
func TestIsoWeekForDaysUsesRowsMonday(t *testing.T) {
	row := MonthGrid(time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC), "sunday")[0]
	if row[0].Weekday() != time.Sunday {
		t.Fatalf("test setup: row should start on Sunday, got %v", row[0].Weekday())
	}
	if got := isoWeekForDays(row); got != 1 {
		t.Errorf("isoWeekForDays(sunday-start row spanning the year boundary) = %d, want 1", got)
	}
}

// A single-day slice (day view) has no row to speak of; isoWeekForDays must
// fall back to that day's own ISO week rather than searching for a Monday
// that will never appear.
func TestIsoWeekForDaysSingleDayUsesItsOwnWeek(t *testing.T) {
	day := time.Date(2026, 6, 11, 0, 0, 0, 0, time.UTC) // a Thursday, week 24
	if got := isoWeekForDays([]time.Time{day}); got != 24 {
		t.Errorf("isoWeekForDays(single Thursday) = %d, want 24", got)
	}
}

// An empty slice must not panic on an unguarded days[0] -- this project has
// already paid for exactly this shape of "theoretically unreachable" bug
// once (Styles.Calendar dividing by a zero-value palette length, reached via
// the project's own zero-value test helper). No current caller passes an
// empty slice, but the function should not depend on caller discipline to
// stay safe.
func TestIsoWeekForDaysEmptySliceDoesNotPanic(t *testing.T) {
	if got := isoWeekForDays(nil); got != 0 {
		t.Errorf("isoWeekForDays(nil) = %d, want 0", got)
	}
}

// RenderMonth itself carries no period text; the status bar owns that context
// and the grid's own header is just the weekday row.
func TestRenderMonthShowsWeekdayHeader(t *testing.T) {
	focus := time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC)
	got := RenderMonth(agendaFixture(), focus, 80, 20, focus, time.UTC, NewStyles(true), "monday")
	if strings.Contains(got, "June 2026") {
		t.Errorf("RenderMonth should not print its own period (the status bar owns that):\n%s", got)
	}
	for _, day := range []string{"Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun"} {
		if !strings.Contains(got, day) {
			t.Errorf("weekday header %q missing:\n%s", day, got)
		}
	}
}

func TestRenderMonthMarksDaysWithEvents(t *testing.T) {
	focus := time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC)
	withEvents := RenderMonth(agendaFixture(), focus, 80, 20, focus, time.UTC, NewStyles(true), "monday")
	without := RenderMonth(nil, focus, 80, 20, focus, time.UTC, NewStyles(true), "monday")
	if withEvents == without {
		t.Error("days with events should render differently from an empty month")
	}
	if !strings.Contains(withEvents, "●") {
		t.Errorf("expected event markers in the grid:\n%s", withEvents)
	}
}

// Paging must actually change which month is drawn. RenderMonth no longer
// prints the month name itself (that's the status bar's job now), so this
// asserts on the grid actually differing, not on a period string that
// isn't there any more.
func TestRenderMonthFollowsTheFocusedDate(t *testing.T) {
	june := time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC)
	july := june.AddDate(0, 1, 0)
	a := RenderMonth(nil, june, 80, 20, june, time.UTC, NewStyles(true), "monday")
	b := RenderMonth(nil, july, 80, 20, june, time.UTC, NewStyles(true), "monday")
	if a == b {
		t.Errorf("the grid should follow the focused date:\n%s\n---\n%s", a, b)
	}
}

// The Wk column takes width away from the day cells that used to have it --
// sweep several terminal widths, including narrow ones, and assert no line
// ever exceeds the width given.
func TestRenderMonthWithWeekNumberColumnNeverExceedsWidth(t *testing.T) {
	focus := time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC)
	for _, width := range []int{12, 20, 24, 28, 35, 50, 80, 120} {
		got := RenderMonth(agendaFixture(), focus, width, 20, focus, time.UTC, NewStyles(true), "monday")
		for i, line := range strings.Split(got, "\n") {
			if w := lipgloss.Width(line); w > width {
				t.Errorf("width %d: line %d is %d cells: %q", width, i, w, line)
			}
		}
	}
}

func TestRenderMonthRespectsWidth(t *testing.T) {
	focus := time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC)
	for _, width := range []int{20, 24, 35, 50, 80, 120} {
		got := RenderMonth(agendaFixture(), focus, width, 20, focus, time.UTC, NewStyles(true), "monday")
		for i, line := range strings.Split(got, "\n") {
			if w := lipglossWidth(line); w > width {
				t.Errorf("width %d: line %d is %d cells: %q", width, i, w, line)
			}
		}
	}
}

func TestRenderMonthHighlightsToday(t *testing.T) {
	focus := time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC)
	a := RenderMonth(nil, focus, 80, 20, focus, time.UTC, NewStyles(true), "monday")
	b := RenderMonth(nil, focus, 80, 20, focus.AddDate(0, 0, 1), time.UTC, NewStyles(true), "monday")
	if a == b {
		t.Error("moving 'today' should change the rendering")
	}
}

func TestDayKeysCoveredIncludesFinalDayOfOvernightEvent(t *testing.T) {
	overnight := model.Occurrence{
		Start: time.Date(2026, 6, 8, 22, 0, 0, 0, time.UTC),
		End:   time.Date(2026, 6, 9, 2, 0, 0, 0, time.UTC),
	}
	got := dayKeysCovered(overnight, time.UTC)
	want := []string{"2026-06-08", "2026-06-09"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("dayKeysCovered(overnight) = %v, want %v", got, want)
	}

	// A one-day all-day event has an exclusive DTEND (start of the next day).
	// It must still yield exactly one day, guarding against over-correction.
	allDay := model.Occurrence{
		Start:  time.Date(2026, 6, 14, 0, 0, 0, 0, time.UTC),
		End:    time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC),
		AllDay: true,
	}
	gotAllDay := dayKeysCovered(allDay, time.UTC)
	wantAllDay := []string{"2026-06-14"}
	if !reflect.DeepEqual(gotAllDay, wantAllDay) {
		t.Errorf("dayKeysCovered(allDay) = %v, want %v", gotAllDay, wantAllDay)
	}
}

// RenderMonth clips its compact grid in very short terminals and fills taller
// allocations without ever overflowing them.
func TestRenderMonthRespectsHeight(t *testing.T) {
	focus := time.Date(2026, 6, 10, 0, 0, 0, 0, time.UTC)
	for _, height := range []int{1, 3, 5, 8, 40} {
		got := RenderMonth(nil, focus, 80, height, focus, time.UTC, NewStyles(true), "monday")
		if n := len(strings.Split(got, "\n")); n > height {
			t.Errorf("height %d: rendered %d lines, want at most %d", height, n, height)
		}
	}
}

func TestRenderMonthUsesExtraRowsForEventPreviews(t *testing.T) {
	focus := time.Date(2026, 6, 10, 0, 0, 0, 0, time.UTC)
	event := occ(
		"planning",
		"Planning meeting",
		time.Date(2026, 6, 10, 9, 30, 0, 0, time.UTC),
		time.Hour,
	)
	compact := stripANSI(RenderMonth([]model.Occurrence{event}, focus, 120, monthGridMinHeight, focus, time.UTC, NewStyles(true), "monday"))
	expanded := stripANSI(RenderMonth([]model.Occurrence{event}, focus, 120, 20, focus, time.UTC, NewStyles(true), "monday"))

	if strings.Contains(compact, "Planning") {
		t.Errorf("compact month should use markers, not event previews:\n%s", compact)
	}
	for _, want := range []string{"09:30", "Planning"} {
		if !strings.Contains(expanded, want) {
			t.Errorf("expanded month is missing %q from its event preview:\n%s", want, expanded)
		}
	}
}

func TestRenderMonthExpandedSeparatesDaysAndWeeks(t *testing.T) {
	focus := time.Date(2026, 6, 10, 0, 0, 0, 0, time.UTC)
	got := stripANSI(RenderMonth(agendaFixture(), focus, 120, 24, focus, time.UTC, NewStyles(true), "monday"))
	if !strings.Contains(got, "│") {
		t.Errorf("expanded month is missing day-column separators:\n%s", got)
	}
	if !strings.Contains(got, "┼") {
		t.Errorf("expanded month is missing inter-week separators:\n%s", got)
	}
}

func TestRenderMonthExpandedReportsHiddenEventsAtDate(t *testing.T) {
	focus := time.Date(2026, 6, 10, 0, 0, 0, 0, time.UTC)
	start := time.Date(2026, 6, 10, 9, 0, 0, 0, time.UTC)
	events := []model.Occurrence{
		occ("a", "One", start, time.Hour),
		occ("b", "Two", start.Add(time.Hour), time.Hour),
		occ("c", "Three", start.Add(2*time.Hour), time.Hour),
	}
	// At the expanded threshold, each week has exactly one event row.
	got := stripANSI(RenderMonth(events, focus, 120, monthExpandedMinHeight, focus, time.UTC, NewStyles(true), "monday"))
	if !strings.Contains(got, "+2") {
		t.Errorf("date header should report the two events hidden below its one preview row:\n%s", got)
	}
	if !strings.Contains(got, "09:00 One") {
		t.Errorf("the visible preview row should remain an event, not become an overflow row:\n%s", got)
	}
}

// The compact grid remains seven rows, while a taller allocation is consumed
// completely by event-preview rows.
func TestRenderMonthUsesAvailableHeight(t *testing.T) {
	focus := time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC)
	now := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	for _, height := range []int{monthGridMinHeight, 20, 40} {
		got := RenderMonth(nil, focus, 80, height, now, time.UTC, NewStyles(true), "monday")
		if n := len(strings.Split(got, "\n")); n != height {
			t.Errorf("height %d: RenderMonth emitted %d lines", height, n)
		}
	}
}

// bgEscape is the substring lipgloss emits for a 24-bit background colour
// (e.g. "\x1b[48;2;24;24;37m"). Its presence -- not its exact value -- is
// what a full-cell inversion test cares about.
const bgEscape = "48;2;"

// styledRunWithBackground returns the plain text carried by the one
// escape-delimited run in s that sets a background colour, so a test can
// assert on *what* got filled, not just that something did. Splitting on
// "\x1b[" and taking the text after the run's own closing "m" is needed
// because the SGR parameter list itself is full of digits ("38;2;205;...")
// that would otherwise defeat a naive Contains check for the target text.
func styledRunWithBackground(s string) (string, bool) {
	for _, run := range strings.Split(s, "\x1b[") {
		if !strings.Contains(run, bgEscape) {
			continue
		}
		if i := strings.Index(run, "m"); i != -1 {
			return run[i+1:], true
		}
	}
	return "", false
}

// The focused day's whole cell must be inverted (foreground AND background),
// and no other cell in the grid.
func TestRenderMonthCellFocusedIsInverted(t *testing.T) {
	focus := time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC) // two-digit day
	st := NewStyles(true)
	got := renderMonthCell(focus, focus, "2026-06-01", "2026-06-15", nil, 6, time.UTC, st)
	if !strings.Contains(got, bgEscape) {
		t.Errorf("focused cell missing a background fill: %q", got)
	}

	other := time.Date(2026, 6, 16, 0, 0, 0, 0, time.UTC)
	gotOther := renderMonthCell(other, focus, "2026-06-01", "2026-06-15", nil, 6, time.UTC, st)
	if strings.Contains(gotOther, bgEscape) {
		t.Errorf("a non-focused cell should not carry a background fill: %q", gotOther)
	}
}

// A background fill that only wraps the visible glyphs, leaving the padding
// unstyled, is the "ragged half-highlighted cell" the brief warns against.
// The focused cell must be a single, unbroken styled run covering its full
// width -- exactly one opening escape and one reset, not one run per glyph.
func TestRenderMonthCellFocusedFillsWholeCellUnbroken(t *testing.T) {
	focus := time.Date(2026, 6, 5, 0, 0, 0, 0, time.UTC) // single-digit day
	st := NewStyles(true)
	got := renderMonthCell(focus, focus, "2026-06-01", "2026-06-05", nil, 6, time.UTC, st)

	if n := strings.Count(got, "\x1b["); n != 2 {
		t.Errorf("expected the focused cell wrapped in exactly one style run (2 escape sequences), got %d: %q", n, got)
	}
	if w := lipgloss.Width(got); w != 6 {
		t.Errorf("focused cell width = %d, want the full cell width 6: %q", w, got)
	}
}

// Focused != today must remain visually distinct: an inverted block versus
// bold accent text, not the same styling.
func TestRenderMonthFocusedDayDistinctFromToday(t *testing.T) {
	focus := time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC)
	todayDay := time.Date(2026, 6, 20, 0, 0, 0, 0, time.UTC)
	st := NewStyles(true)
	focusCell := renderMonthCell(focus, focus, "2026-06-20", "2026-06-15", nil, 6, time.UTC, st)
	todayCell := renderMonthCell(todayDay, focus, "2026-06-20", "2026-06-15", nil, 6, time.UTC, st)
	if focusCell == todayCell {
		t.Errorf("focused-day cell and today's cell rendered identically: %q", focusCell)
	}
	if !strings.Contains(focusCell, bgEscape) {
		t.Errorf("focused (non-today) cell missing a background fill: %q", focusCell)
	}
	if strings.Contains(todayCell, bgEscape) {
		t.Errorf("today's cell (not focused) should not carry a background fill: %q", todayCell)
	}
}

// When the focused day IS today, both facts must still be legible. The
// default palette sets SelectedFg equal to Accent (see theme package tests),
// so the inverted block's own foreground already carries the "this is also
// today" fact -- there is no separate visual state to lose, and a
// focused-today cell renders identically to a focused (non-today) cell with
// the same day number.
func TestRenderMonthFocusedDayAndTodaySameDayBothLegible(t *testing.T) {
	day := time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC)
	st := NewStyles(true)
	sameDayCell := renderMonthCell(day, day, "2026-06-15", "2026-06-15", nil, 6, time.UTC, st)
	plainToday := renderMonthCell(day, day, "2026-06-15", "2026-06-01", nil, 6, time.UTC, st) // today, not focused
	plainFocus := renderMonthCell(day, day, "2026-06-01", "2026-06-15", nil, 6, time.UTC, st) // focused, not today
	neither := renderMonthCell(day, day, "2026-06-01", "2026-06-01", nil, 6, time.UTC, st)    // same day number, neither trait -- isolates styling from digit content

	if !strings.Contains(sameDayCell, bgEscape) {
		t.Errorf("background fill missing when focus == today (fact: focused): %q", sameDayCell)
	}
	if sameDayCell != plainFocus {
		t.Errorf("focus == today should render exactly like focus alone (today's colour is baked into Selected): got %q, want %q", sameDayCell, plainFocus)
	}
	if sameDayCell == plainToday {
		t.Errorf("focus marker lost when focus == today: %q", sameDayCell)
	}
	if sameDayCell == neither {
		t.Errorf("focus == today should still differ from a cell that is neither: %q", sameDayCell)
	}
}

// Cell width must be stable regardless of focus and regardless of a
// single-digit versus two-digit day -- the column-jitter defect this
// project has shipped before.
func TestRenderMonthCellWidthStableAcrossFocus(t *testing.T) {
	st := NewStyles(true)
	for _, cellWidth := range []int{4, 6, 10} {
		single := time.Date(2026, 6, 5, 0, 0, 0, 0, time.UTC)
		double := time.Date(2026, 6, 25, 0, 0, 0, 0, time.UTC)
		unfocusedSingle := renderMonthCell(single, single, "2026-06-01", "2026-06-01", nil, cellWidth, time.UTC, st)
		focusedSingle := renderMonthCell(single, single, "2026-06-01", "2026-06-05", nil, cellWidth, time.UTC, st)
		focusedDouble := renderMonthCell(double, double, "2026-06-01", "2026-06-25", nil, cellWidth, time.UTC, st)
		for _, cell := range []string{unfocusedSingle, focusedSingle, focusedDouble} {
			if w := lipgloss.Width(cell); w != cellWidth {
				t.Errorf("cellWidth %d: cell %q rendered at %d cells", cellWidth, cell, w)
			}
		}
	}
}

// Integration test through the real RenderMonth call site, not the
// renderMonthCell helper directly: the unit tests above call
// renderMonthCell with today and focusKey supplied by hand, so they would
// not catch the call site itself passing them in the wrong order (an easy
// mistake -- they are adjacent, same-typed string parameters). This uses a
// focus day that differs from today, so a today/focusKey swap at the call
// site would move the fill onto today's cell (5) instead of the focused one
// (20) and this test would catch it.
func TestRenderMonthMarksFocusedCellThroughFullRender(t *testing.T) {
	focus := time.Date(2026, 6, 20, 0, 0, 0, 0, time.UTC)
	now := time.Date(2026, 6, 5, 0, 0, 0, 0, time.UTC)
	got := RenderMonth(nil, focus, 80, 20, now, time.UTC, NewStyles(true), "monday")

	if n := strings.Count(got, bgEscape); n != 1 {
		t.Fatalf("expected exactly one background-filled cell in the full render, got %d:\n%s", n, got)
	}
	filled, ok := styledRunWithBackground(got)
	if !ok {
		t.Fatalf("could not locate the background-filled run:\n%s", got)
	}
	if trimmed := strings.TrimRight(filled, " "); trimmed != "20" {
		t.Errorf("the filled run's content = %q, want the focused day's number (20), not today's (5) -- a today/focusKey argument swap at the call site would produce this failure:\n%s", trimmed, got)
	}
	stripped := stripANSI(got)
	if n := len(strings.Split(got, "\n")); n != len(strings.Split(stripped, "\n")) {
		t.Errorf("inversion must not change the grid's line count")
	}
}

// fgColorCode renders a lone foreground colour through an empty style and
// extracts the "38;2;r;g;b"-shaped SGR parameter list lipgloss emits for it,
// so a test can check whether that exact colour appears in a larger,
// multi-attribute escape sequence (bold + fg + bg combined) without having
// to hand-construct the combined sequence itself.
func fgColorCode(hex string) string {
	rendered := lipgloss.NewStyle().Foreground(lipgloss.Color(hex)).Render("x")
	start := strings.Index(rendered, "[") + 1
	end := strings.Index(rendered, "m")
	return rendered[start:end]
}

// divergentPalette is a Palette whose SelectedFg is deliberately NOT the
// same colour as Accent -- unlike DefaultPalette, where the two happen to
// coincide. A test built only against DefaultPalette cannot tell "today
// renders with the accent colour" apart from "today renders with
// SelectedFg, which happens to equal the accent colour by coincidence";
// this palette makes the two cases produce different colours, the same way
// the project's non-UTC-location fixtures exist to catch bugs that a UTC
// fixture cannot.
func divergentPalette() theme.Palette {
	c := func(hex string) theme.Color { return theme.Color{Light: hex, Dark: hex} }
	return theme.Palette{
		Name:       "divergent",
		Fg:         c("#eeeeee"),
		Dim:        c("#888888"),
		Accent:     c("#ff8800"),
		Warn:       c("#ff0000"),
		GridLine:   c("#666666"),
		SelectedFg: c("#00ff00"), // deliberately distinct from Accent
		SelectedBg: c("#000044"),
		StatusFg:   c("#cccccc"),
		StatusBg:   c("#111111"),
		CalendarPalette: []theme.Color{
			c("#123456"),
		},
	}
}

// A focused day that is also today must show the accent colour, not
// SelectedFg, regardless of what a theme chooses for SelectedFg -- otherwise
// a theme whose SelectedFg differs from Accent would silently lose the
// "this is also today" fact the moment the user navigates onto it. Using
// divergentPalette (SelectedFg != Accent) is what makes this test capable of
// failing; against DefaultPalette the two colours coincide and a bug here
// would be invisible.
func TestRenderMonthCellFocusedTodayUsesAccentRegardlessOfPalette(t *testing.T) {
	st := theme.BuildStyles(divergentPalette(), true)
	accentCode := fgColorCode(divergentPalette().Accent.Resolve(true))
	selectedFgCode := fgColorCode(divergentPalette().SelectedFg.Resolve(true))
	if accentCode == selectedFgCode {
		t.Fatalf("test fixture invalid: Accent and SelectedFg resolve to the same SGR code (%s)", accentCode)
	}

	day := time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC)
	focusedToday := renderMonthCell(day, day, "2026-06-15", "2026-06-15", nil, 6, time.UTC, st)
	if !strings.Contains(focusedToday, bgEscape) {
		t.Errorf("focused+today cell missing its background fill (fact: focused): %q", focusedToday)
	}
	if !strings.Contains(focusedToday, accentCode) {
		t.Errorf("focused+today cell should use the accent colour (fact: today), got %q", focusedToday)
	}
	if strings.Contains(focusedToday, selectedFgCode) {
		t.Errorf("focused+today cell should not use plain SelectedFg once it diverges from Accent: %q", focusedToday)
	}

	otherDay := time.Date(2026, 6, 16, 0, 0, 0, 0, time.UTC)
	focusedOnly := renderMonthCell(otherDay, otherDay, "2026-06-01", "2026-06-16", nil, 6, time.UTC, st)
	if !strings.Contains(focusedOnly, selectedFgCode) {
		t.Errorf("focused (non-today) cell should use SelectedFg, got %q", focusedOnly)
	}
	if strings.Contains(focusedOnly, accentCode) {
		t.Errorf("focused (non-today) cell should not use the accent colour: %q", focusedOnly)
	}
}

// plainStyles carries no colour anywhere -- SelectedCell included -- except
// a Reverse attribute on SelectedCell, which is a style rule that needs no
// colour capability at all. This is NOT a claim about how a real terminal
// or lipgloss's own colour-profile downgrading handles a background colour
// when colour support is absent -- that has not been verified here. It only
// proves a narrower, renderer-level fact: renderMonthCell/renderWeekHeader
// apply SelectedCell uniformly across the whole cell regardless of what
// attributes that style happens to carry, so the focused cell stays
// distinguishable even when SelectedCell has no colour in it at all.
// NewStyles(false) is deliberately NOT used here: it is the light theme,
// still full 24-bit colour, not monochrome.
func plainStyles() Styles {
	plain := lipgloss.NewStyle()
	return Styles{
		Base: plain, DayHeader: plain, TodayHeader: plain, Time: plain,
		Summary: plain, Selected: plain, SelectedCell: lipgloss.NewStyle().Reverse(true), Dim: plain, StatusBar: plain,
		Error: plain, GridLine: plain,
	}
}

// Even with every colour stripped away, the focused cell must still be
// distinguishable from an unfocused one: renderMonthCell applies
// st.SelectedCell across the whole cell regardless of what that style is
// made of, so a colour-free SelectedCell (plainStyles' Reverse-only rule)
// still produces a visibly different cell. This proves the renderer's own
// contract, not any claim about real-terminal colour degrading.
func TestRenderMonthCellFocusedVisibleWithoutColour(t *testing.T) {
	focus := time.Date(2026, 6, 20, 0, 0, 0, 0, time.UTC)
	st := plainStyles()
	focusedCell := renderMonthCell(focus, focus, "2026-06-01", "2026-06-20", nil, 6, time.UTC, st)
	other := time.Date(2026, 6, 21, 0, 0, 0, 0, time.UTC)
	otherCell := renderMonthCell(other, focus, "2026-06-01", "2026-06-20", nil, 6, time.UTC, st)

	if focusedCell == otherCell {
		t.Errorf("focused cell indistinguishable from unfocused with no colour at all: %q", focusedCell)
	}
	if !strings.Contains(focusedCell, "\x1b[7m") {
		t.Errorf("focused cell missing the reverse-video attribute: %q", focusedCell)
	}
	if strings.Contains(otherCell, "\x1b[") {
		t.Errorf("unfocused cell should carry no escape codes at all with plainStyles: %q", otherCell)
	}
}
