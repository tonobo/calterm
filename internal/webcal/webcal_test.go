package webcal

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/tonobo/calterm/internal/config"
	"github.com/tonobo/calterm/internal/store"
)

const validFeed = `BEGIN:VCALENDAR
VERSION:2.0
X-WR-CALNAME:Waste collection
BEGIN:VEVENT
UID:waste-1
DTSTART;VALUE=DATE:20260924
DTEND;VALUE=DATE:20260925
SUMMARY:Recycling
END:VEVENT
END:VCALENDAR
`

func TestNormalizeURL(t *testing.T) {
	got, err := NormalizeURL("webcal://calendar.example/waste.ics?place=1")
	if err != nil {
		t.Fatalf("NormalizeURL: %v", err)
	}
	if got != "https://calendar.example/waste.ics?place=1" {
		t.Errorf("NormalizeURL = %q", got)
	}
	for _, raw := range []string{"file:///tmp/event.ics", "javascript:alert(1)", "not a url"} {
		if _, err := NormalizeURL(raw); err == nil {
			t.Errorf("NormalizeURL(%q) should fail", raw)
		}
	}
}

func TestSyncCachesFeedAndUsesConditionalRequest(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if requests == 2 {
			if got := r.Header.Get("If-None-Match"); got != `"feed-v1"` {
				t.Errorf("If-None-Match = %q", got)
			}
			if got := r.Header.Get("If-Modified-Since"); got == "" {
				t.Error("second request omitted If-Modified-Since")
			}
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("Content-Type", "text/calendar")
		w.Header().Set("ETag", `"feed-v1"`)
		w.Header().Set("Last-Modified", "Fri, 11 Sep 2026 09:00:00 GMT")
		_, _ = w.Write([]byte(validFeed))
	}))
	defer server.Close()

	s := store.New(t.TempDir())
	feed := config.WebCal{Name: "waste", URL: server.URL}
	first, err := Sync(context.Background(), server.Client(), s, feed)
	if err != nil {
		t.Fatalf("first Sync: %v", err)
	}
	if first.Name != "Waste collection" || first.Events != 1 || first.Unchanged {
		t.Errorf("first result = %+v", first)
	}
	body, err := s.ReadObject(AccountName, "waste", feedUID)
	if err != nil || string(body) != validFeed {
		t.Fatalf("cached body mismatch: err=%v body=%q", err, body)
	}

	second, err := Sync(context.Background(), server.Client(), s, feed)
	if err != nil {
		t.Fatalf("second Sync: %v", err)
	}
	if !second.Unchanged {
		t.Errorf("second result = %+v, want unchanged", second)
	}
}

func TestSyncUsesOptionalBasicAuthentication(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		username, password, ok := r.BasicAuth()
		if !ok || username != "feed-user" || password != "feed-password" {
			t.Errorf("BasicAuth = %q, %q, %v", username, password, ok)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(validFeed))
	}))
	defer server.Close()

	feed := config.WebCal{
		Name: "authenticated", URL: server.URL,
		Username: "feed-user", PasswordCmd: "printf feed-password",
	}
	if _, err := Sync(context.Background(), server.Client(), store.New(t.TempDir()), feed); err != nil {
		t.Fatalf("Sync: %v", err)
	}
}

func TestMalformedRefreshKeepsPreviousFeed(t *testing.T) {
	body := validFeed
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	defer server.Close()

	s := store.New(t.TempDir())
	feed := config.WebCal{Name: "waste", URL: server.URL}
	if _, err := Sync(context.Background(), server.Client(), s, feed); err != nil {
		t.Fatalf("seeding cache: %v", err)
	}
	body = "this is not an iCalendar document"
	if _, err := Sync(context.Background(), server.Client(), s, feed); err == nil {
		t.Fatal("malformed refresh should fail")
	}
	cached, err := s.ReadObject(AccountName, "waste", feedUID)
	if err != nil {
		t.Fatalf("reading preserved cache: %v", err)
	}
	if !strings.Contains(string(cached), "SUMMARY:Recycling") {
		t.Errorf("malformed response replaced the good cache: %q", cached)
	}
}

func TestHTTPFailureDoesNotCreateCache(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "no", http.StatusServiceUnavailable)
	}))
	defer server.Close()

	s := store.New(t.TempDir())
	_, err := Sync(context.Background(), server.Client(), s, config.WebCal{Name: "waste", URL: server.URL})
	if err == nil || !strings.Contains(err.Error(), "503") {
		t.Fatalf("Sync error = %v, want HTTP 503", err)
	}
	if objects, listErr := s.ListObjects(AccountName, "waste"); listErr != nil || len(objects) != 0 {
		t.Fatalf("objects after failed first sync = %v, err=%v", objects, listErr)
	}
}
