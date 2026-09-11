package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/tonobo/calterm/internal/model"
	"github.com/tonobo/calterm/internal/tui/theme"
)

func weekFixture() []model.Occurrence {
	d := func(day, hour int) time.Time {
		return time.Date(2026, 6, day, hour, 0, 0, 0, time.UTC)
	}
	return []model.Occurrence{
		occ("a", "Standup", d(10, 9), 30*time.Minute),
		occ("b", "Overlap one", d(10, 14), time.Hour),
		occ("c", "Overlap two", d(10, 14), time.Hour),
		{UID: "e", Summary: "Conference", AllDay: true,
			Start: time.Date(2026, 6, 10, 0, 0, 0, 0, time.UTC),
			End:   time.Date(2026, 6, 11, 0, 0, 0, 0, time.UTC), CalendarID: "work"},
	}
}

func TestAssignColumnsSeparatesOverlaps(t *testing.T) {
	occs := []model.Occurrence{
		occ("a", "A", time.Date(2026, 6, 10, 9, 0, 0, 0, time.UTC), time.Hour),
		occ("b", "B", time.Date(2026, 6, 10, 9, 30, 0, 0, time.UTC), time.Hour),
		occ("c", "C", time.Date(2026, 6, 10, 11, 0, 0, 0, time.UTC), time.Hour),
	}
	cols := AssignColumns(occs)
	if cols[0] == cols[1] {
		t.Errorf("overlapping events share column %d", cols[0])
	}
	if cols[2] != 0 {
		t.Errorf("a non-overlapping event got column %d, want 0 (columns should be reused)", cols[2])
	}
}

func TestAssignColumnsThreeWayOverlap(t *testing.T) {
	base := time.Date(2026, 6, 10, 9, 0, 0, 0, time.UTC)
	occs := []model.Occurrence{
		occ("a", "A", base, 2*time.Hour),
		occ("b", "B", base.Add(15*time.Minute), time.Hour),
		occ("c", "C", base.Add(30*time.Minute), time.Hour),
	}
	cols := AssignColumns(occs)
	seen := map[int]bool{cols[0]: true, cols[1]: true, cols[2]: true}
	if len(seen) != 3 {
		t.Errorf("three mutually overlapping events used %d columns, want 3", len(seen))
	}
}

// The week grid's gutter carries the ISO week number, in place of the blank
// space it used to be, for the full seven-day header.
func TestRenderWeekHeaderGutterShowsISOWeek(t *testing.T) {
	days := weekDays(time.Date(2026, 6, 10, 0, 0, 0, 0, time.UTC), "monday") // week 24
	got := renderWeekHeader(days, "2026-06-01", "", 12, NewStyles(true))
	plain := stripANSI(got)
	if !strings.Contains(plain, "Wk 24") {
		t.Errorf("week header gutter missing Wk 24: %q", plain)
	}
}

// week_start = "sunday" must not change the reported week: the gutter is
// still keyed by the row's Monday, not the Sunday the row happens to start
// on. This crosses the 2025/2026 year boundary, where a Sunday-start row's
// own first day (2025-12-28) is ISO week 52 of 2025, but the row's Monday
// (2025-12-29) is week 1 of 2026.
func TestRenderWeekHeaderGutterUsesMondayWithSundayWeekStart(t *testing.T) {
	focus := time.Date(2025, 12, 30, 0, 0, 0, 0, time.UTC)
	days := weekDays(focus, "sunday")
	if days[0].Weekday() != time.Sunday {
		t.Fatalf("test setup: row should start on Sunday, got %v", days[0].Weekday())
	}
	got := stripANSI(renderWeekHeader(days, "2025-12-01", "", 12, NewStyles(true)))
	if !strings.Contains(got, "Wk  1") {
		t.Errorf("sunday-start week header should report week 1 (the Monday's week), got: %q", got)
	}
}

// The gutter's Wk addition must not change the header's total width -- it
// fills the gutter's existing space, not new space.
func TestRenderWeekHeaderGutterWidthUnchanged(t *testing.T) {
	days := weekDays(time.Date(2026, 6, 10, 0, 0, 0, 0, time.UTC), "monday")
	for _, colWidth := range []int{6, 8, 12, 20} {
		got := renderWeekHeader(days, "2026-06-01", "", colWidth, NewStyles(true))
		want := gutterWidth + colWidth*7 + 6
		if w := lipgloss.Width(got); w != want {
			t.Errorf("colWidth %d: header width = %d, want %d: %q", colWidth, w, want, got)
		}
	}
}

// Day view (a single-day slice, as used by RenderWeek(dayOnly=true) and the
// narrow-width collapse) has no grid to hold a column, so the week number is
// appended to the date label instead.
func TestRenderWeekHeaderSingleDayAppendsWeekNumber(t *testing.T) {
	days := []time.Time{time.Date(2026, 8, 27, 0, 0, 0, 0, time.UTC)} // Thu, week 35
	got := stripANSI(renderWeekHeader(days, "2026-08-01", "", 30, NewStyles(true)))
	if !strings.Contains(got, "Thu 27 · Wk 35") {
		t.Errorf("day view header should append the week number, got: %q", got)
	}
}

// Through the real RenderWeek(dayOnly=true) call site: the day view's date
// line carries "· Wk <n>".
func TestRenderDayOnlyAppendsWeekNumber(t *testing.T) {
	focus := time.Date(2026, 8, 27, 0, 0, 0, 0, time.UTC)
	got := stripANSI(RenderWeek(nil, focus, 120, 30, focus, time.UTC, NewStyles(true), true, "monday"))
	if !strings.Contains(got, "· Wk 35") {
		t.Errorf("day view should show '· Wk 35':\n%s", got)
	}
}

func TestRenderWeekShowsDayHeadersAndAllDayRow(t *testing.T) {
	focus := time.Date(2026, 6, 10, 0, 0, 0, 0, time.UTC)
	got := RenderWeek(weekFixture(), focus, 120, 30, focus, time.UTC, NewStyles(true), false, "monday")
	if !strings.Contains(got, "Mon") || !strings.Contains(got, "Sun") {
		t.Errorf("week header should span Mon–Sun:\n%s", got)
	}
	if !strings.Contains(got, "Conference") {
		t.Errorf("all-day event missing from the pinned row:\n%s", got)
	}
	if !strings.Contains(got, "09:") {
		t.Errorf("hour gutter missing:\n%s", got)
	}
}

func TestRenderWeekUsesExtraHeightForEventMetadata(t *testing.T) {
	focus := time.Date(2026, 6, 10, 0, 0, 0, 0, time.UTC)
	event := occ("a", "Planning", time.Date(2026, 6, 10, 9, 0, 0, 0, time.UTC), time.Hour)
	event.Location = "Room 2"

	compact := stripANSI(RenderWeek([]model.Occurrence{event}, focus, 120, 16, focus, time.UTC, NewStyles(true), true, "monday"))
	expanded := stripANSI(RenderWeek([]model.Occurrence{event}, focus, 120, 30, focus, time.UTC, NewStyles(true), true, "monday"))
	if strings.Contains(compact, "09:00–10:00") {
		t.Errorf("compact week should keep one row per hour:\n%s", compact)
	}
	for _, want := range []string{"Planning", "09:00–10:00", "Room 2"} {
		if !strings.Contains(expanded, want) {
			t.Errorf("expanded week is missing %q:\n%s", want, expanded)
		}
	}
	if lines := strings.Split(expanded, "\n"); len(lines) != 30 {
		t.Errorf("expanded week rendered %d lines, want 30", len(lines))
	}
}

func TestRenderWeekSeparatesDayColumns(t *testing.T) {
	focus := time.Date(2026, 6, 10, 0, 0, 0, 0, time.UTC)
	got := stripANSI(RenderWeek(weekFixture(), focus, 120, 30, focus, time.UTC, NewStyles(true), false, "monday"))
	if !strings.Contains(got, weekColumnSeparator) {
		t.Errorf("week view is missing vertical day separators:\n%s", got)
	}
}

// Below the narrow threshold, a seven-column week is unreadable, so it
// collapses to a single day.
func TestRenderWeekCollapsesWhenNarrow(t *testing.T) {
	focus := time.Date(2026, 6, 10, 0, 0, 0, 0, time.UTC)
	narrow := RenderWeek(weekFixture(), focus, 60, 30, focus, time.UTC, NewStyles(true), false, "monday")
	if strings.Contains(narrow, "Sun") {
		t.Errorf("a 60-column week should collapse to one day:\n%s", narrow)
	}
	wide := RenderWeek(weekFixture(), focus, 120, 30, focus, time.UTC, NewStyles(true), false, "monday")
	if !strings.Contains(wide, "Sun") {
		t.Errorf("a 120-column week should show the whole week:\n%s", wide)
	}
}

func TestRenderDayOnlyShowsOneDay(t *testing.T) {
	focus := time.Date(2026, 6, 10, 0, 0, 0, 0, time.UTC)
	got := RenderWeek(weekFixture(), focus, 120, 30, focus, time.UTC, NewStyles(true), true, "monday")
	if strings.Contains(got, "Sun") {
		t.Errorf("day view should show only the focused day:\n%s", got)
	}
	if !strings.Contains(got, "Standup") {
		t.Errorf("day view is missing the day's events:\n%s", got)
	}
}

// Paging must actually change which week is drawn.
func TestRenderWeekFollowsTheFocusedDate(t *testing.T) {
	thisWeek := time.Date(2026, 6, 10, 0, 0, 0, 0, time.UTC)
	nextWeek := thisWeek.AddDate(0, 0, 7)
	a := RenderWeek(weekFixture(), thisWeek, 120, 30, thisWeek, time.UTC, NewStyles(true), false, "monday")
	b := RenderWeek(weekFixture(), nextWeek, 120, 30, thisWeek, time.UTC, NewStyles(true), false, "monday")
	if a == b {
		t.Error("moving the focused date one week did not change the rendering")
	}
	if !strings.Contains(a, "Standup") {
		t.Errorf("the focused week should show its events:\n%s", a)
	}
	if strings.Contains(b, "Standup") {
		t.Errorf("next week should not show this week's events:\n%s", b)
	}
}

// The Wk column takes width away from the day columns that used to have it
// -- sweep several terminal widths, including narrow ones where the grid is
// already tight, and assert no line ever exceeds the width given.
func TestRenderWeekWithWeekNumberColumnNeverExceedsWidth(t *testing.T) {
	focus := time.Date(2026, 6, 10, 0, 0, 0, 0, time.UTC)
	for _, width := range []int{20, 30, 45, 60, 90, 91, 120, 200} {
		for _, dayOnly := range []bool{false, true} {
			got := RenderWeek(weekFixture(), focus, width, 30, focus, time.UTC, NewStyles(true), dayOnly, "monday")
			for i, line := range strings.Split(got, "\n") {
				if w := lipgloss.Width(line); w > width {
					t.Errorf("width %d dayOnly=%v: line %d is %d cells: %q", width, dayOnly, i, w, line)
				}
			}
		}
	}
}

func TestRenderWeekRespectsWidthAndHeight(t *testing.T) {
	focus := time.Date(2026, 6, 10, 0, 0, 0, 0, time.UTC)
	for _, size := range []struct{ w, h int }{{60, 20}, {100, 30}, {200, 50}, {40, 10}} {
		got := RenderWeek(weekFixture(), focus, size.w, size.h, focus, time.UTC, NewStyles(true), false, "monday")
		lines := strings.Split(got, "\n")
		if len(lines) > size.h {
			t.Errorf("size %dx%d produced %d lines", size.w, size.h, len(lines))
		}
		for i, line := range lines {
			if w := lipglossWidth(line); w > size.w {
				t.Errorf("size %dx%d: line %d is %d cells: %q", size.w, size.h, i, w, line)
			}
		}
	}
}

func TestRenderWeekOverlappingEventsBothVisible(t *testing.T) {
	focus := time.Date(2026, 6, 10, 0, 0, 0, 0, time.UTC)
	got := RenderWeek(weekFixture(), focus, 160, 40, focus, time.UTC, NewStyles(true), true, "monday")
	if !strings.Contains(got, "Overlap") {
		t.Errorf("overlapping events should still render:\n%s", got)
	}
}

// At colWidth's own minimum (5), a two-digit day label ("Mon 25", 6 cells)
// used to fill the column entirely, so two adjacent unfocused headers had no
// separating space at all. renderWeekHeader must reserve at least one gap
// cell per column regardless of how long the label is.
func TestRenderWeekHeaderReservesGapBetweenUnfocusedColumns(t *testing.T) {
	const colWidth = 5                                                       // RenderWeek's own clamped minimum
	days := weekDays(time.Date(2026, 6, 24, 0, 0, 0, 0, time.UTC), "monday") // Mon 22 - Sun 28, all two-digit
	got := []rune(stripANSI(renderWeekHeader(days, "", "", colWidth, NewStyles(true))))
	wantWidth := gutterWidth + colWidth*7 + 6
	if w := lipgloss.Width(string(got)); w != wantWidth {
		t.Fatalf("header width = %d, want %d: %q", w, wantWidth, string(got))
	}
	for i := range days {
		start := gutterWidth + i*(colWidth+1)
		col := string(got[start : start+colWidth])
		if !strings.HasSuffix(col, " ") {
			t.Errorf("column %d has no trailing gap at colWidth %d: %q", i, colWidth, col)
		}
	}
}

// stripANSI removes terminal escape sequences so plain-text width and content
// checks aren't confused by styling or OSC-8 hyperlinks.
func stripANSI(s string) string {
	return ansi.Strip(s)
}

// Many mutually-overlapping events on a single day must stay confined to
// that day's own column — none of their lane content may spill into the
// next day's column.
func TestRenderWeekManyOverlapsStayWithinTheirDayColumn(t *testing.T) {
	focus := time.Date(2026, 6, 10, 0, 0, 0, 0, time.UTC)
	base := time.Date(2026, 6, 8, 9, 0, 0, 0, time.UTC) // Monday, the week's first day
	var occs []model.Occurrence
	for i := 0; i < 20; i++ {
		occs = append(occs, occ(fmt.Sprintf("m%d", i), fmt.Sprintf("Event%d", i), base, time.Hour))
	}

	got := RenderWeek(occs, focus, 120, 30, focus, time.UTC, NewStyles(true), false, "monday")
	lines := strings.Split(got, "\n")

	const colWidth = 15 // (120 - gutterWidth - six separators) / 7 days
	mondayEnd := gutterWidth + colWidth
	tuesdayStart := mondayEnd + 1 // skip the vertical separator
	tuesdayEnd := tuesdayStart + colWidth

	// Only the hour-grid rows (header at 0, all-day at 1) place event
	// content per-column; the header legitimately has "Tue" text there.
	for i, line := range lines {
		if i < 2 {
			continue
		}
		plain := []rune(stripANSI(line))
		if len(plain) <= tuesdayStart {
			continue
		}
		end := tuesdayEnd
		if end > len(plain) {
			end = len(plain)
		}
		tuesdayCell := strings.TrimSpace(string(plain[tuesdayStart:end]))
		if tuesdayCell != "" {
			t.Errorf("line %d: Monday's overlapping events spilled into Tuesday's column: %q", i, line)
		}
	}
}

// Two all-day events on the same day must not silently drop the second —
// the all-day row should indicate an overflow, mirroring month.go's badge.
func TestRenderWeekAllDayOverflowIsIndicated(t *testing.T) {
	focus := time.Date(2026, 6, 10, 0, 0, 0, 0, time.UTC)
	day := time.Date(2026, 6, 10, 0, 0, 0, 0, time.UTC)
	occs := []model.Occurrence{
		{UID: "x1", Summary: "Holiday", AllDay: true, Start: day, End: day.AddDate(0, 0, 1), CalendarID: "work"},
		{UID: "x2", Summary: "Birthday", AllDay: true, Start: day, End: day.AddDate(0, 0, 1), CalendarID: "work"},
	}

	got := RenderWeek(occs, focus, 120, 30, focus, time.UTC, NewStyles(true), false, "monday")
	if !strings.Contains(got, "+1") {
		t.Errorf("all-day row should indicate the second same-day event with a +1 badge:\n%s", got)
	}
}

// renderTimeSlotCell guarantees exactly colWidth per column; renderAllDayRow ended
// with pad() and no truncate, so an oversized "+N" badge (many same-day all-day
// events in a narrow column) pushed every later day column out of alignment.
func TestRenderAllDayRowIsExactlyColumnWidth(t *testing.T) {
	day := time.Date(2026, 6, 10, 0, 0, 0, 0, time.UTC)
	days := []time.Time{day, day.AddDate(0, 0, 1)}

	var many []model.Occurrence
	for i := 0; i < 1000; i++ {
		many = append(many, model.Occurrence{
			UID: "x", Summary: "A very long all-day event summary indeed",
			AllDay: true, Start: day, End: day.AddDate(0, 0, 1), CalendarID: "work",
		})
	}
	byDay := groupByDay(many, time.UTC)

	for _, colWidth := range []int{5, 6, 8, 20} {
		got := renderAllDayRow(days, byDay, colWidth, NewStyles(true))
		want := gutterWidth + colWidth*len(days) + len(days) - 1
		if w := lipgloss.Width(got); w != want {
			t.Errorf("colWidth %d: all-day row width = %d, want exactly %d: %q", colWidth, w, want, got)
		}
	}
}

// The header must invert the focused day's whole column, distinct from
// today's.
func TestRenderWeekHeaderMarksOnlyTheFocusedDay(t *testing.T) {
	days := weekDays(time.Date(2026, 6, 10, 0, 0, 0, 0, time.UTC), "monday")
	today := "2026-06-08"    // Monday, unfocused
	focusKey := "2026-06-11" // Thursday
	got := renderWeekHeader(days, today, focusKey, 12, NewStyles(true))

	if n := strings.Count(got, bgEscape); n != 1 {
		t.Fatalf("expected exactly one background-filled column, got %d: %q", n, got)
	}
	if !strings.Contains(got, "Thu 11") {
		t.Errorf("focused day's label missing: %q", got)
	}
}

// The background fill must cover the whole column, padding included, as a
// single unbroken styled run -- not just the visible label -- mirroring the
// same guarantee pinned for the month grid.
func TestRenderWeekHeaderFocusedFillsWholeColumnUnbroken(t *testing.T) {
	days := weekDays(time.Date(2026, 6, 10, 0, 0, 0, 0, time.UTC), "monday")
	got := renderWeekHeader(days, "2026-06-08", "2026-06-09", 12, NewStyles(true)) // Tuesday, single-digit day

	filled, ok := styledRunWithBackground(got)
	if !ok {
		t.Fatalf("could not locate the background-filled run: %q", got)
	}
	if w := lipgloss.Width(filled); w != 12 {
		t.Errorf("filled run width = %d, want the full column width 12: %q", w, filled)
	}
}

// When focus == today, both facts must remain legible in the header. As in
// the month grid, focusedCellStyle swaps in the accent colour when the
// column is also today, and DefaultPalette's SelectedFg happens to already
// equal Accent, so the swap is a no-op here and a focused-today column
// renders identically to a focused (non-today) column with the same label.
// TestRenderWeekHeaderFocusedTodayUsesAccentRegardlessOfPalette below is
// what actually exercises the swap, using a palette where it is not a no-op.
func TestRenderWeekHeaderFocusedDayAndTodaySameDayApparent(t *testing.T) {
	days := weekDays(time.Date(2026, 6, 10, 0, 0, 0, 0, time.UTC), "monday")
	st := NewStyles(true)
	sameDay := renderWeekHeader(days, "2026-06-09", "2026-06-09", 12, st)
	plainToday := renderWeekHeader(days, "2026-06-09", "2026-06-01", 12, st)
	plainFocus := renderWeekHeader(days, "2026-06-01", "2026-06-09", 12, st)

	if !strings.Contains(sameDay, bgEscape) {
		t.Errorf("background fill missing when focus == today: %q", sameDay)
	}
	if sameDay != plainFocus {
		t.Errorf("focus == today should render exactly like focus alone: got %q, want %q", sameDay, plainFocus)
	}
	if sameDay == plainToday {
		t.Errorf("focus fill lost when focus == today: %q", sameDay)
	}
}

// The header's per-column width must not change with the focus marker: a
// two-digit focused day must line up exactly like every other column.
func TestRenderWeekHeaderColumnWidthUnaffectedByFocus(t *testing.T) {
	days := weekDays(time.Date(2026, 6, 22, 0, 0, 0, 0, time.UTC), "monday") // includes a two-digit day
	st := NewStyles(true)
	for _, colWidth := range []int{6, 8, 12, 20} {
		unfocused := renderWeekHeader(days, "2026-06-01", "", colWidth, st)
		focusedSingle := renderWeekHeader(days, "2026-06-01", "2026-06-23", colWidth, st) // single-digit day
		focusedDouble := renderWeekHeader(days, "2026-06-01", "2026-06-25", colWidth, st) // two-digit day
		want := gutterWidth + colWidth*7 + 6
		for _, got := range []string{unfocused, focusedSingle, focusedDouble} {
			if w := lipgloss.Width(got); w != want {
				t.Errorf("colWidth %d: header width = %d, want %d: %q", colWidth, w, want, got)
			}
		}
	}
}

// Integration test through the real RenderWeek call site, not
// renderWeekHeader directly: the unit tests above call renderWeekHeader
// with today and focusKey supplied by hand, so they would not catch the
// call site itself passing them in the wrong order. This uses a focus day
// that differs from today, so a today/focusKey swap at the call site would
// move the fill onto today's column (Mon 8) instead of the focused one
// (Thu 11) and this test would catch it.
func TestRenderWeekMarksFocusedColumnThroughFullRender(t *testing.T) {
	focus := time.Date(2026, 6, 11, 0, 0, 0, 0, time.UTC) // Thursday
	now := time.Date(2026, 6, 8, 0, 0, 0, 0, time.UTC)    // Monday, same week
	got := RenderWeek(nil, focus, 120, 30, now, time.UTC, NewStyles(true), false, "monday")
	header := strings.Split(got, "\n")[0]

	if n := strings.Count(header, bgEscape); n != 1 {
		t.Fatalf("expected exactly one background-filled column in the header, got %d:\n%s", n, header)
	}
	filled, ok := styledRunWithBackground(header)
	if !ok {
		t.Fatalf("could not locate the background-filled run:\n%s", header)
	}
	if trimmed := strings.TrimSpace(filled); trimmed != "Thu 11" {
		t.Errorf("the filled run's content = %q, want the focused day's label (Thu 11), not today's (Mon 8) -- a today/focusKey argument swap at the call site would produce this failure:\n%s", trimmed, header)
	}
}

// Even with every colour stripped away, the focused column must remain
// distinguishable from an unfocused one: renderWeekHeader applies
// st.SelectedCell across the whole column regardless of what that style is
// made of, so a colour-free SelectedCell (plainStyles' Reverse-only rule,
// defined in month_test.go) still produces a visibly different column. This
// proves the renderer's own contract, not any claim about real-terminal
// colour degrading. NewStyles(false) is deliberately NOT used here: it is
// the light theme, still full 24-bit colour, not monochrome.
func TestRenderWeekHeaderFocusedVisibleWithoutColour(t *testing.T) {
	days := weekDays(time.Date(2026, 6, 10, 0, 0, 0, 0, time.UTC), "monday")
	st := plainStyles()
	focusKey := "2026-06-11" // Thursday
	got := renderWeekHeader(days, "2026-06-08", focusKey, 12, st)
	unfocused := renderWeekHeader(days, "2026-06-08", "", 12, st)

	if got == unfocused {
		t.Errorf("focused header indistinguishable from unfocused with no colour at all: %q", got)
	}
	if !strings.Contains(got, "\x1b[7m") {
		t.Errorf("focused column missing the reverse-video attribute: %q", got)
	}
}

// A focused column that is also today must show the accent colour, not
// SelectedFg, regardless of what a theme chooses for SelectedFg. Mirrors
// TestRenderMonthCellFocusedTodayUsesAccentRegardlessOfPalette; see there
// for why divergentPalette (SelectedFg != Accent) is required to make this
// test capable of failing.
func TestRenderWeekHeaderFocusedTodayUsesAccentRegardlessOfPalette(t *testing.T) {
	st := theme.BuildStyles(divergentPalette(), true)
	accentCode := fgColorCode(divergentPalette().Accent.Resolve(true))
	selectedFgCode := fgColorCode(divergentPalette().SelectedFg.Resolve(true))

	days := weekDays(time.Date(2026, 6, 10, 0, 0, 0, 0, time.UTC), "monday")
	focusedToday := renderWeekHeader(days, "2026-06-09", "2026-06-09", 12, st) // Tuesday
	if !strings.Contains(focusedToday, bgEscape) {
		t.Errorf("focused+today column missing its background fill (fact: focused): %q", focusedToday)
	}
	if !strings.Contains(focusedToday, accentCode) {
		t.Errorf("focused+today column should use the accent colour (fact: today), got %q", focusedToday)
	}
	if strings.Contains(focusedToday, selectedFgCode) {
		t.Errorf("focused+today column should not use plain SelectedFg once it diverges from Accent: %q", focusedToday)
	}

	focusedOnly := renderWeekHeader(days, "2026-06-01", "2026-06-09", 12, st) // Tuesday, not today
	if !strings.Contains(focusedOnly, selectedFgCode) {
		t.Errorf("focused (non-today) column should use SelectedFg, got %q", focusedOnly)
	}
	if strings.Contains(focusedOnly, accentCode) {
		t.Errorf("focused (non-today) column should not use the accent colour: %q", focusedOnly)
	}
}
