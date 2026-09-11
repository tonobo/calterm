package tui

import (
	"strings"
	"testing"
	"time"

	"charm.land/lipgloss/v2"

	"github.com/tonobo/calterm/internal/model"
)

func dayPaneFixture() []model.Occurrence {
	d := func(hour, min int) time.Time {
		return time.Date(2026, 8, 27, hour, min, 0, 0, time.UTC)
	}
	return []model.Occurrence{
		{UID: "allday", Summary: "Company holiday", AllDay: true,
			Start:     time.Date(2026, 8, 27, 0, 0, 0, 0, time.UTC),
			End:       time.Date(2026, 8, 28, 0, 0, 0, 0, time.UTC),
			AccountID: "personal", CalendarID: "work"},
		occ("a", "Standup", d(9, 0), 30*time.Minute),
		occ("b", "Design review with the whole extended product and design team", d(14, 0), time.Hour),
		occ("c", "One-on-one", d(11, 0), 30*time.Minute),
	}
}

func TestRenderDayPaneShowsDateAndEvents(t *testing.T) {
	day := time.Date(2026, 8, 27, 0, 0, 0, 0, time.UTC)
	got := RenderDayPane(dayPaneFixture(), day, 60, 10, day, time.UTC, NewStyles(true), testNames)
	if !strings.Contains(got, "Thu 27 August") {
		t.Errorf("day pane missing the date line:\n%s", got)
	}
	for _, want := range []string{"Standup", "One-on-one", "Company holiday"} {
		if !strings.Contains(got, want) {
			t.Errorf("day pane missing %q:\n%s", want, got)
		}
	}
}

// The focused-day pane has no grid to hold a Wk column, so the date line
// appends it instead. Aug 27 2026 is ISO week 35.
func TestRenderDayPaneDateLineIncludesWeekNumber(t *testing.T) {
	day := time.Date(2026, 8, 27, 0, 0, 0, 0, time.UTC)
	got := RenderDayPane(dayPaneFixture(), day, 60, 10, day, time.UTC, NewStyles(true), testNames)
	if !strings.Contains(got, "Thu 27 August · Wk 35") {
		t.Errorf("day pane date line missing '· Wk 35':\n%s", got)
	}
}

func TestRenderDayPaneShowsAllDayLabelBesideTimed(t *testing.T) {
	day := time.Date(2026, 8, 27, 0, 0, 0, 0, time.UTC)
	got := RenderDayPane(dayPaneFixture(), day, 60, 10, day, time.UTC, NewStyles(true), testNames)
	if !strings.Contains(got, "all day") {
		t.Errorf("all-day event should carry the all-day label:\n%s", got)
	}
	if !strings.Contains(got, "09:00") {
		t.Errorf("timed event should show its start time:\n%s", got)
	}
}

func TestRenderDayPaneEmptyDayShowsNoEventsLine(t *testing.T) {
	day := time.Date(2026, 8, 27, 0, 0, 0, 0, time.UTC)
	got := RenderDayPane(nil, day, 60, 10, day, time.UTC, NewStyles(true), testNames)
	if !strings.Contains(got, "No events") {
		t.Errorf("empty day should show a No events line:\n%s", got)
	}
	if strings.TrimSpace(got) == "" {
		t.Error("an empty day must not render an empty pane")
	}
}

func TestRenderDayPaneOverflowReportsRemainder(t *testing.T) {
	day := time.Date(2026, 8, 27, 0, 0, 0, 0, time.UTC)
	d := func(hour int) time.Time { return time.Date(2026, 8, 27, hour, 0, 0, 0, time.UTC) }
	var occs []model.Occurrence
	for i := 0; i < 10; i++ {
		occs = append(occs, occ("e"+string(rune('a'+i)), "Event", d(i), 30*time.Minute))
	}
	// height 5: 1 date line + 4 budget lines -> 3 events shown, "+7 more".
	got := RenderDayPane(occs, day, 60, 5, day, time.UTC, NewStyles(true), testNames)
	if !strings.Contains(got, "+7 more") {
		t.Errorf("overflow should report the remainder as +7 more:\n%s", got)
	}
	lines := strings.Split(got, "\n")
	if len(lines) > 5 {
		t.Errorf("pane emitted %d lines, want at most 5", len(lines))
	}
}

func TestRenderDayPaneNeverExceedsWidthOrHeight(t *testing.T) {
	day := time.Date(2026, 8, 27, 0, 0, 0, 0, time.UTC)
	longFixture := []model.Occurrence{
		occ("a", "An extremely long summary that goes on and on and on well past any reasonable pane width budget", time.Date(2026, 8, 27, 9, 0, 0, 0, time.UTC), time.Hour),
		occ("b", "Another very long summary padded out with extra words to force truncation logic to run", time.Date(2026, 8, 27, 10, 0, 0, 0, time.UTC), time.Hour),
		occ("c", "Third overlapping-in-time long summary describing an event at length", time.Date(2026, 8, 27, 9, 30, 0, 0, time.UTC), time.Hour),
	}
	for _, size := range []struct{ w, h int }{{10, 3}, {24, 4}, {40, 6}, {80, 10}} {
		got := RenderDayPane(longFixture, day, size.w, size.h, day, time.UTC, NewStyles(true), testNames)
		lines := strings.Split(got, "\n")
		if got == "" {
			continue
		}
		if len(lines) > size.h {
			t.Errorf("size %dx%d: rendered %d lines, want at most %d", size.w, size.h, len(lines), size.h)
		}
		for i, line := range lines {
			if w := lipgloss.Width(line); w > size.w {
				t.Errorf("size %dx%d: line %d is %d cells: %q", size.w, size.h, i, w, line)
			}
		}
	}
}

func TestRenderDayPaneFollowsTheGivenDay(t *testing.T) {
	day := time.Date(2026, 8, 27, 0, 0, 0, 0, time.UTC)
	other := day.AddDate(0, 0, 1)
	got := RenderDayPane(dayPaneFixture(), other, 60, 10, day, time.UTC, NewStyles(true), testNames)
	if strings.Contains(got, "Standup") {
		t.Errorf("pane for a different day should not show today's events:\n%s", got)
	}
	if !strings.Contains(got, "Fri 28 August") {
		t.Errorf("pane should be dated for the requested day:\n%s", got)
	}
}
