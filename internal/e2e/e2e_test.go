package e2e

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/tonobo/calterm/internal/caldav"
	"github.com/tonobo/calterm/internal/config"
	"github.com/tonobo/calterm/internal/index"
	"github.com/tonobo/calterm/internal/store"
	"github.com/tonobo/calterm/internal/sync"
	"github.com/tonobo/calterm/internal/waybar"
)

// A weekly standup with one instance cancelled, which exercises expansion,
// overrides, and the waybar "next event" selection together.
const seriesICS = `BEGIN:VCALENDAR
VERSION:2.0
PRODID:-//calterm//e2e//EN
BEGIN:VEVENT
UID:standup
DTSTAMP:20260101T000000Z
DTSTART;TZID=Europe/Berlin:20260601T100000
DTEND;TZID=Europe/Berlin:20260601T103000
RRULE:FREQ=WEEKLY;COUNT=4
SUMMARY:Standup
LOCATION:Room 2
END:VEVENT
BEGIN:VEVENT
UID:standup
DTSTAMP:20260101T000000Z
RECURRENCE-ID;TZID=Europe/Berlin:20260608T100000
DTSTART;TZID=Europe/Berlin:20260608T100000
DTEND;TZID=Europe/Berlin:20260608T103000
STATUS:CANCELLED
SUMMARY:Standup
END:VEVENT
END:VCALENDAR
`

func server(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := new(strings.Builder)
		buf := make([]byte, 1<<16)
		n, _ := r.Body.Read(buf)
		body.Write(buf[:n])

		w.Header().Set("Content-Type", "application/xml")
		w.WriteHeader(http.StatusMultiStatus)

		switch {
		case r.Method == "PROPFIND" && r.Header.Get("Depth") == "0":
			fmt.Fprint(w, `<?xml version="1.0"?>
<d:multistatus xmlns:d="DAV:" xmlns:cs="http://calendarserver.org/ns/">
  <d:response><d:href>/dav/work/</d:href><d:propstat>
    <d:prop><d:displayname>Work</d:displayname><cs:getctag>v1</cs:getctag></d:prop>
    <d:status>HTTP/1.1 200 OK</d:status>
  </d:propstat></d:response>
</d:multistatus>`)
		case r.Method == "PROPFIND":
			fmt.Fprint(w, `<?xml version="1.0"?>
<d:multistatus xmlns:d="DAV:">
  <d:response><d:href>/dav/work/standup.ics</d:href><d:propstat>
    <d:prop><d:getetag>"e1"</d:getetag></d:prop>
    <d:status>HTTP/1.1 200 OK</d:status>
  </d:propstat></d:response>
</d:multistatus>`)
		case r.Method == "REPORT":
			fmt.Fprintf(w, `<?xml version="1.0"?>
<d:multistatus xmlns:d="DAV:" xmlns:c="urn:ietf:params:xml:ns:caldav">
  <d:response><d:href>/dav/work/standup.ics</d:href><d:propstat>
    <d:prop><d:getetag>"e1"</d:getetag><c:calendar-data>%s</c:calendar-data></d:prop>
    <d:status>HTTP/1.1 200 OK</d:status>
  </d:propstat></d:response>
</d:multistatus>`, seriesICS)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestSyncThenWaybar(t *testing.T) {
	loc, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		t.Skipf("tz database unavailable: %v", err)
	}
	srv := server(t)
	s := store.New(t.TempDir())
	c, err := caldav.NewClient(srv.URL, "u", "p", false)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := sync.Calendar(context.Background(), c, s, "personal", "work", "/dav/work/"); err != nil {
		t.Fatalf("sync: %v", err)
	}

	// One hour before the first occurrence's 10:00 Europe/Berlin start
	// (08:00 UTC in CEST), so the event is "upcoming" rather than "now".
	now := time.Date(2026, 6, 1, 7, 0, 0, 0, time.UTC)
	// LastSync is what the index's freshness is stamped from: the account
	// just synced successfully, so it is `now`.
	meta := &store.Meta{Accounts: []store.AccountMeta{{
		Name: "personal", LastSync: now,
		Calendars: []store.CalendarMeta{{ID: "work", Name: "Work"}},
	}}}
	idx, err := index.Build(s, meta, now, loc)
	if err != nil {
		t.Fatalf("index.Build: %v", err)
	}

	// COUNT=4 with one instance cancelled leaves three.
	if len(idx.Occurrences) != 3 {
		var got []string
		for _, o := range idx.Occurrences {
			got = append(got, o.Start.In(loc).Format("2006-01-02 15:04"))
		}
		t.Fatalf("got %d occurrences %v, want 3 (COUNT=4 minus one cancelled)", len(idx.Occurrences), got)
	}
	if err := s.SaveOccurrences(idx); err != nil {
		t.Fatal(err)
	}

	cfg := config.WaybarConfig{
		LeadTime: 15 * time.Minute, StaleAfter: 2 * time.Hour,
		TextFormat: "{start} {summary}", TooltipFormat: "{summary}\n{location}",
	}
	out := waybar.Render(idx, meta, cfg, nil, now, loc)
	if out.Class != waybar.ClassUpcoming {
		t.Errorf("class = %q, want upcoming", out.Class)
	}
	if !strings.Contains(out.Text, "Standup") {
		t.Errorf("text = %q, want it to name the next Standup", out.Text)
	}

	// A second sync must be a no-op: the ctag has not moved.
	res, err := sync.Calendar(context.Background(), c, s, "personal", "work", "/dav/work/")
	if err != nil {
		t.Fatalf("second sync: %v", err)
	}
	if !res.Unchanged {
		t.Errorf("second sync fetched %d objects, want an unchanged no-op", res.Fetched)
	}
}
