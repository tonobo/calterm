package model

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/emersion/go-ical"
)

func berlin(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		t.Skipf("tz database unavailable: %v", err)
	}
	return loc
}

func loadFixture(t *testing.T, name string) *ical.Calendar {
	t.Helper()
	f, err := os.Open(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	cal, err := ical.NewDecoder(f).Decode()
	if err != nil {
		t.Fatalf("decoding %s: %v", name, err)
	}
	return cal
}

// wide is a window large enough to contain every fixture.
func wide() Window {
	return Window{
		From: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		To:   time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC),
	}
}

func starts(occs []Occurrence, loc *time.Location) []string {
	out := make([]string, len(occs))
	for i, o := range occs {
		out[i] = o.Start.In(loc).Format("2006-01-02 15:04")
	}
	return out
}

func assertStarts(t *testing.T, got []Occurrence, loc *time.Location, want ...string) {
	t.Helper()
	g := starts(got, loc)
	if len(g) != len(want) {
		t.Fatalf("got %d occurrences %v, want %d %v", len(g), g, len(want), want)
	}
	for i := range want {
		if g[i] != want[i] {
			t.Errorf("occurrence %d start = %s, want %s (full: %v)", i, g[i], want[i], g)
		}
	}
}

func TestExpandSingleEvent(t *testing.T) {
	loc := berlin(t)
	got, err := Expand(loadFixture(t, "single.ics"), wide(), loc)
	if err != nil {
		t.Fatalf("Expand: %v", err)
	}
	assertStarts(t, got, loc, "2026-06-10 14:00")
	if got[0].Summary != "Dentist" {
		t.Errorf("summary = %q, want Dentist", got[0].Summary)
	}
	if got[0].Location != "Main Street 1" {
		t.Errorf("location = %q", got[0].Location)
	}
	if got[0].AllDay {
		t.Error("AllDay = true, want false")
	}
	if d := got[0].End.Sub(got[0].Start); d != time.Hour {
		t.Errorf("duration = %v, want 1h", d)
	}
	if got[0].RecurrenceID != "" {
		t.Errorf("RecurrenceID = %q, want empty for a non-recurring event", got[0].RecurrenceID)
	}
}

func TestExpandKeepsCategories(t *testing.T) {
	body := "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//calterm//test//EN\r\n" +
		"BEGIN:VEVENT\r\nUID:categories\r\nDTSTAMP:20260101T000000Z\r\n" +
		"DTSTART:20260610T100000Z\r\nDTEND:20260610T110000Z\r\nSUMMARY:Planning\r\n" +
		"CATEGORIES:Class A,Class B,Class A\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"

	got, err := Expand(decodeICS(t, body), wide(), time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || len(got[0].Categories) != 2 || got[0].Categories[0] != "Class A" || got[0].Categories[1] != "Class B" {
		t.Fatalf("categories = %+v", got)
	}
}

func TestExpandKeepsOrganizerAndAttendeeStatus(t *testing.T) {
	body := "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//calterm//test//EN\r\n" +
		"BEGIN:VEVENT\r\nUID:invite\r\nDTSTAMP:20260101T000000Z\r\n" +
		"DTSTART:20260610T100000Z\r\nDTEND:20260610T110000Z\r\nSUMMARY:Planning\r\n" +
		"ORGANIZER;CN=Organizer:mailto:organizer@example.com\r\n" +
		"ATTENDEE;CN=Organizer;PARTSTAT=ACCEPTED:mailto:organizer@example.com\r\n" +
		"ATTENDEE;CN=User;ROLE=REQ-PARTICIPANT:MAILTO:user%2Bwork@example.com\r\n" +
		"END:VEVENT\r\nEND:VCALENDAR\r\n"

	got, err := Expand(decodeICS(t, body), wide(), time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d occurrences, want 1", len(got))
	}
	o := &got[0]
	if o.Organizer == nil || o.Organizer.Name != "Organizer" || o.Organizer.Email != "organizer@example.com" {
		t.Fatalf("organizer = %+v", o.Organizer)
	}
	if len(o.Attendees) != 2 {
		t.Fatalf("attendees = %+v", o.Attendees)
	}
	if o.Attendees[1].Status != "NEEDS-ACTION" {
		t.Errorf("default PARTSTAT = %q, want NEEDS-ACTION", o.Attendees[1].Status)
	}
	o.MarkIdentity("user+work@example.com")
	if o.AttendeeStatus != "NEEDS-ACTION" || !o.Attendees[1].Self {
		t.Errorf("marked identity = status %q, attendees %+v", o.AttendeeStatus, o.Attendees)
	}
}

func TestExpandAllDayIsAnchoredToUTCMidnight(t *testing.T) {
	loc := berlin(t)
	got, err := Expand(loadFixture(t, "allday.ics"), wide(), loc)
	if err != nil {
		t.Fatalf("Expand: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d occurrences, want 1", len(got))
	}
	o := got[0]
	if !o.AllDay {
		t.Error("AllDay = false, want true")
	}
	want := time.Date(2026, 6, 14, 0, 0, 0, 0, time.UTC)
	if !o.Start.Equal(want) {
		t.Errorf("start = %s, want %s", o.Start, want)
	}
	if o.Start.Location() != time.UTC {
		t.Errorf("all-day start location = %v, want UTC", o.Start.Location())
	}
	if d := o.End.Sub(o.Start); d != 24*time.Hour {
		t.Errorf("duration = %v, want 24h", d)
	}
}

func TestExpandWeeklyWithExdate(t *testing.T) {
	loc := berlin(t)
	got, err := Expand(loadFixture(t, "weekly-exdate.ics"), wide(), loc)
	if err != nil {
		t.Fatalf("Expand: %v", err)
	}
	// COUNT=5 from Jun 1, weekly, with Jun 15 excluded.
	assertStarts(t, got, loc,
		"2026-06-01 10:00",
		"2026-06-08 10:00",
		"2026-06-22 10:00",
		"2026-06-29 10:00",
	)
}

func TestExpandOverrideMovesInstance(t *testing.T) {
	loc := berlin(t)
	got, err := Expand(loadFixture(t, "override-moved.ics"), wide(), loc)
	if err != nil {
		t.Fatalf("Expand: %v", err)
	}
	assertStarts(t, got, loc,
		"2026-06-01 10:00",
		"2026-06-08 12:00", // moved by the override, and re-sorted into place
		"2026-06-15 10:00",
		"2026-06-22 10:00",
	)
	if got[1].Summary != "Standup (moved)" {
		t.Errorf("moved instance summary = %q, want \"Standup (moved)\"", got[1].Summary)
	}
	if got[1].RecurrenceID == "" {
		t.Error("moved instance should carry a RecurrenceID")
	}
	if got[0].Summary != "Standup" {
		t.Errorf("unmoved instance summary = %q, want Standup", got[0].Summary)
	}
}

func TestExpandOverrideCancelsInstance(t *testing.T) {
	loc := berlin(t)
	got, err := Expand(loadFixture(t, "override-cancelled.ics"), wide(), loc)
	if err != nil {
		t.Fatalf("Expand: %v", err)
	}
	assertStarts(t, got, loc,
		"2026-06-01 10:00",
		"2026-06-15 10:00",
		"2026-06-22 10:00",
	)
}

func TestExpandKeepsWallClockAcrossDST(t *testing.T) {
	loc := berlin(t)
	got, err := Expand(loadFixture(t, "dst.ics"), wide(), loc)
	if err != nil {
		t.Fatalf("Expand: %v", err)
	}
	// Europe/Berlin springs forward on 2026-03-29. The wall-clock hour must
	// stay at 10:00 on every instance; the UTC offset is what moves.
	assertStarts(t, got, loc,
		"2026-03-27 10:00",
		"2026-03-28 10:00",
		"2026-03-29 10:00",
		"2026-03-30 10:00",
		"2026-03-31 10:00",
	)
	if _, offBefore := got[0].Start.Zone(); true {
		_, offAfter := got[4].Start.Zone()
		if offBefore == offAfter {
			t.Errorf("expected the UTC offset to change across the DST boundary, both were %d", offBefore)
		}
	}
}

func TestExpandRDateAddsInstance(t *testing.T) {
	loc := berlin(t)
	got, err := Expand(loadFixture(t, "rdate.ics"), wide(), loc)
	if err != nil {
		t.Fatalf("Expand: %v", err)
	}
	assertStarts(t, got, loc,
		"2026-06-01 10:00",
		"2026-06-08 10:00",
		"2026-06-20 16:00",
	)
}

func TestExpandDurationAndUTCUntil(t *testing.T) {
	loc := berlin(t)
	got, err := Expand(loadFixture(t, "duration.ics"), wide(), loc)
	if err != nil {
		t.Fatalf("Expand: %v", err)
	}
	// UNTIL=20260616T080000Z is 10:00 Berlin, so Jun 15 is the last instance.
	assertStarts(t, got, loc,
		"2026-06-01 10:00",
		"2026-06-08 10:00",
		"2026-06-15 10:00",
	)
	if d := got[0].End.Sub(got[0].Start); d != 45*time.Minute {
		t.Errorf("duration = %v, want 45m", d)
	}
}

func TestExpandSkipsMalformedEventWithoutFailing(t *testing.T) {
	loc := berlin(t)
	got, err := Expand(loadFixture(t, "malformed.ics"), wide(), loc)
	if err != nil {
		t.Fatalf("a malformed event must not fail the whole calendar, got %v", err)
	}
	assertStarts(t, got, loc, "2026-06-10 09:00")
}

func TestExpandWindowClipping(t *testing.T) {
	loc := berlin(t)
	win := Window{
		From: time.Date(2026, 6, 8, 0, 0, 0, 0, time.UTC),
		To:   time.Date(2026, 6, 16, 0, 0, 0, 0, time.UTC),
	}
	got, err := Expand(loadFixture(t, "weekly-exdate.ics"), win, loc)
	if err != nil {
		t.Fatalf("Expand: %v", err)
	}
	assertStarts(t, got, loc, "2026-06-08 10:00")
}

// An event that began before the window but is still running must appear.
func TestExpandIncludesEventOverlappingWindowStart(t *testing.T) {
	loc := berlin(t)
	win := Window{
		From: time.Date(2026, 6, 10, 12, 30, 0, 0, loc),
		To:   time.Date(2026, 6, 11, 0, 0, 0, 0, loc),
	}
	got, err := Expand(loadFixture(t, "single.ics"), win, loc)
	if err != nil {
		t.Fatalf("Expand: %v", err)
	}
	assertStarts(t, got, loc, "2026-06-10 14:00")
}

func TestRecurrenceKeyIsTimezoneIndependent(t *testing.T) {
	loc := berlin(t)
	a := time.Date(2026, 6, 8, 10, 0, 0, 0, loc)
	b := a.UTC()
	if RecurrenceKey(a) != RecurrenceKey(b) {
		t.Errorf("RecurrenceKey differs by representation: %q vs %q", RecurrenceKey(a), RecurrenceKey(b))
	}
}

func TestOverlaps(t *testing.T) {
	base := time.Date(2026, 6, 10, 14, 0, 0, 0, time.UTC)
	o := Occurrence{Start: base, End: base.Add(time.Hour)}
	tests := []struct {
		name     string
		from, to time.Time
		want     bool
	}{
		{"fully inside", base.Add(-time.Hour), base.Add(2 * time.Hour), true},
		{"overlaps start", base.Add(30 * time.Minute), base.Add(2 * time.Hour), true},
		{"overlaps end", base.Add(-time.Hour), base.Add(30 * time.Minute), true},
		{"entirely before", base.Add(-3 * time.Hour), base.Add(-time.Hour), false},
		{"entirely after", base.Add(2 * time.Hour), base.Add(3 * time.Hour), false},
		{"touching end is exclusive", base.Add(time.Hour), base.Add(2 * time.Hour), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := o.Overlaps(tt.from, tt.to); got != tt.want {
				t.Errorf("Overlaps = %v, want %v", got, tt.want)
			}
		})
	}
}

// --- RANGE=THISANDFUTURE ---------------------------------------------------
//
// The most intricate hand-rolled logic in this package, and the one with the
// least room for guessing: RFC 5545 defines RANGE=THISANDFUTURE against the
// RECURRENCE SET -- the instance identified by the RECURRENCE-ID and every
// later instance of the series -- not against instances as some earlier
// override may already have moved them.

func decodeICS(t *testing.T, body string) *ical.Calendar {
	t.Helper()
	cal, err := ical.NewDecoder(strings.NewReader(body)).Decode()
	if err != nil {
		t.Fatalf("decoding: %v", err)
	}
	return cal
}

// dailySeries is five daily instances at 10:00 UTC from 2026-06-01, plus
// whatever override components the test appends.
func dailySeries(overrides ...string) string {
	return "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//calterm//test//EN\r\n" +
		"BEGIN:VEVENT\r\nUID:series\r\nDTSTAMP:20260101T000000Z\r\n" +
		"DTSTART:20260601T100000Z\r\nDTEND:20260601T103000Z\r\n" +
		"RRULE:FREQ=DAILY;COUNT=5\r\nSUMMARY:Standup\r\nEND:VEVENT\r\n" +
		strings.Join(overrides, "") + "END:VCALENDAR\r\n"
}

// thisAndFuture moves the instance at rid, and every later one, by adopting
// newStart (and the given summary).
func thisAndFuture(rid, newStart, newEnd, summary string) string {
	return "BEGIN:VEVENT\r\nUID:series\r\nDTSTAMP:20260101T000000Z\r\n" +
		"RECURRENCE-ID;RANGE=THISANDFUTURE:" + rid + "\r\n" +
		"DTSTART:" + newStart + "\r\nDTEND:" + newEnd + "\r\n" +
		"SUMMARY:" + summary + "\r\nEND:VEVENT\r\n"
}

// single overrides exactly one instance.
func single(rid, newStart, newEnd, summary string) string {
	return "BEGIN:VEVENT\r\nUID:series\r\nDTSTAMP:20260101T000000Z\r\n" +
		"RECURRENCE-ID:" + rid + "\r\n" +
		"DTSTART:" + newStart + "\r\nDTEND:" + newEnd + "\r\n" +
		"SUMMARY:" + summary + "\r\nEND:VEVENT\r\n"
}

func TestThisAndFutureShiftsThisAndEveryLaterInstance(t *testing.T) {
	cal := decodeICS(t, dailySeries(
		thisAndFuture("20260603T100000Z", "20260603T120000Z", "20260603T123000Z", "Standup (moved to noon)"),
	))
	got, err := Expand(cal, wide(), time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	assertStarts(t, got, time.UTC,
		"2026-06-01 10:00",
		"2026-06-02 10:00",
		"2026-06-03 12:00",
		"2026-06-04 12:00",
		"2026-06-05 12:00",
	)
	for _, o := range got[2:] {
		if o.Summary != "Standup (moved to noon)" {
			t.Errorf("instance at %s kept summary %q, want the override's", o.Start.Format(time.RFC3339), o.Summary)
		}
	}
	if got[1].Summary != "Standup" {
		t.Errorf("instance before the range has summary %q, want the master's", got[1].Summary)
	}
}

func TestThisAndFutureCancellationRemovesThisAndEveryLaterInstance(t *testing.T) {
	body := dailySeries()
	body = strings.Replace(body, "END:VCALENDAR",
		"BEGIN:VEVENT\r\nUID:series\r\nDTSTAMP:20260101T000000Z\r\n"+
			"RECURRENCE-ID;RANGE=THISANDFUTURE:20260604T100000Z\r\n"+
			"DTSTART:20260604T100000Z\r\nDTEND:20260604T103000Z\r\n"+
			"STATUS:CANCELLED\r\nSUMMARY:Standup\r\nEND:VEVENT\r\nEND:VCALENDAR", 1)
	got, err := Expand(decodeICS(t, body), wide(), time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	assertStarts(t, got, time.UTC,
		"2026-06-01 10:00",
		"2026-06-02 10:00",
		"2026-06-03 10:00",
	)
}

// The case that exposes the selection rule. The Jun 2 instance was moved by
// its own single-instance override to Jun 4 -- past the THISANDFUTURE
// boundary at Jun 3. Selecting by the instance's CURRENT start sweeps it into
// the range and silently discards its override; selecting by its recurrence
// key (its ORIGINAL time, which is what RANGE=THISANDFUTURE is defined
// against) correctly leaves it alone.
func TestThisAndFutureSelectsByRecurrenceKeyNotCurrentStart(t *testing.T) {
	cal := decodeICS(t, dailySeries(
		single("20260602T100000Z", "20260604T090000Z", "20260604T093000Z", "Moved to Thursday"),
		thisAndFuture("20260603T100000Z", "20260603T120000Z", "20260603T123000Z", "Noon from here on"),
	))
	got, err := Expand(cal, wide(), time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	assertStarts(t, got, time.UTC,
		"2026-06-01 10:00",
		"2026-06-03 12:00",
		"2026-06-04 09:00", // the moved Jun 2 instance, untouched
		"2026-06-04 12:00",
		"2026-06-05 12:00",
	)
	for _, o := range got {
		if o.Start.Equal(time.Date(2026, 6, 4, 9, 0, 0, 0, time.UTC)) && o.Summary != "Moved to Thursday" {
			t.Errorf("the Jun 2 instance lost its own override: summary = %q", o.Summary)
		}
	}
}

// The mirror image: an instance moved EARLIER than the boundary must still be
// swept in, because its recurrence key is inside the range.
func TestThisAndFutureSweepsAnInstanceMovedBeforeTheBoundary(t *testing.T) {
	cal := decodeICS(t, dailySeries(
		single("20260605T100000Z", "20260602T090000Z", "20260602T093000Z", "Pulled forward"),
		thisAndFuture("20260603T100000Z", "20260603T120000Z", "20260603T123000Z", "Noon from here on"),
	))
	got, err := Expand(cal, wide(), time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	assertStarts(t, got, time.UTC,
		"2026-06-01 10:00",
		"2026-06-02 10:00",
		"2026-06-03 12:00",
		"2026-06-04 12:00",
		"2026-06-05 12:00",
	)
}

// Overrides are applied in document order over a mutating map, so the result
// used to depend on the order the server happened to serialise them in. With
// selection keyed on the recurrence set, two overrides whose ranges do not
// overlap give the same answer either way round.
func TestThisAndFutureIsIndependentOfDocumentOrder(t *testing.T) {
	sing := single("20260602T100000Z", "20260604T090000Z", "20260604T093000Z", "Moved to Thursday")
	taf := thisAndFuture("20260603T100000Z", "20260603T120000Z", "20260603T123000Z", "Noon from here on")

	first, err := Expand(decodeICS(t, dailySeries(sing, taf)), wide(), time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Expand(decodeICS(t, dailySeries(taf, sing)), wide(), time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	a, b := starts(first, time.UTC), starts(second, time.UTC)
	if len(a) != len(b) {
		t.Fatalf("order changed the occurrence count: %v vs %v", a, b)
	}
	for i := range a {
		if a[i] != b[i] {
			t.Errorf("occurrence %d differs by document order: %s vs %s (%v vs %v)", i, a[i], b[i], a, b)
		}
	}
}

// An object may contain only RECURRENCE-ID components, with no master: the
// series lives in another object, or the master was deleted. Those overrides
// still describe real events and must be expanded on their own.
func TestOrphanOverridesWithNoMasterStillExpand(t *testing.T) {
	body := "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//calterm//test//EN\r\n" +
		single("20260602T100000Z", "20260602T140000Z", "20260602T150000Z", "Rescheduled") +
		"BEGIN:VEVENT\r\nUID:series\r\nDTSTAMP:20260101T000000Z\r\n" +
		"RECURRENCE-ID:20260603T100000Z\r\nDTSTART:20260603T100000Z\r\nDTEND:20260603T103000Z\r\n" +
		"STATUS:CANCELLED\r\nSUMMARY:Dropped\r\nEND:VEVENT\r\n" +
		"END:VCALENDAR\r\n"

	got, err := Expand(decodeICS(t, body), wide(), time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	assertStarts(t, got, time.UTC, "2026-06-02 14:00")
	if got[0].Summary != "Rescheduled" {
		t.Errorf("summary = %q, want Rescheduled", got[0].Summary)
	}
	if got[0].RecurrenceID != RecurrenceKey(time.Date(2026, 6, 2, 10, 0, 0, 0, time.UTC)) {
		t.Errorf("RecurrenceID = %q, want the ORIGINAL recurrence instant", got[0].RecurrenceID)
	}
}
