package model

import (
	"testing"
	"time"
)

func at(day, hour int) time.Time {
	return time.Date(2026, 6, day, hour, 0, 0, 0, time.UTC)
}

func sample() []Occurrence {
	return []Occurrence{
		{UID: "a", Summary: "Early", Start: at(10, 9), End: at(10, 10)},
		{UID: "b", Summary: "Midday", Start: at(10, 12), End: at(10, 13)},
		{UID: "c", Summary: "Holiday", Start: time.Date(2026, 6, 11, 0, 0, 0, 0, time.UTC), End: time.Date(2026, 6, 12, 0, 0, 0, 0, time.UTC), AllDay: true},
		{UID: "d", Summary: "Later", Start: at(12, 15), End: at(12, 16)},
	}
}

func summaries(occs []Occurrence) []string {
	out := make([]string, len(occs))
	for i, o := range occs {
		out[i] = o.Summary
	}
	return out
}

func TestBetween(t *testing.T) {
	got := Between(sample(), at(10, 10), at(11, 12))
	want := []string{"Midday", "Holiday"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", summaries(got), want)
	}
	for i := range want {
		if got[i].Summary != want[i] {
			t.Errorf("index %d = %q, want %q", i, got[i].Summary, want[i])
		}
	}
}

func TestOnDayIncludesAllDayAndTimed(t *testing.T) {
	got := OnDay(sample(), at(10, 0), time.UTC)
	if s := summaries(got); len(s) != 2 || s[0] != "Early" || s[1] != "Midday" {
		t.Errorf("OnDay(Jun 10) = %v, want [Early Midday]", s)
	}
	got = OnDay(sample(), at(11, 0), time.UTC)
	if s := summaries(got); len(s) != 1 || s[0] != "Holiday" {
		t.Errorf("OnDay(Jun 11) = %v, want [Holiday]", s)
	}
	if got := OnDay(sample(), at(13, 0), time.UTC); len(got) != 0 {
		t.Errorf("OnDay(Jun 13) = %v, want empty", summaries(got))
	}
}

// An all-day event is a date, not an instant: it must land on the same
// calendar day regardless of the viewer's timezone.
func TestOnDayAllDayIsTimezoneStable(t *testing.T) {
	loc, err := time.LoadLocation("Pacific/Auckland") // UTC+12/+13
	if err != nil {
		t.Skipf("tz database unavailable: %v", err)
	}
	day := time.Date(2026, 6, 11, 0, 0, 0, 0, loc)
	got := OnDay(sample(), day, loc)
	// June 11 in Auckland includes June 10 12:00-June 11 12:00 UTC, so both
	// Midday (10:12-13 UTC) and Holiday (all-day on 11-12 UTC) fall on this day.
	if s := summaries(got); len(s) != 2 || s[0] != "Midday" || s[1] != "Holiday" {
		t.Errorf("OnDay in Auckland = %v, want [Midday Holiday]", s)
	}
}

func TestNextAfter(t *testing.T) {
	got, ok := NextAfter(sample(), at(10, 10))
	if !ok {
		t.Fatal("NextAfter returned no occurrence")
	}
	if got.Summary != "Midday" {
		t.Errorf("NextAfter = %q, want Midday", got.Summary)
	}
	if _, ok := NextAfter(sample(), at(20, 0)); ok {
		t.Error("NextAfter past the last event should report false")
	}
}

func TestNextAfterSkipsInProgress(t *testing.T) {
	// At 12:30 the Midday event is running; the *next* one is Later.
	got, ok := NextAfter(sample(), at(10, 12).Add(30*time.Minute))
	if !ok {
		t.Fatal("NextAfter returned no occurrence")
	}
	if got.Summary != "Holiday" {
		t.Errorf("NextAfter = %q, want Holiday", got.Summary)
	}
}

func TestInProgress(t *testing.T) {
	got, ok := InProgress(sample(), at(10, 12).Add(30*time.Minute))
	if !ok {
		t.Fatal("InProgress found nothing at 12:30")
	}
	if got.Summary != "Midday" {
		t.Errorf("InProgress = %q, want Midday", got.Summary)
	}
	// All-day events are never reported as in progress: they would mask every
	// timed event for the whole day in the waybar module.
	if got, ok := InProgress(sample(), time.Date(2026, 6, 11, 10, 0, 0, 0, time.UTC)); ok {
		t.Errorf("InProgress during an all-day event = %q, want none", got.Summary)
	}
	if _, ok := InProgress(sample(), at(10, 11)); ok {
		t.Error("InProgress between events should report false")
	}
}

// Regression test: timed events must be found on their local calendar day.
// A timed event at 09:00 local time, which is the previous day in UTC,
// should still be found when querying for "today" in that timezone.
func TestOnDayTimedEventUsesViewerLocation(t *testing.T) {
	loc, err := time.LoadLocation("Pacific/Auckland") // UTC+12 in June
	if err != nil {
		t.Skipf("tz database unavailable: %v", err)
	}
	// Event at 09:00-10:00 in Auckland, which is 21:00-22:00 UTC the previous day
	event := Occurrence{
		UID:     "test",
		Summary: "Morning meeting",
		Start:   time.Date(2026, 6, 10, 9, 0, 0, 0, loc),
		End:     time.Date(2026, 6, 10, 10, 0, 0, 0, loc),
	}
	// Query for events on June 10 in Auckland
	day := time.Date(2026, 6, 10, 0, 0, 0, 0, loc)
	got := OnDay([]Occurrence{event}, day, loc)
	if len(got) != 1 || got[0].Summary != "Morning meeting" {
		t.Errorf("OnDay(June 10 in Auckland) = %v, want [Morning meeting]", summaries(got))
	}
}
