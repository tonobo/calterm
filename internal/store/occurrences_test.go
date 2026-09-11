package store

import (
	"testing"
	"time"

	"github.com/tonobo/calterm/internal/model"
)

func TestLoadOccurrencesMissingReturnsEmpty(t *testing.T) {
	s := New(t.TempDir())
	idx, err := s.LoadOccurrences()
	if err != nil {
		t.Fatalf("LoadOccurrences on a fresh store: %v", err)
	}
	if len(idx.Occurrences) != 0 {
		t.Errorf("got %d occurrences, want 0", len(idx.Occurrences))
	}
	if !idx.GeneratedAt.IsZero() {
		t.Errorf("GeneratedAt = %v, want zero for a missing index", idx.GeneratedAt)
	}
}

func TestOccurrenceIndexRoundTrip(t *testing.T) {
	s := New(t.TempDir())
	gen := time.Date(2026, 6, 10, 8, 0, 0, 0, time.UTC)
	want := &OccurrenceIndex{
		GeneratedAt: gen,
		From:        gen.AddDate(0, -1, 0),
		To:          gen.AddDate(1, 0, 0),
		Occurrences: []model.Occurrence{{
			UID:        "a",
			Summary:    "Standup",
			Start:      time.Date(2026, 6, 10, 9, 0, 0, 0, time.UTC),
			End:        time.Date(2026, 6, 10, 9, 30, 0, 0, time.UTC),
			CalendarID: "work",
			AccountID:  "personal",
		}},
	}
	if err := s.SaveOccurrences(want); err != nil {
		t.Fatalf("SaveOccurrences: %v", err)
	}
	got, err := s.LoadOccurrences()
	if err != nil {
		t.Fatalf("LoadOccurrences: %v", err)
	}
	if len(got.Occurrences) != 1 {
		t.Fatalf("got %d occurrences, want 1", len(got.Occurrences))
	}
	o := got.Occurrences[0]
	if o.Summary != "Standup" || o.CalendarID != "work" || o.AccountID != "personal" {
		t.Errorf("round trip lost fields: %+v", o)
	}
	if !o.Start.Equal(want.Occurrences[0].Start) {
		t.Errorf("start = %v, want %v", o.Start, want.Occurrences[0].Start)
	}
	if !got.GeneratedAt.Equal(gen) {
		t.Errorf("GeneratedAt = %v, want %v", got.GeneratedAt, gen)
	}
}

func TestOccurrenceIndexAge(t *testing.T) {
	gen := time.Date(2026, 6, 10, 8, 0, 0, 0, time.UTC)
	idx := &OccurrenceIndex{GeneratedAt: gen}
	if got := idx.Age(gen.Add(90 * time.Minute)); got != 90*time.Minute {
		t.Errorf("Age = %v, want 90m", got)
	}
	// A never-generated index is infinitely stale, not zero-age.
	empty := &OccurrenceIndex{}
	if got := empty.Age(gen); got <= 0 {
		t.Errorf("Age of an ungenerated index = %v, want a large positive duration", got)
	}
}

func TestCorruptOccurrenceIndexIsTreatedAsEmpty(t *testing.T) {
	s := New(t.TempDir())
	if err := WriteFileAtomic(s.occurrencesPath(), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	idx, err := s.LoadOccurrences()
	if err != nil {
		t.Fatalf("a corrupt index must not error, got %v", err)
	}
	if len(idx.Occurrences) != 0 {
		t.Errorf("got %d occurrences, want 0", len(idx.Occurrences))
	}
}

func TestMetaRoundTrip(t *testing.T) {
	s := New(t.TempDir())
	last := time.Date(2026, 6, 10, 8, 0, 0, 0, time.UTC)
	want := &Meta{
		LastSync: last,
		Accounts: []AccountMeta{{
			Name: "personal",
			Calendars: []CalendarMeta{
				{ID: "work", Path: "/dav/cal/work/", Name: "Work", Color: "#ff0000"},
			},
		}},
	}
	if err := s.SaveMeta(want); err != nil {
		t.Fatalf("SaveMeta: %v", err)
	}
	got, err := s.LoadMeta()
	if err != nil {
		t.Fatalf("LoadMeta: %v", err)
	}
	if len(got.Accounts) != 1 || len(got.Accounts[0].Calendars) != 1 {
		t.Fatalf("meta round trip lost structure: %+v", got)
	}
	cal := got.Accounts[0].Calendars[0]
	if cal.Name != "Work" || cal.Color != "#ff0000" || cal.Path != "/dav/cal/work/" {
		t.Errorf("calendar meta = %+v", cal)
	}
	if !got.LastSync.Equal(last) {
		t.Errorf("LastSync = %v, want %v", got.LastSync, last)
	}
}

func TestLoadMetaMissingReturnsEmpty(t *testing.T) {
	s := New(t.TempDir())
	got, err := s.LoadMeta()
	if err != nil {
		t.Fatalf("LoadMeta on a fresh store: %v", err)
	}
	if len(got.Accounts) != 0 {
		t.Errorf("got %d accounts, want 0", len(got.Accounts))
	}
}
