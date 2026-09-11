package eventopen

import (
	"testing"
	"time"

	"github.com/tonobo/calterm/internal/config"
	"github.com/tonobo/calterm/internal/model"
	"github.com/tonobo/calterm/internal/store"
)

func TestMatchUsesUIDAndRecurrenceID(t *testing.T) {
	wanted := time.Date(2026, 9, 15, 11, 0, 0, 0, time.UTC)
	ref := model.EventReference{Occurrence: model.Occurrence{
		UID: "series", RecurrenceID: model.RecurrenceKey(wanted), Start: wanted.Add(time.Hour),
	}, Recurring: true}
	idx := &store.OccurrenceIndex{Occurrences: []model.Occurrence{
		{UID: "series", RecurrenceID: model.RecurrenceKey(wanted.AddDate(0, 0, -7)), Start: wanted.AddDate(0, 0, -7), Summary: "wrong"},
		{UID: "series", RecurrenceID: model.RecurrenceKey(wanted), Start: wanted.Add(time.Hour), Summary: "right"},
	}}

	got, ok := Match(ref, idx, nil)
	if !ok || got.Summary != "right" {
		t.Fatalf("Match = %+v, %v", got, ok)
	}
}

func TestMatchRecurringEventFallsBackToNearestDTSTART(t *testing.T) {
	start := time.Date(2026, 9, 15, 11, 0, 0, 0, time.UTC)
	ref := model.EventReference{Occurrence: model.Occurrence{UID: "series", Start: start}, Recurring: true}
	idx := &store.OccurrenceIndex{Occurrences: []model.Occurrence{
		{UID: "series", RecurrenceID: model.RecurrenceKey(start.AddDate(0, 0, -7)), Start: start.AddDate(0, 0, -7), Summary: "previous"},
		{UID: "series", RecurrenceID: model.RecurrenceKey(start), Start: start, Summary: "nearest"},
		{UID: "series", RecurrenceID: model.RecurrenceKey(start.AddDate(0, 0, 7)), Start: start.AddDate(0, 0, 7), Summary: "next"},
	}}

	got, ok := Match(ref, idx, nil)
	if !ok || got.Summary != "nearest" {
		t.Fatalf("Match = %+v, %v", got, ok)
	}
}

func TestMatchPrefersAccountWhoseAttendeeIdentityIsInvited(t *testing.T) {
	start := time.Date(2026, 9, 15, 11, 0, 0, 0, time.UTC)
	ref := model.EventReference{Occurrence: model.Occurrence{
		UID: "duplicate", Start: start,
		Attendees: []model.Participant{{Email: "user@work.example"}},
	}}
	idx := &store.OccurrenceIndex{Occurrences: []model.Occurrence{
		{UID: "duplicate", Start: start, AccountID: "personal", Summary: "personal copy"},
		{UID: "duplicate", Start: start, AccountID: "work", Summary: "work copy"},
	}}
	cfg := &config.Config{Accounts: []config.Account{
		{Name: "personal", Email: "user@personal.example"},
		{Name: "work", Email: "user@work.example"},
	}}

	got, ok := Match(ref, idx, cfg)
	if !ok || got.AccountID != "work" {
		t.Fatalf("Match = %+v, %v", got, ok)
	}
}

func TestMatchDoesNotUseUIDOnlyForNonRecurringEvent(t *testing.T) {
	ref := model.EventReference{Occurrence: model.Occurrence{UID: "one-off", RecurrenceID: "missing"}}
	idx := &store.OccurrenceIndex{Occurrences: []model.Occurrence{{UID: "one-off", RecurrenceID: "different"}}}
	if got, ok := Match(ref, idx, nil); ok {
		t.Fatalf("Match unexpectedly returned %+v", got)
	}
}
