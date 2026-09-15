package reminder

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/tonobo/calterm/internal/model"
	"github.com/tonobo/calterm/internal/store"
)

func occurrence(uid string, start time.Time) model.Occurrence {
	return model.Occurrence{
		UID: uid, Start: start, End: start.Add(30 * time.Minute),
		Summary: "Example event", AccountID: "personal", CalendarID: "calendar",
	}
}

func TestDueSelectsOnlyEligibleOccurrences(t *testing.T) {
	now := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	atBoundary := occurrence("boundary", now.Add(5*time.Minute))
	recentThreshold := occurrence("recent", now.Add(4*time.Minute))
	tooLate := occurrence("late", now.Add(5*time.Minute+time.Nanosecond))
	tooOld := occurrence("old", now.Add(5*time.Minute-StartGrace-time.Nanosecond))
	allDay := occurrence("all-day", now.Add(5*time.Minute))
	allDay.AllDay = true
	unconfigured := occurrence("unconfigured", now.Add(5*time.Minute))
	unconfigured.CalendarID = "other"
	cancelled := occurrence("cancelled", now.Add(5*time.Minute))
	cancelled.Status = "CANCELLED"
	declined := occurrence("declined", now.Add(5*time.Minute))
	declined.AttendeeStatus = "DECLINED"
	rules := []Rule{{Calendar: "personal/calendar", Before: 5 * time.Minute, Sound: "complete"}}

	got := Due(
		[]model.Occurrence{tooLate, atBoundary, allDay, unconfigured, cancelled, recentThreshold, declined, tooOld},
		NewState(), rules, now,
	)
	if len(got) != 2 || got[0].Occurrence.UID != "recent" || got[1].Occurrence.UID != "boundary" {
		t.Fatalf("due occurrences = %+v, want recent and boundary", got)
	}
	if got[0].Before != 5*time.Minute || got[0].Sound != "complete" {
		t.Errorf("delivery rule = %+v", got[0])
	}
}

func TestDueDoesNotReplayOlderThresholdsWhenSeveralAreConfigured(t *testing.T) {
	now := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	event := occurrence("event", now.Add(4*time.Minute))
	rules := []Rule{
		{Calendar: "personal/calendar", Before: 30 * time.Minute},
		{Calendar: "personal/calendar", Before: 5 * time.Minute, Sound: "message-new-instant"},
		{Calendar: "personal/calendar", Before: time.Minute, Sound: "message-new-instant"},
	}

	got := Due([]model.Occurrence{event}, NewState(), rules, now)
	if len(got) != 1 || got[0].Before != 5*time.Minute {
		t.Fatalf("due reminders = %+v, want only the five-minute threshold", got)
	}
}

func TestDueCollapsesMirroredCopiesOfOneEvent(t *testing.T) {
	now := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	first := occurrence("shared-uid", now.Add(5*time.Minute))
	second := first
	second.AccountID = "other"
	second.CalendarID = "team"
	rules := []Rule{
		{Calendar: "personal/calendar", Before: 5 * time.Minute},
		{Calendar: "other/team", Before: 5 * time.Minute},
	}

	got := Due([]model.Occurrence{first, second}, NewState(), rules, now)
	if len(got) != 1 {
		t.Fatalf("mirrored event produced %d reminders: %+v", len(got), got)
	}
}

func TestDueHonoursDeduplicationAndDistinguishesInstances(t *testing.T) {
	now := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	first := occurrence("series", now.Add(5*time.Minute))
	first.RecurrenceID = "2026-06-10T12:01:00Z"
	second := occurrence("series", now.Add(5*time.Minute))
	second.RecurrenceID = "2026-06-10T12:02:00Z"
	state := NewState()
	state.Mark(first, 5*time.Minute)

	got := Due(
		[]model.Occurrence{first, second}, state,
		[]Rule{{Calendar: "personal/calendar", Before: 5 * time.Minute}}, now,
	)
	if len(got) != 1 || got[0].Occurrence.RecurrenceID != second.RecurrenceID {
		t.Fatalf("due occurrences = %+v, want only the second instance", got)
	}
}

func TestZeroBeforeStillAllowsTheStartGrace(t *testing.T) {
	now := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	started := occurrence("started", now.Add(-time.Minute))
	future := occurrence("future", now.Add(time.Nanosecond))

	rules := []Rule{{Calendar: "personal/calendar", Before: 0}}
	got := Due([]model.Occurrence{started, future}, NewState(), rules, now)
	if len(got) != 1 || got[0].Occurrence.UID != "started" {
		t.Fatalf("due occurrences = %+v, want only recently started", got)
	}
}

func TestStateRoundTripAndPrune(t *testing.T) {
	storage := store.New(t.TempDir())
	now := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	old := occurrence("old", now.Add(-48*time.Hour))
	keep := occurrence("keep", now.Add(time.Minute))
	state := NewState()
	state.Mark(old, 5*time.Minute)
	state.Mark(keep, 5*time.Minute)
	state.Mark(keep, 30*time.Minute)
	state.Prune(now.Add(-24 * time.Hour))
	if state.Has(old, 5*time.Minute) || !state.Has(keep, 5*time.Minute) || !state.Has(keep, 30*time.Minute) {
		t.Fatalf("state after prune = %+v", state.Notified)
	}
	if err := state.Save(storage); err != nil {
		t.Fatalf("Save: %v", err)
	}
	loaded, err := LoadState(storage)
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	if !loaded.Has(keep, 5*time.Minute) || !loaded.Has(keep, 30*time.Minute) {
		t.Fatal("round-tripped state lost the retained occurrence")
	}
	info, err := os.Stat(filepath.Join(storage.Root(), stateFileName))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("state permissions = %o, want 600", info.Mode().Perm())
	}
}

func TestLoadStateRejectsCorruptData(t *testing.T) {
	storage := store.New(t.TempDir())
	if err := os.WriteFile(filepath.Join(storage.Root(), stateFileName), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadState(storage); err == nil {
		t.Fatal("LoadState accepted corrupt data")
	}
}
