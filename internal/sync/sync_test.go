package sync

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/tonobo/calterm/internal/caldav"
	"github.com/tonobo/calterm/internal/index"
	"github.com/tonobo/calterm/internal/store"
)

// fakeServer serves a mutable set of calendar objects with a ctag that the
// test controls, which is exactly the surface sync depends on.
type fakeServer struct {
	ctag    string
	objects map[string]string // href -> ics body
	etags   map[string]string // href -> etag
	calls   []string
}

func (f *fakeServer) handler(t *testing.T) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := new(strings.Builder)
		buf := make([]byte, 1<<16)
		n, _ := r.Body.Read(buf)
		body.Write(buf[:n])

		w.Header().Set("Content-Type", "application/xml")
		w.WriteHeader(http.StatusMultiStatus)

		switch {
		case r.Method == "PROPFIND" && r.Header.Get("Depth") == "0":
			f.calls = append(f.calls, "ctag")
			fmt.Fprintf(w, `<?xml version="1.0"?>
<d:multistatus xmlns:d="DAV:" xmlns:cs="http://calendarserver.org/ns/">
  <d:response><d:href>/dav/work/</d:href><d:propstat>
    <d:prop><d:displayname>Work</d:displayname><cs:getctag>%s</cs:getctag></d:prop>
    <d:status>HTTP/1.1 200 OK</d:status>
  </d:propstat></d:response>
</d:multistatus>`, f.ctag)

		case r.Method == "PROPFIND":
			f.calls = append(f.calls, "etags")
			var sb strings.Builder
			sb.WriteString(`<?xml version="1.0"?><d:multistatus xmlns:d="DAV:">`)
			sb.WriteString(`<d:response><d:href>/dav/work/</d:href><d:propstat><d:prop><d:resourcetype><d:collection/></d:resourcetype></d:prop><d:status>HTTP/1.1 200 OK</d:status></d:propstat></d:response>`)
			for href := range f.objects {
				fmt.Fprintf(&sb, `<d:response><d:href>%s</d:href><d:propstat><d:prop><d:getetag>"%s"</d:getetag></d:prop><d:status>HTTP/1.1 200 OK</d:status></d:propstat></d:response>`, href, f.etags[href])
			}
			sb.WriteString(`</d:multistatus>`)
			w.Write([]byte(sb.String()))

		case r.Method == "REPORT":
			f.calls = append(f.calls, "multiget")
			var sb strings.Builder
			sb.WriteString(`<?xml version="1.0"?><d:multistatus xmlns:d="DAV:" xmlns:c="urn:ietf:params:xml:ns:caldav">`)
			for href, ics := range f.objects {
				if !strings.Contains(body.String(), href) {
					continue
				}
				fmt.Fprintf(&sb, `<d:response><d:href>%s</d:href><d:propstat><d:prop><d:getetag>"%s"</d:getetag><c:calendar-data>%s</c:calendar-data></d:prop><d:status>HTTP/1.1 200 OK</d:status></d:propstat></d:response>`, href, f.etags[href], ics)
			}
			sb.WriteString(`</d:multistatus>`)
			w.Write([]byte(sb.String()))
		}
	})
}

func ics(uid, summary string) string {
	return "BEGIN:VCALENDAR\nVERSION:2.0\nBEGIN:VEVENT\nUID:" + uid +
		"\nDTSTAMP:20260101T000000Z\nDTSTART:20260610T100000Z\nDTEND:20260610T103000Z\nSUMMARY:" +
		summary + "\nEND:VEVENT\nEND:VCALENDAR\n"
}

func setup(t *testing.T) (*fakeServer, *caldav.Client, *store.Store) {
	t.Helper()
	f := &fakeServer{
		ctag:    "ctag-1",
		objects: map[string]string{"/dav/work/a.ics": ics("a", "Alpha")},
		etags:   map[string]string{"/dav/work/a.ics": "etag-a1"},
	}
	srv := httptest.NewServer(f.handler(t))
	t.Cleanup(srv.Close)
	c, err := caldav.NewClient(srv.URL, "u", "p", false)
	if err != nil {
		t.Fatal(err)
	}
	return f, c, store.New(t.TempDir())
}

func TestFirstSyncFetchesEverything(t *testing.T) {
	f, c, s := setup(t)
	res, err := Calendar(context.Background(), c, s, "personal", "work", "/dav/work/")
	if err != nil {
		t.Fatalf("Calendar: %v", err)
	}
	if res.Unchanged {
		t.Error("first sync reported Unchanged")
	}
	if res.Fetched != 1 {
		t.Errorf("Fetched = %d, want 1", res.Fetched)
	}
	body, err := s.ReadObject("personal", "work", "a")
	if err != nil {
		t.Fatalf("object was not stored: %v", err)
	}
	if !strings.Contains(string(body), "SUMMARY:Alpha") {
		t.Errorf("stored body = %q", body)
	}
	idx, _ := s.LoadIndex("personal", "work")
	if idx.CTag != "ctag-1" {
		t.Errorf("stored CTag = %q, want ctag-1", idx.CTag)
	}
	_ = f
}

func TestUnchangedCTagSkipsAllFetches(t *testing.T) {
	f, c, s := setup(t)
	if _, err := Calendar(context.Background(), c, s, "personal", "work", "/dav/work/"); err != nil {
		t.Fatal(err)
	}
	f.calls = nil

	res, err := Calendar(context.Background(), c, s, "personal", "work", "/dav/work/")
	if err != nil {
		t.Fatalf("second sync: %v", err)
	}
	if !res.Unchanged {
		t.Error("Unchanged = false, want true for an identical ctag")
	}
	if res.Fetched != 0 {
		t.Errorf("Fetched = %d, want 0", res.Fetched)
	}
	for _, call := range f.calls {
		if call != "ctag" {
			t.Errorf("unchanged calendar made a %q call; only the ctag check is allowed (calls: %v)", call, f.calls)
		}
	}
}

func TestChangedObjectIsRefetched(t *testing.T) {
	f, c, s := setup(t)
	if _, err := Calendar(context.Background(), c, s, "personal", "work", "/dav/work/"); err != nil {
		t.Fatal(err)
	}
	f.ctag = "ctag-2"
	f.objects["/dav/work/a.ics"] = ics("a", "Alpha revised")
	f.etags["/dav/work/a.ics"] = "etag-a2"

	res, err := Calendar(context.Background(), c, s, "personal", "work", "/dav/work/")
	if err != nil {
		t.Fatalf("Calendar: %v", err)
	}
	if res.Fetched != 1 {
		t.Errorf("Fetched = %d, want 1", res.Fetched)
	}
	body, _ := s.ReadObject("personal", "work", "a")
	if !strings.Contains(string(body), "Alpha revised") {
		t.Errorf("object was not updated: %q", body)
	}
}

func TestUnchangedObjectIsNotRefetchedWhenCTagMoves(t *testing.T) {
	f, c, s := setup(t)
	if _, err := Calendar(context.Background(), c, s, "personal", "work", "/dav/work/"); err != nil {
		t.Fatal(err)
	}
	// A new object appears; the existing one is untouched.
	f.ctag = "ctag-2"
	f.objects["/dav/work/b.ics"] = ics("b", "Beta")
	f.etags["/dav/work/b.ics"] = "etag-b1"

	res, err := Calendar(context.Background(), c, s, "personal", "work", "/dav/work/")
	if err != nil {
		t.Fatalf("Calendar: %v", err)
	}
	if res.Fetched != 1 {
		t.Errorf("Fetched = %d, want 1 (only the new object)", res.Fetched)
	}
}

func TestDeletedObjectIsRemoved(t *testing.T) {
	f, c, s := setup(t)
	if _, err := Calendar(context.Background(), c, s, "personal", "work", "/dav/work/"); err != nil {
		t.Fatal(err)
	}
	f.ctag = "ctag-2"
	delete(f.objects, "/dav/work/a.ics")
	delete(f.etags, "/dav/work/a.ics")

	res, err := Calendar(context.Background(), c, s, "personal", "work", "/dav/work/")
	if err != nil {
		t.Fatalf("Calendar: %v", err)
	}
	if res.Deleted != 1 {
		t.Errorf("Deleted = %d, want 1", res.Deleted)
	}
	if _, err := s.ReadObject("personal", "work", "a"); err == nil {
		t.Error("deleted object is still in the store")
	}
	idx, _ := s.LoadIndex("personal", "work")
	if len(idx.Objects) != 0 {
		t.Errorf("index still holds %d objects, want 0", len(idx.Objects))
	}
}

// A server that does not implement getctag must still sync correctly.
func TestSyncWithoutCTagSupport(t *testing.T) {
	f, c, s := setup(t)
	f.ctag = ""
	res, err := Calendar(context.Background(), c, s, "personal", "work", "/dav/work/")
	if err != nil {
		t.Fatalf("Calendar: %v", err)
	}
	if res.Fetched != 1 {
		t.Errorf("Fetched = %d, want 1", res.Fetched)
	}
	// A second sync must also work, and must not claim Unchanged from an
	// empty-vs-empty ctag comparison.
	res, err = Calendar(context.Background(), c, s, "personal", "work", "/dav/work/")
	if err != nil {
		t.Fatalf("second sync: %v", err)
	}
	if res.Unchanged {
		t.Error("Unchanged = true, but an empty ctag proves nothing about staleness")
	}
	if res.Fetched != 0 {
		t.Errorf("Fetched = %d, want 0 (etags were unchanged)", res.Fetched)
	}
}

// A failure partway through must leave the previous cache readable.
func TestFailedSyncLeavesCacheIntact(t *testing.T) {
	f, c, s := setup(t)
	if _, err := Calendar(context.Background(), c, s, "personal", "work", "/dav/work/"); err != nil {
		t.Fatal(err)
	}
	f.ctag = "ctag-2"
	f.objects["/dav/work/b.ics"] = ics("b", "Beta")
	f.etags["/dav/work/b.ics"] = "etag-b1"

	// Point the client at a dead server to force a mid-sync failure.
	dead, err := caldav.NewClient("http://127.0.0.1:1", "u", "p", false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Calendar(context.Background(), dead, s, "personal", "work", "/dav/work/"); err == nil {
		t.Fatal("expected an error from an unreachable server")
	}
	body, err := s.ReadObject("personal", "work", "a")
	if err != nil {
		t.Fatalf("previously cached object was lost: %v", err)
	}
	if !strings.Contains(string(body), "Alpha") {
		t.Errorf("cached body = %q", body)
	}
	idx, _ := s.LoadIndex("personal", "work")
	if idx.CTag != "ctag-1" {
		t.Errorf("CTag = %q, want the previous ctag-1 to be preserved", idx.CTag)
	}
}

// A multiget response that silently drops one requested href (per
// caldav.MultiGet's documented behaviour) must not let the CTag advance,
// or that object's staleness becomes permanent and undetectable.
func TestPartialMultiGetWithholdsCTag(t *testing.T) {
	f, c, s := setup(t)
	if _, err := Calendar(context.Background(), c, s, "personal", "work", "/dav/work/"); err != nil {
		t.Fatal(err)
	}

	f.ctag = "ctag-2"
	f.objects["/dav/work/b.ics"] = ics("b", "Beta")
	f.etags["/dav/work/b.ics"] = "etag-b1"
	f.objects["/dav/work/a.ics"] = ics("a", "Alpha revised")
	f.etags["/dav/work/a.ics"] = "etag-a2"

	// Simulate the server silently omitting calendar-data for one href
	// (e.g. a per-object hiccup, or a delete racing the multiget) inside an
	// otherwise-200 multistatus, by wrapping the REPORT handler.
	drop := "/dav/work/a.ics"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "REPORT" {
			f.handler(t).ServeHTTP(w, r)
			return
		}
		body := new(strings.Builder)
		buf := make([]byte, 1<<16)
		n, _ := r.Body.Read(buf)
		body.Write(buf[:n])

		w.Header().Set("Content-Type", "application/xml")
		w.WriteHeader(http.StatusMultiStatus)
		var sb strings.Builder
		sb.WriteString(`<?xml version="1.0"?><d:multistatus xmlns:d="DAV:" xmlns:c="urn:ietf:params:xml:ns:caldav">`)
		for href, ics := range f.objects {
			if !strings.Contains(body.String(), href) {
				continue
			}
			if href == drop {
				// Present in the multistatus, but with no calendar-data --
				// exactly what caldav.MultiGet silently discards.
				fmt.Fprintf(&sb, `<d:response><d:href>%s</d:href><d:propstat><d:prop><d:getetag>"%s"</d:getetag></d:prop><d:status>HTTP/1.1 200 OK</d:status></d:propstat></d:response>`, href, f.etags[href])
				continue
			}
			fmt.Fprintf(&sb, `<d:response><d:href>%s</d:href><d:propstat><d:prop><d:getetag>"%s"</d:getetag><c:calendar-data>%s</c:calendar-data></d:prop><d:status>HTTP/1.1 200 OK</d:status></d:propstat></d:response>`, href, f.etags[href], ics)
		}
		sb.WriteString(`</d:multistatus>`)
		w.Write([]byte(sb.String()))
	}))
	defer srv.Close()
	c2, err := caldav.NewClient(srv.URL, "u", "p", false)
	if err != nil {
		t.Fatal(err)
	}

	res, err := Calendar(context.Background(), c2, s, "personal", "work", "/dav/work/")
	if err != nil {
		t.Fatalf("Calendar: %v", err)
	}
	if res.Missing != 1 {
		t.Errorf("Missing = %d, want 1", res.Missing)
	}
	idx, _ := s.LoadIndex("personal", "work")
	if idx.CTag != "" {
		t.Errorf("CTag = %q, want empty so the next sync retries the missing object", idx.CTag)
	}

	// Self-healing: a subsequent sync against a normal server must fetch the
	// previously-missing object and let the CTag advance again.
	res, err = Calendar(context.Background(), c, s, "personal", "work", "/dav/work/")
	if err != nil {
		t.Fatalf("second Calendar: %v", err)
	}
	if res.Fetched != 1 {
		t.Errorf("Fetched = %d, want 1 (only the previously-missing object)", res.Fetched)
	}
	body, err := s.ReadObject("personal", "work", "a")
	if err != nil {
		t.Fatalf("object a was not stored: %v", err)
	}
	if !strings.Contains(string(body), "Alpha revised") {
		t.Errorf("stored body = %q, want Alpha revised", body)
	}
	idx, _ = s.LoadIndex("personal", "work")
	if idx.CTag != "ctag-2" {
		t.Errorf("CTag = %q, want ctag-2 after the successful retry", idx.CTag)
	}
}

// Objects are stored BY UID, deletions are driven by HREFS disappearing, and
// index.Build used to enumerate the objects DIRECTORY -- three authorities
// over one set of files. If a server keeps an href but the object's UID
// changes (a rewrite by another client, or a body that gains a UID where
// uidFromHref had been used), the index entry's UID was overwritten, the old
// .ics was never deleted, and Build kept expanding it: a ghost event that no
// amount of syncing could remove.
func TestUIDChangeAtTheSameHrefLeavesNoGhost(t *testing.T) {
	f, c, s := setup(t)
	if _, err := Calendar(context.Background(), c, s, "personal", "work", "/dav/work/"); err != nil {
		t.Fatalf("first sync: %v", err)
	}

	// The server rewrites the same href with a different UID.
	f.objects["/dav/work/a.ics"] = ics("b", "Beta")
	f.etags["/dav/work/a.ics"] = "etag-a2"
	f.ctag = "ctag-2"
	if _, err := Calendar(context.Background(), c, s, "personal", "work", "/dav/work/"); err != nil {
		t.Fatalf("second sync: %v", err)
	}

	if _, err := s.ReadObject("personal", "work", "a"); !os.IsNotExist(err) {
		t.Errorf("the old UID's file is still on disk (err = %v); the href now holds UID b", err)
	}

	now := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	meta := &store.Meta{Accounts: []store.AccountMeta{{
		Name: "personal", LastSync: now,
		Calendars: []store.CalendarMeta{{ID: "work", Path: "/dav/work/", Name: "Work"}},
	}}}
	idx, err := index.Build(s, meta, now, time.UTC)
	if err != nil {
		t.Fatalf("index.Build: %v", err)
	}
	var got []string
	for _, o := range idx.Occurrences {
		got = append(got, o.UID+"/"+o.Summary)
	}
	if len(idx.Occurrences) != 1 || idx.Occurrences[0].UID != "b" {
		t.Errorf("expanded cache = %v, want exactly one occurrence with UID b", got)
	}
}

// UIDs are a shared namespace across hrefs: two different hrefs can carry
// the same UID at different times. If href A's old UID is deleted eagerly
// while processing A -- before href B (which is about to start using that
// UID) has been processed -- then processing B can delete the very file A's
// processing just wrote a moment before B's own UID was written. This test
// swaps two hrefs' UIDs in one sync and asserts both objects survive it, and
// keep surviving further no-op syncs.
func TestUIDSwapBetweenHrefsKeepsBothObjects(t *testing.T) {
	f, c, s := setup(t)
	f.objects["/dav/work/b.ics"] = ics("y", "Beta")
	f.etags["/dav/work/b.ics"] = "etag-b1"
	f.objects["/dav/work/a.ics"] = ics("x", "Alpha")
	f.etags["/dav/work/a.ics"] = "etag-a1"

	if _, err := Calendar(context.Background(), c, s, "personal", "work", "/dav/work/"); err != nil {
		t.Fatalf("first sync: %v", err)
	}

	// The server swaps the UIDs served at each href.
	f.objects["/dav/work/a.ics"] = ics("y", "Beta")
	f.etags["/dav/work/a.ics"] = "etag-a2"
	f.objects["/dav/work/b.ics"] = ics("x", "Alpha")
	f.etags["/dav/work/b.ics"] = "etag-b2"
	f.ctag = "ctag-2"

	if _, err := Calendar(context.Background(), c, s, "personal", "work", "/dav/work/"); err != nil {
		t.Fatalf("second sync (swap): %v", err)
	}

	if _, err := s.ReadObject("personal", "work", "x"); err != nil {
		t.Errorf("UID x is gone after the swap: %v", err)
	}
	if _, err := s.ReadObject("personal", "work", "y"); err != nil {
		t.Errorf("UID y is gone after the swap: %v", err)
	}

	now := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	meta := &store.Meta{Accounts: []store.AccountMeta{{
		Name: "personal", LastSync: now,
		Calendars: []store.CalendarMeta{{ID: "work", Path: "/dav/work/", Name: "Work"}},
	}}}
	idx, err := index.Build(s, meta, now, time.UTC)
	if err != nil {
		t.Fatalf("index.Build: %v", err)
	}
	if len(idx.Occurrences) != 2 {
		t.Errorf("expanded cache has %d occurrences, want 2", len(idx.Occurrences))
	}

	// A third, no-op sync must not disturb anything: this proves the loss
	// (if any) is permanent, not merely a transient race.
	if _, err := Calendar(context.Background(), c, s, "personal", "work", "/dav/work/"); err != nil {
		t.Fatalf("third sync (no-op): %v", err)
	}
	if _, err := s.ReadObject("personal", "work", "x"); err != nil {
		t.Errorf("UID x is gone after the third sync: %v", err)
	}
	if _, err := s.ReadObject("personal", "work", "y"); err != nil {
		t.Errorf("UID y is gone after the third sync: %v", err)
	}
}

// A pre-existing sibling of the swap bug: an href rename with the UID
// unchanged. The new href is fetched and written before the old href's
// disappearance is processed; if the vanished-href deletion loop deletes by
// UID unconditionally, it deletes the file the rename just wrote.
func TestHrefRenameKeepsTheObject(t *testing.T) {
	f, c, s := setup(t)
	f.objects["/dav/work/a.ics"] = ics("x", "Alpha")
	f.etags["/dav/work/a.ics"] = "etag-a1"

	if _, err := Calendar(context.Background(), c, s, "personal", "work", "/dav/work/"); err != nil {
		t.Fatalf("first sync: %v", err)
	}

	// The event moves to a new href, keeping the same UID.
	delete(f.objects, "/dav/work/a.ics")
	delete(f.etags, "/dav/work/a.ics")
	f.objects["/dav/work/c.ics"] = ics("x", "Alpha")
	f.etags["/dav/work/c.ics"] = "etag-c1"
	f.ctag = "ctag-2"

	if _, err := Calendar(context.Background(), c, s, "personal", "work", "/dav/work/"); err != nil {
		t.Fatalf("second sync (rename): %v", err)
	}

	if _, err := s.ReadObject("personal", "work", "x"); err != nil {
		t.Errorf("UID x is gone after the href rename: %v", err)
	}
	idx, _ := s.LoadIndex("personal", "work")
	if len(idx.Objects) != 1 {
		t.Errorf("index has %d objects, want 1", len(idx.Objects))
	}
	if ref, ok := idx.Objects["/dav/work/c.ics"]; !ok || ref.UID != "x" {
		t.Errorf("index entry for the new href = %+v, ok=%v, want UID x", ref, ok)
	}
}
