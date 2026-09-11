package caldav

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const ctagResponse = `<?xml version="1.0" encoding="utf-8"?>
<d:multistatus xmlns:d="DAV:" xmlns:cs="http://calendarserver.org/ns/" xmlns:ic="http://apple.com/ns/ical/">
  <d:response>
    <d:href>/dav/work/</d:href>
    <d:propstat>
      <d:prop>
        <d:displayname>Work</d:displayname>
        <cs:getctag>ctag-42</cs:getctag>
        <ic:calendar-color>#3366ffff</ic:calendar-color>
        <d:resourcetype><d:collection/><c:calendar xmlns:c="urn:ietf:params:xml:ns:caldav"/></d:resourcetype>
      </d:prop>
      <d:status>HTTP/1.1 200 OK</d:status>
    </d:propstat>
  </d:response>
</d:multistatus>`

const etagResponse = `<?xml version="1.0" encoding="utf-8"?>
<d:multistatus xmlns:d="DAV:">
  <d:response>
    <d:href>/dav/work/</d:href>
    <d:propstat>
      <d:prop><d:resourcetype><d:collection/></d:resourcetype></d:prop>
      <d:status>HTTP/1.1 200 OK</d:status>
    </d:propstat>
  </d:response>
  <d:response>
    <d:href>/dav/work/a.ics</d:href>
    <d:propstat>
      <d:prop><d:getetag>"etag-a"</d:getetag></d:prop>
      <d:status>HTTP/1.1 200 OK</d:status>
    </d:propstat>
  </d:response>
  <d:response>
    <d:href>/dav/work/b.ics</d:href>
    <d:propstat>
      <d:prop><d:getetag>"etag-b"</d:getetag></d:prop>
      <d:status>HTTP/1.1 200 OK</d:status>
    </d:propstat>
    <d:propstat>
      <d:prop><d:calendar-color/></d:prop>
      <d:status>HTTP/1.1 404 Not Found</d:status>
    </d:propstat>
  </d:response>
</d:multistatus>`

const multigetResponse = `<?xml version="1.0" encoding="utf-8"?>
<d:multistatus xmlns:d="DAV:" xmlns:c="urn:ietf:params:xml:ns:caldav">
  <d:response>
    <d:href>/dav/work/a.ics</d:href>
    <d:propstat>
      <d:prop>
        <d:getetag>"etag-a"</d:getetag>
        <c:calendar-data>BEGIN:VCALENDAR&#13;
VERSION:2.0&#13;
BEGIN:VEVENT&#13;
UID:a&#13;
SUMMARY:Tom &amp;amp; Jerry&#13;
END:VEVENT&#13;
END:VCALENDAR&#13;
</c:calendar-data>
      </d:prop>
      <d:status>HTTP/1.1 200 OK</d:status>
    </d:propstat>
  </d:response>
</d:multistatus>`

// wantMultiGetCalendarData is the exact iCalendar text encoding/xml must
// produce after a single decode pass over multigetResponse above. The
// "&amp;amp;" on the wire is a legitimately double-escaped ampersand: it
// must decode to the literal text "&amp;" (one XML-entity resolution),
// not to a bare "&" (which is what an added html.UnescapeString pass over
// the already-decoded text would wrongly produce -- that corruption is
// exactly what the raw-bytes guarantee forbids). See client.go's flatten
// for the comment explaining why no second unescaping pass is applied.
const wantMultiGetCalendarData = "BEGIN:VCALENDAR\r\n" +
	"VERSION:2.0\r\n" +
	"BEGIN:VEVENT\r\n" +
	"UID:a\r\n" +
	"SUMMARY:Tom &amp; Jerry\r\n" +
	"END:VEVENT\r\n" +
	"END:VCALENDAR\r\n"

type recorded struct {
	method string
	path   string
	depth  string
	body   string
	auth   string
}

func newTestServer(t *testing.T, handler func(w http.ResponseWriter, r *http.Request, rec *recorded)) (*httptest.Server, *[]recorded) {
	t.Helper()
	var log []recorded
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf := new(strings.Builder)
		if r.Body != nil {
			b := make([]byte, 1<<16)
			n, _ := r.Body.Read(b)
			buf.Write(b[:n])
		}
		user, pass, _ := r.BasicAuth()
		rec := recorded{method: r.Method, path: r.URL.Path, depth: r.Header.Get("Depth"), body: buf.String(), auth: user + ":" + pass}
		log = append(log, rec)
		handler(w, r, &rec)
	}))
	t.Cleanup(srv.Close)
	return srv, &log
}

func TestCalendarPropsParsesCTagColorAndName(t *testing.T) {
	srv, log := newTestServer(t, func(w http.ResponseWriter, r *http.Request, rec *recorded) {
		w.Header().Set("Content-Type", "application/xml")
		w.WriteHeader(http.StatusMultiStatus)
		w.Write([]byte(ctagResponse))
	})
	c, err := NewClient(srv.URL, "calendar-user", "hunter2", false)
	if err != nil {
		t.Fatal(err)
	}
	got, err := c.CalendarProps(context.Background(), "/dav/work/")
	if err != nil {
		t.Fatalf("CalendarProps: %v", err)
	}
	if got.CTag != "ctag-42" {
		t.Errorf("CTag = %q, want ctag-42", got.CTag)
	}
	if got.DisplayName != "Work" {
		t.Errorf("DisplayName = %q, want Work", got.DisplayName)
	}
	if got.Color != "#3366ffff" {
		t.Errorf("Color = %q, want #3366ffff", got.Color)
	}
	if !got.IsCalendar {
		t.Error("IsCalendar = false, want true")
	}
	if len(*log) != 1 {
		t.Fatalf("made %d requests, want 1", len(*log))
	}
	req := (*log)[0]
	if req.method != "PROPFIND" {
		t.Errorf("method = %s, want PROPFIND", req.method)
	}
	if req.depth != "0" {
		t.Errorf("Depth = %q, want 0", req.depth)
	}
	if req.auth != "calendar-user:hunter2" {
		t.Errorf("basic auth = %q, want calendar-user:hunter2", req.auth)
	}
}

func TestListObjectETagsSkipsCollectionAndNon200Props(t *testing.T) {
	srv, log := newTestServer(t, func(w http.ResponseWriter, r *http.Request, rec *recorded) {
		w.WriteHeader(http.StatusMultiStatus)
		w.Write([]byte(etagResponse))
	})
	c, _ := NewClient(srv.URL, "u", "p", false)
	got, err := c.ListObjectETags(context.Background(), "/dav/work/")
	if err != nil {
		t.Fatalf("ListObjectETags: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d resources, want 2 (the collection itself must be skipped): %+v", len(got), got)
	}
	if got[0].Href != "/dav/work/a.ics" || got[0].ETag != "etag-a" {
		t.Errorf("first = %+v, want a.ics / etag-a (quotes stripped)", got[0])
	}
	if got[1].ETag != "etag-b" {
		t.Errorf("second ETag = %q, want etag-b", got[1].ETag)
	}
	if (*log)[0].depth != "1" {
		t.Errorf("Depth = %q, want 1", (*log)[0].depth)
	}
}

func TestMultiGetReturnsRawCalendarData(t *testing.T) {
	srv, log := newTestServer(t, func(w http.ResponseWriter, r *http.Request, rec *recorded) {
		w.WriteHeader(http.StatusMultiStatus)
		w.Write([]byte(multigetResponse))
	})
	c, _ := NewClient(srv.URL, "u", "p", false)
	got, err := c.MultiGet(context.Background(), "/dav/work/", []string{"/dav/work/a.ics"})
	if err != nil {
		t.Fatalf("MultiGet: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d resources, want 1", len(got))
	}
	if got[0].CalendarData != wantMultiGetCalendarData {
		t.Errorf("CalendarData =\n%q\nwant exactly\n%q\n(if this now fails because a second unescaping pass was added, that pass corrupts a legitimately-escaped ampersand -- see the fixture's comment)", got[0].CalendarData, wantMultiGetCalendarData)
	}
	if got[0].ETag != "etag-a" {
		t.Errorf("ETag = %q, want etag-a", got[0].ETag)
	}
	req := (*log)[0]
	if req.method != "REPORT" {
		t.Errorf("method = %s, want REPORT", req.method)
	}
	if !strings.Contains(req.body, "calendar-multiget") {
		t.Errorf("request body should be a calendar-multiget, got %q", req.body)
	}
	if !strings.Contains(req.body, "/dav/work/a.ics") {
		t.Errorf("request body should name the requested href, got %q", req.body)
	}
}

func TestMultiGetWithNoHrefsMakesNoRequest(t *testing.T) {
	srv, log := newTestServer(t, func(w http.ResponseWriter, r *http.Request, rec *recorded) {
		t.Error("server must not be contacted for an empty multiget")
	})
	c, _ := NewClient(srv.URL, "u", "p", false)
	got, err := c.MultiGet(context.Background(), "/dav/work/", nil)
	if err != nil {
		t.Fatalf("MultiGet: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got %d resources, want 0", len(got))
	}
	if len(*log) != 0 {
		t.Errorf("made %d requests, want 0", len(*log))
	}
}

func TestPutCalendarObjectUsesConditionalAuthenticatedPUT(t *testing.T) {
	var contentType, ifMatch string
	srv, log := newTestServer(t, func(w http.ResponseWriter, r *http.Request, rec *recorded) {
		contentType = r.Header.Get("Content-Type")
		ifMatch = r.Header.Get("If-Match")
		w.WriteHeader(http.StatusNoContent)
	})
	c, _ := NewClient(srv.URL+"/dav/", "calendar-user", "hunter2", false)
	body := []byte("BEGIN:VCALENDAR\r\nEND:VCALENDAR\r\n")
	if err := c.PutCalendarObject(context.Background(), "/dav/work/invite.ics", "etag-42", body); err != nil {
		t.Fatal(err)
	}
	if len(*log) != 1 {
		t.Fatalf("made %d requests, want 1", len(*log))
	}
	req := (*log)[0]
	if req.method != http.MethodPut || req.path != "/dav/work/invite.ics" || req.body != string(body) {
		t.Errorf("request = %+v", req)
	}
	if req.auth != "calendar-user:hunter2" || contentType != "text/calendar; charset=utf-8" || ifMatch != `"etag-42"` {
		t.Errorf("auth=%q content-type=%q if-match=%q", req.auth, contentType, ifMatch)
	}
}

func TestPutCalendarObjectReportsConflict(t *testing.T) {
	srv, _ := newTestServer(t, func(w http.ResponseWriter, r *http.Request, rec *recorded) {
		w.WriteHeader(http.StatusPreconditionFailed)
		w.Write([]byte("changed by organizer"))
	})
	c, _ := NewClient(srv.URL, "u", "p", false)
	err := c.PutCalendarObject(context.Background(), "/dav/work/invite.ics", "old", []byte("calendar"))
	if err == nil || !strings.Contains(err.Error(), "412") || !strings.Contains(err.Error(), "changed by organizer") {
		t.Fatalf("error = %v, want useful conflict response", err)
	}
}

func TestHTTPErrorsAreReported(t *testing.T) {
	srv, _ := newTestServer(t, func(w http.ResponseWriter, r *http.Request, rec *recorded) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte("nope"))
	})
	c, _ := NewClient(srv.URL, "u", "p", false)
	_, err := c.CalendarProps(context.Background(), "/dav/work/")
	if err == nil {
		t.Fatal("expected an error for a 401 response")
	}
	if !strings.Contains(err.Error(), "401") {
		t.Errorf("error %q should mention the status code", err)
	}
}

func TestMissingCTagIsNotAnError(t *testing.T) {
	const noCTag = `<?xml version="1.0" encoding="utf-8"?>
<d:multistatus xmlns:d="DAV:">
  <d:response>
    <d:href>/dav/work/</d:href>
    <d:propstat>
      <d:prop><d:displayname>Work</d:displayname></d:prop>
      <d:status>HTTP/1.1 200 OK</d:status>
    </d:propstat>
  </d:response>
</d:multistatus>`
	srv, _ := newTestServer(t, func(w http.ResponseWriter, r *http.Request, rec *recorded) {
		w.WriteHeader(http.StatusMultiStatus)
		w.Write([]byte(noCTag))
	})
	c, _ := NewClient(srv.URL, "u", "p", false)
	got, err := c.CalendarProps(context.Background(), "/dav/work/")
	if err != nil {
		t.Fatalf("a server without getctag must still work, got %v", err)
	}
	if got.CTag != "" {
		t.Errorf("CTag = %q, want empty", got.CTag)
	}
	if got.DisplayName != "Work" {
		t.Errorf("DisplayName = %q, want Work", got.DisplayName)
	}
}

// Server hrefs are percent-encoded by definition, and Discover feeds them
// straight back into resolve. Assigning the href to url.URL.Path made
// url.String() escape it a SECOND time, so /calendars/user%40example.com/
// went out as /calendars/user%2540example.com/ -- a 404 on the very first
// PROPFIND for any server with an @, a space, or non-ASCII in its paths
// (SOGo, Zimbra, email-style principals, a calendar named "Café"). calterm
// simply never worked against those servers.
func TestResolvePreservesPercentEncoding(t *testing.T) {
	tests := []struct {
		name string
		base string
		href string
		want string
	}{
		{"encoded @ in an email-style principal", "/dav/", "/calendars/user%40example.com/personal/", "/calendars/user%40example.com/personal/"},
		{"encoded space in a display name", "/dav/", "/dav/My%20Calendar/", "/dav/My%20Calendar/"},
		{"encoded non-ASCII", "/dav/", "/dav/Caf%C3%A9/", "/dav/Caf%C3%A9/"},
		{"a relative href resolves against the base, not the root", "/dav/user/", "personal/", "/dav/user/personal/"},
		{"a root-relative href replaces the base path", "/dav/user/", "/other/", "/other/"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// EscapedPath, not Path: Path is already decoded and would
				// hide the double-encoding entirely.
				got = r.URL.EscapedPath()
				w.Header().Set("Content-Type", "application/xml")
				w.WriteHeader(http.StatusMultiStatus)
				w.Write([]byte(ctagResponse))
			}))
			defer srv.Close()

			c, err := NewClient(srv.URL+tt.base, "u", "p", false)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := c.CalendarProps(context.Background(), tt.href); err != nil {
				t.Fatalf("CalendarProps: %v", err)
			}
			if got != tt.want {
				t.Errorf("server received path %q, want %q", got, tt.want)
			}
		})
	}
}

// Discovery feeds the current-user-principal and calendar-home-set hrefs the
// server hands back straight into resolve, so a percent-encoded principal --
// the norm on any server using email addresses as principals -- must survive
// the round trip.
func TestDiscoverFollowsEncodedHrefs(t *testing.T) {
	var listed string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := new(strings.Builder)
		io.Copy(body, r.Body)

		w.Header().Set("Content-Type", "application/xml")
		w.WriteHeader(http.StatusMultiStatus)
		switch {
		case strings.Contains(body.String(), "current-user-principal"):
			w.Write([]byte(`<?xml version="1.0"?>
<d:multistatus xmlns:d="DAV:"><d:response><d:href>/dav/</d:href><d:propstat>
  <d:prop><d:current-user-principal><d:href>/principals/user%40example.com/</d:href></d:current-user-principal></d:prop>
  <d:status>HTTP/1.1 200 OK</d:status>
</d:propstat></d:response></d:multistatus>`))
		case strings.Contains(body.String(), "calendar-home-set"):
			w.Write([]byte(`<?xml version="1.0"?>
<d:multistatus xmlns:d="DAV:" xmlns:c="urn:ietf:params:xml:ns:caldav"><d:response><d:href>/principals/user%40example.com/</d:href><d:propstat>
  <d:prop><c:calendar-home-set><d:href>/calendars/user%40example.com/My%20Calendars/</d:href></c:calendar-home-set></d:prop>
  <d:status>HTTP/1.1 200 OK</d:status>
</d:propstat></d:response></d:multistatus>`))
		default:
			listed = r.URL.EscapedPath()
			w.Write([]byte(ctagResponse))
		}
	}))
	defer srv.Close()

	c, err := NewClient(srv.URL+"/dav/", "u", "p", false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Discover(context.Background()); err != nil {
		t.Fatalf("Discover: %v", err)
	}
	const want = "/calendars/user%40example.com/My%20Calendars/"
	if listed != want {
		t.Errorf("calendar listing went to %q, want %q", listed, want)
	}
}

// The base URL's own path is where discovery starts, and it must go out
// encoded too: a calendar home containing a literal percent sign is legal and
// its %25 must not be decoded on the wire.
func TestDiscoverKeepsTheBasePathEncoded(t *testing.T) {
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.EscapedPath())
		w.Header().Set("Content-Type", "application/xml")
		w.WriteHeader(http.StatusMultiStatus)
		if r.Header.Get("Depth") == "0" {
			w.Write([]byte(`<?xml version="1.0"?><d:multistatus xmlns:d="DAV:"></d:multistatus>`))
			return
		}
		w.Write([]byte(ctagResponse))
	}))
	defer srv.Close()

	c, err := NewClient(srv.URL+"/dav/100%25%20mine/", "u", "p", false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Discover(context.Background()); err != nil {
		t.Fatalf("Discover: %v", err)
	}
	const want = "/dav/100%25%20mine/"
	for _, p := range paths {
		if p != want {
			t.Errorf("server received path %q, want %q", p, want)
		}
	}
}

// The critical companion to resolve's fix: only the request URL is resolved.
// The <d:href> elements in the multiget body -- and, upstream, the sync index
// keys -- must carry the server's verbatim href string, byte for byte, or the
// server cannot match the request to its own resources.
func TestMultiGetSendsVerbatimHrefs(t *testing.T) {
	var body string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b := new(strings.Builder)
		io.Copy(b, r.Body)
		body = b.String()
		w.Header().Set("Content-Type", "application/xml")
		w.WriteHeader(http.StatusMultiStatus)
		w.Write([]byte(multigetResponse))
	}))
	defer srv.Close()

	c, err := NewClient(srv.URL+"/dav/", "u", "p", false)
	if err != nil {
		t.Fatal(err)
	}
	const href = "/dav/user%40example.com/My%20Calendar/a.ics"
	if _, err := c.MultiGet(context.Background(), "/dav/work/", []string{href}); err != nil {
		t.Fatalf("MultiGet: %v", err)
	}
	if !strings.Contains(body, "<d:href>"+href+"</d:href>") {
		t.Errorf("request body does not carry the verbatim href %q:\n%s", href, body)
	}
}

// A propstat status line's reason phrase is optional: "HTTP/1.1 200" is
// legal. flatten used to test strings.Contains(status, " 200 "), which needs
// a trailing space and so silently dropped EVERY property of such a response.
// In ListObjectETags that yields an empty ETag, the entry is skipped, and
// sync.Calendar then concludes every cached object was deleted server-side
// and removes it -- a whole calendar wiped by a missing reason phrase.
func TestFlattenAcceptsAStatusWithNoReasonPhrase(t *testing.T) {
	tests := []struct {
		name   string
		status string
		want   bool
	}{
		{"canonical", "HTTP/1.1 200 OK", true},
		{"no reason phrase", "HTTP/1.1 200", true},
		{"trailing whitespace only", "HTTP/1.1 200 ", true},
		{"HTTP/2 style", "HTTP/2 200", true},
		{"not found", "HTTP/1.1 404 Not Found", false},
		{"forbidden, no reason phrase", "HTTP/1.1 403", false},
		{"unparseable", "gibberish", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ms := &multistatus{Responses: []davResponse{{
				Href:      "/dav/work/a.ics",
				Propstats: []propstat{{Status: tt.status, Prop: davProp{ETag: `"etag-a"`}}},
			}}}
			got := flatten(ms)
			if len(got) != 1 {
				t.Fatalf("flatten returned %d resources, want 1", len(got))
			}
			if (got[0].ETag != "") != tt.want {
				t.Errorf("status %q gave ETag %q, want present=%v", tt.status, got[0].ETag, tt.want)
			}
		})
	}
}
