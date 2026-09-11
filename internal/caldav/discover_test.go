package caldav

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// A three-step discovery: current-user-principal, calendar-home-set, then the
// calendars beneath the home set.
func TestDiscoverWalksPrincipalChain(t *testing.T) {
	var depths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := new(strings.Builder)
		buf := make([]byte, 1<<16)
		n, _ := r.Body.Read(buf)
		body.Write(buf[:n])
		depths = append(depths, r.URL.Path+" depth="+r.Header.Get("Depth"))

		w.Header().Set("Content-Type", "application/xml")
		w.WriteHeader(http.StatusMultiStatus)

		switch {
		case strings.Contains(body.String(), "current-user-principal"):
			w.Write([]byte(`<?xml version="1.0"?>
<d:multistatus xmlns:d="DAV:">
  <d:response><d:href>/dav/</d:href><d:propstat>
    <d:prop><d:current-user-principal><d:href>/dav/principals/calendar-user/</d:href></d:current-user-principal></d:prop>
    <d:status>HTTP/1.1 200 OK</d:status>
  </d:propstat></d:response>
</d:multistatus>`))
		case strings.Contains(body.String(), "calendar-home-set"):
			w.Write([]byte(`<?xml version="1.0"?>
<d:multistatus xmlns:d="DAV:" xmlns:c="urn:ietf:params:xml:ns:caldav">
  <d:response><d:href>/dav/principals/calendar-user/</d:href><d:propstat>
    <d:prop><c:calendar-home-set><d:href>/dav/calendars/calendar-user/</d:href></c:calendar-home-set></d:prop>
    <d:status>HTTP/1.1 200 OK</d:status>
  </d:propstat></d:response>
</d:multistatus>`))
		default:
			w.Write([]byte(`<?xml version="1.0"?>
<d:multistatus xmlns:d="DAV:" xmlns:cs="http://calendarserver.org/ns/" xmlns:ic="http://apple.com/ns/ical/">
  <d:response><d:href>/dav/calendars/calendar-user/</d:href><d:propstat>
    <d:prop><d:resourcetype><d:collection/></d:resourcetype></d:prop>
    <d:status>HTTP/1.1 200 OK</d:status>
  </d:propstat></d:response>
  <d:response><d:href>/dav/calendars/calendar-user/work/</d:href><d:propstat>
    <d:prop>
      <d:displayname>Work</d:displayname>
      <cs:getctag>ctag-1</cs:getctag>
      <ic:calendar-color>#ff0000</ic:calendar-color>
      <d:resourcetype><d:collection/><c:calendar xmlns:c="urn:ietf:params:xml:ns:caldav"/></d:resourcetype>
    </d:prop>
    <d:status>HTTP/1.1 200 OK</d:status>
  </d:propstat></d:response>
  <d:response><d:href>/dav/calendars/calendar-user/notes/</d:href><d:propstat>
    <d:prop>
      <d:displayname>Notes</d:displayname>
      <d:resourcetype><d:collection/></d:resourcetype>
    </d:prop>
    <d:status>HTTP/1.1 200 OK</d:status>
  </d:propstat></d:response>
</d:multistatus>`))
		}
	}))
	defer srv.Close()

	c, err := NewClient(srv.URL+"/dav/", "u", "p", false)
	if err != nil {
		t.Fatal(err)
	}
	got, err := c.Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	// Only the calendar collection is returned; the plain collection is not.
	if len(got) != 1 {
		t.Fatalf("got %d calendars, want 1: %+v", len(got), got)
	}
	if got[0].DisplayName != "Work" {
		t.Errorf("DisplayName = %q, want Work", got[0].DisplayName)
	}
	if got[0].Href != "/dav/calendars/calendar-user/work/" {
		t.Errorf("Href = %q", got[0].Href)
	}
	if got[0].Color != "#ff0000" {
		t.Errorf("Color = %q, want #ff0000", got[0].Color)
	}
}
