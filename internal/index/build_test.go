package index

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/tonobo/calterm/internal/store"
)

const standupICS = `BEGIN:VCALENDAR
VERSION:2.0
PRODID:-//calterm//test//EN
BEGIN:VEVENT
UID:standup-1
DTSTAMP:20260101T000000Z
DTSTART;TZID=Europe/Berlin:20260610T100000
DTEND;TZID=Europe/Berlin:20260610T103000
SUMMARY:Standup
END:VEVENT
END:VCALENDAR
`

const holidayICS = `BEGIN:VCALENDAR
VERSION:2.0
PRODID:-//calterm//test//EN
BEGIN:VEVENT
UID:holiday-1
DTSTAMP:20260101T000000Z
DTSTART;VALUE=DATE:20260614
DTEND;VALUE=DATE:20260615
SUMMARY:Holiday
END:VEVENT
END:VCALENDAR
`

const garbageICS = "this is not iCalendar at all\n"

// testNow is the instant the fixture accounts last synced successfully; the
// index's GeneratedAt is stamped from AccountMeta.LastSync, not from the
// rebuild's clock.
var testNow = time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)

func testMeta() *store.Meta {
	return &store.Meta{
		Accounts: []store.AccountMeta{{
			Name:     "personal",
			LastSync: testNow,
			Calendars: []store.CalendarMeta{
				{ID: "work", Path: "/dav/work/", Name: "Work", Color: "#3366ff"},
			},
		}},
	}
}

// setup writes each object to disk AND registers it in the calendar's index,
// which is what Build reads: the index is the single authority on what a
// calendar contains.
func setup(t *testing.T, objects map[string]string) *store.Store {
	t.Helper()
	s := store.New(t.TempDir())
	idx := &store.Index{CTag: "ctag-1", Objects: map[string]store.ObjectRef{}}
	for uid, body := range objects {
		if err := s.WriteObject("personal", "work", uid, []byte(body)); err != nil {
			t.Fatal(err)
		}
		href := "/dav/work/" + uid + ".ics"
		idx.Objects[href] = store.ObjectRef{Href: href, ETag: "etag-" + uid, UID: uid}
	}
	if err := s.SaveIndex("personal", "work", idx); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestBuildExpandsCachedObjects(t *testing.T) {
	loc, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		t.Skipf("tz database unavailable: %v", err)
	}
	s := setup(t, map[string]string{"standup-1": standupICS, "holiday-1": holidayICS})
	now := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)

	idx, err := Build(s, testMeta(), now, loc)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(idx.Occurrences) != 2 {
		t.Fatalf("got %d occurrences, want 2", len(idx.Occurrences))
	}
	// Sorted by start: Standup (Jun 10) before Holiday (Jun 14).
	if idx.Occurrences[0].Summary != "Standup" {
		t.Errorf("first = %q, want Standup", idx.Occurrences[0].Summary)
	}
	if idx.Occurrences[1].Summary != "Holiday" {
		t.Errorf("second = %q, want Holiday", idx.Occurrences[1].Summary)
	}
	if !idx.GeneratedAt.Equal(now) {
		t.Errorf("GeneratedAt = %v, want %v", idx.GeneratedAt, now)
	}
}

func TestBuildStampsAccountAndCalendar(t *testing.T) {
	s := setup(t, map[string]string{"standup-1": standupICS})
	idx, err := Build(s, testMeta(), time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), time.UTC)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	o := idx.Occurrences[0]
	if o.AccountID != "personal" {
		t.Errorf("AccountID = %q, want personal", o.AccountID)
	}
	if o.CalendarID != "work" {
		t.Errorf("CalendarID = %q, want work", o.CalendarID)
	}
}

// One unreadable object must not take down the whole index.
func TestBuildSkipsGarbageObject(t *testing.T) {
	s := setup(t, map[string]string{"standup-1": standupICS, "garbage-1": garbageICS})
	idx, err := Build(s, testMeta(), time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), time.UTC)
	if err != nil {
		t.Fatalf("Build must tolerate an unparseable object, got %v", err)
	}
	if len(idx.Occurrences) != 1 {
		t.Fatalf("got %d occurrences, want 1", len(idx.Occurrences))
	}
	if idx.Occurrences[0].Summary != "Standup" {
		t.Errorf("surviving occurrence = %q, want Standup", idx.Occurrences[0].Summary)
	}
}

func TestBuildOnEmptyStore(t *testing.T) {
	s := store.New(t.TempDir())
	idx, err := Build(s, testMeta(), time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), time.UTC)
	if err != nil {
		t.Fatalf("Build on an empty store: %v", err)
	}
	if len(idx.Occurrences) != 0 {
		t.Errorf("got %d occurrences, want 0", len(idx.Occurrences))
	}
}

func TestDefaultWindow(t *testing.T) {
	now := time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)
	w := DefaultWindow(now)
	wantFrom := time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC)
	wantTo := time.Date(2027, 6, 15, 12, 0, 0, 0, time.UTC)
	if !w.From.Equal(wantFrom) {
		t.Errorf("From = %v, want %v", w.From, wantFrom)
	}
	if !w.To.Equal(wantTo) {
		t.Errorf("To = %v, want %v", w.To, wantTo)
	}
}

// Build reads the calendar's index, not the objects directory, so anything on
// disk that the index does not reference is inert: a stray file, or -- the
// case that motivated this -- an .ics orphaned when a server rewrote an href
// with a new UID. Enumerating the directory made such an orphan a ghost event
// that reappeared on every rebuild and that no sync could ever remove.
func TestBuildIgnoresFilesTheIndexDoesNotReference(t *testing.T) {
	s := setup(t, map[string]string{"standup-1": standupICS})
	dir := filepath.Join(s.CalendarDir("personal", "work"), "objects")
	if err := os.WriteFile(filepath.Join(dir, "README.txt"), []byte("ignore me"), 0o600); err != nil {
		t.Fatal(err)
	}
	// An orphaned object: a real, parseable .ics that the index has forgotten.
	if err := s.WriteObject("personal", "work", "holiday-1", []byte(holidayICS)); err != nil {
		t.Fatal(err)
	}

	idx, err := Build(s, testMeta(), time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), time.UTC)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	var got []string
	for _, o := range idx.Occurrences {
		got = append(got, o.Summary)
	}
	if len(idx.Occurrences) != 1 || idx.Occurrences[0].Summary != "Standup" {
		t.Errorf("occurrences = %v, want just [Standup]; the orphaned file must be inert", got)
	}
}

// GeneratedAt is stamped from the OLDEST account LastSync, not from the
// rebuild's clock: freshness is earned by a successful fetch. A healthy
// account must not vouch for one that has been failing for days, and an
// account that has never synced makes the whole index count as never
// generated, which Age reports as maximally stale.
func TestBuildStampsGeneratedAtFromOldestAccountLastSync(t *testing.T) {
	fresh := time.Date(2026, 6, 20, 12, 0, 0, 0, time.UTC)
	stale := time.Date(2026, 6, 17, 9, 0, 0, 0, time.UTC)
	rebuild := time.Date(2026, 6, 20, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name     string
		accounts []store.AccountMeta
		want     time.Time
	}{
		{"single healthy account", []store.AccountMeta{{Name: "a", LastSync: fresh}}, fresh},
		{"oldest wins", []store.AccountMeta{{Name: "a", LastSync: fresh}, {Name: "b", LastSync: stale}}, stale},
		{"oldest wins regardless of order", []store.AccountMeta{{Name: "b", LastSync: stale}, {Name: "a", LastSync: fresh}}, stale},
		{"a never-synced account is maximally stale", []store.AccountMeta{{Name: "a", LastSync: fresh}, {Name: "b"}}, time.Time{}},
		{"no accounts at all", nil, time.Time{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := store.New(t.TempDir())
			idx, err := Build(s, &store.Meta{Accounts: tt.accounts}, rebuild, time.UTC)
			if err != nil {
				t.Fatalf("Build: %v", err)
			}
			if !idx.GeneratedAt.Equal(tt.want) {
				t.Errorf("GeneratedAt = %v, want %v", idx.GeneratedAt, tt.want)
			}
		})
	}
}
