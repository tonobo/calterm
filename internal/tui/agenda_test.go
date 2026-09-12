package tui

import (
	"strings"
	"testing"
	"time"

	"charm.land/lipgloss/v2"

	"github.com/tonobo/calterm/internal/model"
)

func occ(uid, summary string, start time.Time, dur time.Duration) model.Occurrence {
	return model.Occurrence{
		UID: uid, Summary: summary, Start: start, End: start.Add(dur),
		AccountID: "personal", CalendarID: "work",
	}
}

func agendaFixture() []model.Occurrence {
	d := func(day, hour, min int) time.Time {
		return time.Date(2026, 6, day, hour, min, 0, 0, time.UTC)
	}
	return []model.Occurrence{
		occ("a", "Standup", d(10, 9, 0), 30*time.Minute),
		occ("b", "Design review", d(10, 14, 0), time.Hour),
		{UID: "c", Summary: "Company holiday", AllDay: true,
			Start:     time.Date(2026, 6, 11, 0, 0, 0, 0, time.UTC),
			End:       time.Date(2026, 6, 12, 0, 0, 0, 0, time.UTC),
			AccountID: "personal", CalendarID: "work"},
		occ("d", "1:1", d(12, 11, 0), 30*time.Minute),
	}
}

// Keyed by account AND calendar: see model.CalendarKey.
var testNames = map[string]string{"personal/work": "Work"}

func TestRenderAgendaGroupsByDay(t *testing.T) {
	now := time.Date(2026, 6, 10, 8, 0, 0, 0, time.UTC)
	got := RenderAgenda(agendaFixture(), 0, 80, 24, now, time.UTC, NewStyles(true), testNames, false)

	for _, want := range []string{"Standup", "Design review", "Company holiday", "1:1"} {
		if !strings.Contains(got, want) {
			t.Errorf("agenda is missing %q:\n%s", want, got)
		}
	}
	// Each distinct day gets exactly one header.
	for _, header := range []string{"Wed 10 June", "Thu 11 June", "Fri 12 June"} {
		if n := strings.Count(got, header); n != 1 {
			t.Errorf("header %q appears %d times, want 1:\n%s", header, n, got)
		}
	}
}

// Agenda day separators have no grid to hold a Wk column, so the week
// number is appended to the date line instead. 2026-06-10 is ISO week 24.
func TestRenderAgendaDayHeaderIncludesWeekNumber(t *testing.T) {
	now := time.Date(2026, 6, 10, 8, 0, 0, 0, time.UTC)
	got := RenderAgenda(agendaFixture(), 0, 80, 24, now, time.UTC, NewStyles(true), testNames, false)
	if !strings.Contains(got, "Wed 10 June · Wk 24") {
		t.Errorf("agenda day header missing '· Wk 24':\n%s", got)
	}
}

// Today's header appends " · Today" after the base label; the Wk suffix
// must still be present alongside it, not silently dropped.
func TestRenderAgendaTodayHeaderIncludesWeekNumber(t *testing.T) {
	now := time.Date(2026, 6, 10, 8, 0, 0, 0, time.UTC)
	got := RenderAgenda(agendaFixture(), 0, 80, 24, now, time.UTC, NewStyles(true), testNames, false)
	if !strings.Contains(got, "Wk 24") || !strings.Contains(got, "Today") {
		t.Errorf("today's header should carry both the Wk suffix and Today:\n%s", got)
	}
}

func TestRenderAgendaMarksToday(t *testing.T) {
	now := time.Date(2026, 6, 10, 8, 0, 0, 0, time.UTC)
	got := RenderAgenda(agendaFixture(), 0, 80, 24, now, time.UTC, NewStyles(true), testNames, false)
	if !strings.Contains(got, "Today") {
		t.Errorf("the current day's header should be labelled Today:\n%s", got)
	}
}

func TestRenderAgendaShowsTimesAndAllDay(t *testing.T) {
	now := time.Date(2026, 6, 10, 8, 0, 0, 0, time.UTC)
	got := RenderAgenda(agendaFixture(), 0, 80, 24, now, time.UTC, NewStyles(true), testNames, false)
	if !strings.Contains(got, "09:00") {
		t.Errorf("timed event should show its start time:\n%s", got)
	}
	if !strings.Contains(got, "all day") {
		t.Errorf("all-day event should be labelled:\n%s", got)
	}
}

func TestRenderAgendaShowsCategoriesBeforeSummary(t *testing.T) {
	now := time.Date(2026, 6, 10, 8, 0, 0, 0, time.UTC)
	o := occ("grouped", "Planning", now.Add(time.Hour), time.Hour)
	o.Categories = []string{"Class A", "Class B"}
	got := stripANSI(RenderAgenda([]model.Occurrence{o}, 0, 80, 24, now, time.UTC, NewStyles(true), testNames, false))
	if !strings.Contains(got, "[Class A, Class B] Planning") {
		t.Errorf("agenda is missing event categories:\n%s", got)
	}
}

// A multi-day absence which began before the visible agenda context must not
// disappear while the week view still shows it on today. Carry it into today's
// group, matching the day/week overlap semantics.
func TestRenderAgendaCarriesOngoingMultiDayEventIntoToday(t *testing.T) {
	loc := time.FixedZone("CEST", 2*60*60)
	now := time.Date(2026, 9, 1, 8, 0, 0, 0, loc)
	occs := []model.Occurrence{
		{UID: "vacation", Summary: "Extended vacation", AllDay: true,
			Start: time.Date(2026, 8, 7, 0, 0, 0, 0, time.UTC),
			End:   time.Date(2026, 9, 3, 0, 0, 0, 0, time.UTC)},
		{UID: "party", Summary: "Summer horse party", AllDay: true,
			Start: time.Date(2026, 8, 29, 0, 0, 0, 0, time.UTC),
			End:   time.Date(2026, 8, 30, 0, 0, 0, 0, time.UTC)},
		occ("casual", "Team coffee", time.Date(2026, 9, 1, 9, 0, 0, 0, loc), 2*time.Hour),
	}

	got := stripANSI(RenderAgenda(occs, 1, 100, 20, now, loc, NewStyles(true), testNames, false))
	if !strings.Contains(got, "Tue 1 September · Wk 36 · Today\n"+cursorMarker+" all day") ||
		!strings.Contains(got, "Extended vacation") {
		t.Errorf("ongoing absence is not selected in today's group:\n%s", got)
	}
	if strings.Contains(got, "Fri 7 August") {
		t.Errorf("ongoing absence remained filed under its stale start date:\n%s", got)
	}
	if party, today := strings.Index(got, "Sat 29 August"), strings.Index(got, "Tue 1 September"); party < 0 || today < party {
		t.Errorf("agenda day groups are not chronological:\n%s", got)
	}
}

func TestRenderAgendaEmpty(t *testing.T) {
	now := time.Date(2026, 6, 10, 8, 0, 0, 0, time.UTC)
	got := RenderAgenda(nil, 0, 80, 24, now, time.UTC, NewStyles(true), testNames, false)
	if !strings.Contains(strings.ToLower(got), "no events") {
		t.Errorf("an empty agenda should say so, got:\n%s", got)
	}
}

func TestRenderAgendaShowsInvitationResponse(t *testing.T) {
	now := time.Date(2026, 6, 10, 8, 0, 0, 0, time.UTC)
	o := occ("invite", "Planning", now.Add(time.Hour), time.Hour)
	o.AttendeeStatus = "NEEDS-ACTION"
	got := stripANSI(RenderAgenda([]model.Occurrence{o}, 0, 80, 24, now, time.UTC, NewStyles(true), testNames, false))
	if !strings.Contains(got, "? Planning") {
		t.Errorf("pending invitation marker is missing:\n%s", got)
	}
}

func TestRenderAgendaShowsMultiDayAllDayEndpoint(t *testing.T) {
	now := time.Date(2026, 9, 11, 8, 0, 0, 0, time.UTC)
	o := model.Occurrence{
		UID: "holiday", Summary: "Project vacation", AllDay: true,
		Start: time.Date(2026, 9, 14, 0, 0, 0, 0, time.UTC),
		End:   time.Date(2026, 9, 19, 0, 0, 0, 0, time.UTC),
	}
	got := stripANSI(RenderAgenda([]model.Occurrence{o}, 0, 80, 24, now, time.UTC, NewStyles(true), testNames, false))
	if !strings.Contains(got, "all day→Fri") {
		t.Errorf("multi-day all-day endpoint is missing:\n%s", got)
	}
}

// An empty agenda because every calendar is hidden must say so, rather than
// blaming the cache for what the user just did by toggling calendars off.
func TestRenderAgendaEmptyBecauseCalendarsAreHidden(t *testing.T) {
	now := time.Date(2026, 6, 10, 8, 0, 0, 0, time.UTC)
	got := RenderAgenda(nil, 0, 80, 24, now, time.UTC, NewStyles(true), testNames, true)
	lower := strings.ToLower(got)
	if !strings.Contains(lower, "hidden") {
		t.Errorf("should mention that calendars are hidden, got:\n%s", got)
	}
	if strings.Contains(lower, "indexed window") {
		t.Errorf("should not blame the cache when the cause is hidden calendars:\n%s", got)
	}
}

// The rendered block must never exceed the width it was given, or it will wrap
// and corrupt the layout.
func TestRenderAgendaRespectsWidth(t *testing.T) {
	now := time.Date(2026, 6, 10, 8, 0, 0, 0, time.UTC)
	long := []model.Occurrence{occ("x",
		strings.Repeat("a very long summary ", 20),
		time.Date(2026, 6, 10, 9, 0, 0, 0, time.UTC), time.Hour)}

	for _, width := range []int{40, 60, 80, 120} {
		got := RenderAgenda(long, 0, width, 24, now, time.UTC, NewStyles(true), testNames, false)
		for i, line := range strings.Split(got, "\n") {
			if w := lipglossWidth(line); w > width {
				t.Errorf("width %d: line %d is %d cells wide: %q", width, i, w, line)
			}
		}
	}
}

func TestRenderAgendaCursorSelectsTheRightEvent(t *testing.T) {
	now := time.Date(2026, 6, 10, 8, 0, 0, 0, time.UTC)
	first := RenderAgenda(agendaFixture(), 0, 80, 24, now, time.UTC, NewStyles(true), testNames, false)
	second := RenderAgenda(agendaFixture(), 1, 80, 24, now, time.UTC, NewStyles(true), testNames, false)
	if first == second {
		t.Error("moving the cursor should change the rendered output")
	}
	if !strings.Contains(first, cursorMarker) {
		t.Errorf("the selected row should carry the cursor marker %q:\n%s", cursorMarker, first)
	}
	if strings.Count(first, cursorMarker) != 1 {
		t.Errorf("exactly one row should be marked, got %d", strings.Count(first, cursorMarker))
	}
}

// A cursor out of range must not panic — it can happen after a sync shrinks
// the occurrence list under the user.
// A long list must scroll rather than clip: the cursor has to stay on screen.
func TestRenderAgendaScrollsToKeepCursorVisible(t *testing.T) {
	now := time.Date(2026, 6, 10, 8, 0, 0, 0, time.UTC)
	var many []model.Occurrence
	for i := 0; i < 100; i++ {
		many = append(many, occ(
			string(rune('a'+i%26)),
			"Event "+time.Duration(i).String(),
			time.Date(2026, 6, 10, 0, 0, 0, 0, time.UTC).Add(time.Duration(i)*time.Hour),
			30*time.Minute))
	}
	const height = 24
	for _, cursor := range []int{0, 50, 99} {
		got := RenderAgenda(many, cursor, 80, height, now, time.UTC, NewStyles(true), testNames, false)
		lines := strings.Split(got, "\n")
		if len(lines) > height {
			t.Errorf("cursor %d: rendered %d lines, want at most %d", cursor, len(lines), height)
		}
		if !strings.Contains(got, cursorMarker) {
			t.Errorf("cursor %d scrolled off screen; the marker is not in the output", cursor)
		}
	}
}

func TestRenderAgendaOutOfRangeCursor(t *testing.T) {
	now := time.Date(2026, 6, 10, 8, 0, 0, 0, time.UTC)
	for _, cursor := range []int{-1, 99} {
		got := RenderAgenda(agendaFixture(), cursor, 80, 24, now, time.UTC, NewStyles(true), testNames, false)
		if got == "" {
			t.Errorf("cursor %d produced no output", cursor)
		}
	}
}

func lipglossWidth(s string) int { return lipgloss.Width(s) }

// The time column must stay a fixed width regardless of whether the row is
// selected. cursorMarker ("▸") is 3 bytes but 1 display cell; padding the
// time column with len(marker) instead of lipgloss.Width(marker) makes the
// selected row's column one cell narrower, so the calendar dot and summary
// jump left every time the cursor lands on that row.
//
// This renders a single row directly (rather than the full agenda, which
// would put the row on its own line after a day header) so lipgloss.Width
// measures only that row's prefix: lipgloss.Width takes the MAX width across
// lines when the string contains a newline, which would mask the very
// column shift this test exists to catch.
func TestRenderAgendaColumnsAlignRegardlessOfCursor(t *testing.T) {
	o := occ("a", "Standup", time.Date(2026, 6, 10, 9, 0, 0, 0, time.UTC), 30*time.Minute)

	selected := renderAgendaRow(o, true, 80, time.UTC, NewStyles(true), testNames)
	unselected := renderAgendaRow(o, false, 80, time.UTC, NewStyles(true), testNames)

	i1 := strings.Index(selected, "Standup")
	i2 := strings.Index(unselected, "Standup")
	if i1 < 0 || i2 < 0 {
		t.Fatalf("Standup not found in one of the rows:\nselected:   %q\nunselected: %q", selected, unselected)
	}

	col1 := lipglossWidth(selected[:i1])
	col2 := lipglossWidth(unselected[:i2])
	if col1 != col2 {
		t.Errorf("summary column shifts with the cursor: selected row starts at column %d, unselected at %d", col1, col2)
	}
}
