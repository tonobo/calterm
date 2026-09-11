package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tonobo/calterm/internal/config"
	"github.com/tonobo/calterm/internal/model"
	"github.com/tonobo/calterm/internal/store"
	"github.com/tonobo/calterm/internal/tui"
)

func TestRunVersion(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := run([]string{"version"}, &out, &errOut); code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, errOut.String())
	}
	if !strings.Contains(out.String(), "calterm") {
		t.Errorf("output = %q, want it to mention calterm", out.String())
	}
}

func TestRunUnknownSubcommand(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := run([]string{"frobnicate"}, &out, &errOut); code == 0 {
		t.Fatal("exit code = 0, want non-zero for an unknown subcommand")
	}
	if !strings.Contains(errOut.String(), "frobnicate") {
		t.Errorf("stderr = %q, want it to name the unknown subcommand", errOut.String())
	}
}

func TestRunHelpListsSubcommands(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := run([]string{"help"}, &out, &errOut); code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	for _, want := range []string{"open", "sync", "waybar", "version"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("help output does not mention %q: %s", want, out.String())
		}
	}
}

func TestRunSyncWithMissingConfigFails(t *testing.T) {
	var out, errOut bytes.Buffer
	code := run([]string{"sync", "-config", "/nonexistent/calterm.toml"}, &out, &errOut)
	if code == 0 {
		t.Fatal("exit code = 0, want non-zero when the config is missing")
	}
	if errOut.Len() == 0 {
		t.Error("stderr is empty; a failure must explain itself")
	}
}

func writeOpenTestConfig(t *testing.T, cacheHome string) string {
	t.Helper()
	t.Setenv("XDG_CACHE_HOME", cacheHome)
	path := filepath.Join(t.TempDir(), "config.toml")
	body := `[[account]]
name = "work"
url = "http://127.0.0.1:1"
username = "tim"
password_cmd = "printf password"
email = "user@work.example"
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func writeOpenTestICS(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "event.ics")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func openTestICS(uid string) string {
	return "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//calterm//test//EN\r\n" +
		"BEGIN:VEVENT\r\nUID:" + uid + "\r\nDTSTART:20260915T110000Z\r\n" +
		"DTEND:20260915T114500Z\r\nSUMMARY:Planning session\r\n" +
		"ATTENDEE:mailto:user@work.example\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"
}

func TestOpenCommandShowsUncachedICSReadOnly(t *testing.T) {
	configPath := writeOpenTestConfig(t, t.TempDir())
	icsPath := writeOpenTestICS(t, openTestICS("not-cached"))

	oldRunner := runFocusedTUI
	defer func() { runFocusedTUI = oldRunner }()
	var got tui.FocusOptions
	runFocusedTUI = func(_ *config.Config, _ *store.Store, _ time.Time, _ string, opts tui.FocusOptions) error {
		got = opts
		return nil
	}

	var out, errOut bytes.Buffer
	if code := run([]string{"open", "-config", configPath, icsPath}, &out, &errOut); code != 0 {
		t.Fatalf("exit code = %d, stderr: %s", code, errOut.String())
	}
	if !got.ReadOnly || got.Occurrence.UID != "not-cached" || got.Occurrence.Summary != "Planning session" {
		t.Fatalf("focus options = %+v", got)
	}
	if got.Occurrence.AccountID != "" || got.Occurrence.CalendarID != "" {
		t.Errorf("uncached occurrence acquired ownership: %+v", got.Occurrence)
	}
	if !strings.Contains(got.Notice, "not present in synced calendar") {
		t.Errorf("notice = %q", got.Notice)
	}
}

func TestOpenCommandUsesCachedOccurrence(t *testing.T) {
	cacheHome := t.TempDir()
	configPath := writeOpenTestConfig(t, cacheHome)
	icsPath := writeOpenTestICS(t, openTestICS("cached"))
	s := store.New(filepath.Join(cacheHome, "calterm"))
	start := time.Date(2026, 9, 15, 11, 0, 0, 0, time.UTC)
	if err := s.SaveOccurrences(&store.OccurrenceIndex{Occurrences: []model.Occurrence{{
		UID: "cached", Start: start, End: start.Add(45 * time.Minute), Summary: "Cached copy",
		AccountID: "work", CalendarID: "hidden",
	}}}); err != nil {
		t.Fatal(err)
	}

	oldRunner := runFocusedTUI
	defer func() { runFocusedTUI = oldRunner }()
	var got tui.FocusOptions
	runFocusedTUI = func(_ *config.Config, _ *store.Store, _ time.Time, _ string, opts tui.FocusOptions) error {
		got = opts
		return nil
	}

	var out, errOut bytes.Buffer
	if code := run([]string{"open", "-config", configPath, icsPath}, &out, &errOut); code != 0 {
		t.Fatalf("exit code = %d, stderr: %s", code, errOut.String())
	}
	if got.ReadOnly || got.Occurrence.AccountID != "work" || got.Occurrence.CalendarID != "hidden" {
		t.Fatalf("focus options = %+v", got)
	}
}

func TestOpenCommandContinuesAfterSyncFailure(t *testing.T) {
	cacheHome := t.TempDir()
	configPath := writeOpenTestConfig(t, cacheHome)
	icsPath := writeOpenTestICS(t, openTestICS("cached"))
	s := store.New(filepath.Join(cacheHome, "calterm"))
	start := time.Date(2026, 9, 15, 11, 0, 0, 0, time.UTC)
	if err := s.SaveOccurrences(&store.OccurrenceIndex{Occurrences: []model.Occurrence{{
		UID: "cached", Start: start, End: start.Add(45 * time.Minute),
		AccountID: "work", CalendarID: "team",
	}}}); err != nil {
		t.Fatal(err)
	}

	oldSync, oldRunner := syncForOpen, runFocusedTUI
	defer func() { syncForOpen, runFocusedTUI = oldSync, oldRunner }()
	syncCalled, runnerCalled := false, false
	syncForOpen = func(*config.Config, *store.Store, time.Time, io.Writer) error {
		syncCalled = true
		return errors.New("offline")
	}
	var got tui.FocusOptions
	runFocusedTUI = func(_ *config.Config, _ *store.Store, _ time.Time, _ string, opts tui.FocusOptions) error {
		runnerCalled = true
		got = opts
		return nil
	}

	var out, errOut bytes.Buffer
	if code := run([]string{"open", "--sync", "-config", configPath, icsPath}, &out, &errOut); code != 0 {
		t.Fatalf("exit code = %d, stderr: %s", code, errOut.String())
	}
	if !syncCalled || !runnerCalled || got.ReadOnly {
		t.Fatalf("syncCalled=%v runnerCalled=%v opts=%+v", syncCalled, runnerCalled, got)
	}
	if !got.NoticeIsError || !strings.Contains(got.Notice, "sync failed; using existing cache: offline") {
		t.Errorf("notice = %q, error=%v", got.Notice, got.NoticeIsError)
	}
}

func TestOpenCommandRejectsMultipleEvents(t *testing.T) {
	configPath := writeOpenTestConfig(t, t.TempDir())
	icsPath := writeOpenTestICS(t, "BEGIN:VCALENDAR\r\nVERSION:2.0\r\n"+
		"BEGIN:VEVENT\r\nUID:a\r\nDTSTART:20260915T110000Z\r\nEND:VEVENT\r\n"+
		"BEGIN:VEVENT\r\nUID:b\r\nDTSTART:20260916T110000Z\r\nEND:VEVENT\r\n"+
		"END:VCALENDAR\r\n")

	var out, errOut bytes.Buffer
	if code := run([]string{"open", "-config", configPath, icsPath}, &out, &errOut); code == 0 {
		t.Fatal("exit code = 0, want malformed multi-event invitation rejected")
	}
	if !strings.Contains(errOut.String(), "want exactly one") {
		t.Errorf("stderr = %q", errOut.String())
	}
}

func TestCalendarID(t *testing.T) {
	tests := []struct{ href, want string }{
		{"/dav/calendars/calendar-user/work/", "work"},
		{"/dav/calendars/calendar-user/work", "work"},
		{"https://example.com/dav/cal/personal/", "personal"},
		{"work", "work"},
		{"/", "default"},
	}
	for _, tt := range tests {
		if got := calendarID(tt.href); got != tt.want {
			t.Errorf("calendarID(%q) = %q, want %q", tt.href, got, tt.want)
		}
	}
}

// TestRunSyncKeepsMetadataForAFailedAccount guards against a transient
// account failure silently erasing that account's calendars from the
// derived index. runSync always builds meta.Accounts fresh each run: if a
// failed account's entry were simply dropped instead of carried forward,
// the unconditional s.SaveMeta(meta) call below would overwrite meta.json
// and erase that account's calendars even though its cached objects are
// still on disk untouched.
//
// "personal" is served by a real httptest CalDAV server so it succeeds and
// SaveMeta actually runs (a variant where every account merely fails never
// reaches SaveMeta, via the "every account failed" guard, and so cannot
// exercise this bug at all). "work" points at an unreachable address
// (127.0.0.1:1, refused immediately -- the same address internal/sync's
// tests use for this purpose) so it fails while personal succeeds.
// newCalDAVTestServer serves the minimal PROPFIND/REPORT exchange one healthy
// account needs: discovery, a CTag, one object ETag, and one multiget body.
func newCalDAVTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	const calProps = "getctag"
	const objETags = "getetag"
	const principal = "current-user-principal"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := new(strings.Builder)
		buf := make([]byte, 1<<16)
		n, _ := r.Body.Read(buf)
		body.Write(buf[:n])

		w.Header().Set("Content-Type", "application/xml")
		w.WriteHeader(http.StatusMultiStatus)

		switch {
		case strings.Contains(body.String(), principal):
			// No current-user-principal href: Discover falls back to
			// treating the configured base URL as the calendar home.
			w.Write([]byte(`<?xml version="1.0"?><d:multistatus xmlns:d="DAV:"></d:multistatus>`))

		case strings.Contains(body.String(), calProps) && r.Header.Get("Depth") == "1":
			// Discover's calendar listing under the (fallback) home.
			w.Write([]byte(`<?xml version="1.0"?>
<d:multistatus xmlns:d="DAV:" xmlns:cs="http://calendarserver.org/ns/">
  <d:response><d:href>/dav/personal-cal/</d:href><d:propstat>
    <d:prop>
      <d:displayname>Personal</d:displayname>
      <cs:getctag>ctag-1</cs:getctag>
      <d:resourcetype><d:collection/><c:calendar xmlns:c="urn:ietf:params:xml:ns:caldav"/></d:resourcetype>
    </d:prop>
    <d:status>HTTP/1.1 200 OK</d:status>
  </d:propstat></d:response>
</d:multistatus>`))

		case strings.Contains(body.String(), calProps) && r.Header.Get("Depth") == "0":
			// sync.Calendar's CTag check.
			w.Write([]byte(`<?xml version="1.0"?>
<d:multistatus xmlns:d="DAV:" xmlns:cs="http://calendarserver.org/ns/">
  <d:response><d:href>/dav/personal-cal/</d:href><d:propstat>
    <d:prop><cs:getctag>ctag-1</cs:getctag></d:prop>
    <d:status>HTTP/1.1 200 OK</d:status>
  </d:propstat></d:response>
</d:multistatus>`))

		case r.Method != "REPORT" && strings.Contains(body.String(), objETags):
			// sync.Calendar's object ETag listing.
			w.Write([]byte(`<?xml version="1.0"?>
<d:multistatus xmlns:d="DAV:">
  <d:response><d:href>/dav/personal-cal/a.ics</d:href><d:propstat>
    <d:prop><d:getetag>"etag-a1"</d:getetag></d:prop>
    <d:status>HTTP/1.1 200 OK</d:status>
  </d:propstat></d:response>
</d:multistatus>`))

		case r.Method == "REPORT":
			// sync.Calendar's multiget.
			w.Write([]byte(`<?xml version="1.0"?>
<d:multistatus xmlns:d="DAV:" xmlns:c="urn:ietf:params:xml:ns:caldav">
  <d:response><d:href>/dav/personal-cal/a.ics</d:href><d:propstat>
    <d:prop><d:getetag>"etag-a1"</d:getetag><c:calendar-data>BEGIN:VCALENDAR
VERSION:2.0
BEGIN:VEVENT
UID:a
DTSTAMP:20260101T000000Z
DTSTART:20260610T100000Z
DTEND:20260610T103000Z
SUMMARY:Alpha
END:VEVENT
END:VCALENDAR
</c:calendar-data></d:prop>
    <d:status>HTTP/1.1 200 OK</d:status>
  </d:propstat></d:response>
</d:multistatus>`))
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestRunSyncKeepsMetadataForAFailedAccount(t *testing.T) {
	srv := newCalDAVTestServer(t)

	s := store.New(t.TempDir())

	seed := &store.Meta{
		Accounts: []store.AccountMeta{
			{
				Name: "work",
				Calendars: []store.CalendarMeta{
					{ID: "work-cal", Path: "/dav/work/", Name: "Work"},
				},
			},
		},
	}
	if err := s.SaveMeta(seed); err != nil {
		t.Fatalf("seeding metadata: %v", err)
	}

	cfg := &config.Config{
		Accounts: []config.Account{
			{Name: "work", URL: "http://127.0.0.1:1", Username: "u", PasswordCmd: "echo hunter2"},
			{Name: "personal", URL: srv.URL + "/dav/", Username: "u", PasswordCmd: "echo hunter2"},
		},
	}

	var out bytes.Buffer
	err := runSync(cfg, s, time.Unix(0, 0), &out)
	if err == nil {
		t.Fatal("runSync returned nil error, want an error since the work account fails")
	}

	got, err := s.LoadMeta()
	if err != nil {
		t.Fatalf("reloading metadata: %v", err)
	}
	byName := map[string]store.AccountMeta{}
	for _, am := range got.Accounts {
		byName[am.Name] = am
	}
	work, ok := byName["work"]
	if !ok {
		t.Fatalf("work account missing from metadata after failed sync; got accounts: %+v", got.Accounts)
	}
	if len(work.Calendars) != 1 || work.Calendars[0].ID != "work-cal" {
		t.Errorf("work account calendars = %+v, want the pre-seeded work-cal entry preserved", work.Calendars)
	}
	personal, ok := byName["personal"]
	if !ok {
		t.Fatalf("personal account missing from metadata after successful sync; got accounts: %+v", got.Accounts)
	}
	if len(personal.Calendars) != 1 || personal.Calendars[0].ID != "personal-cal" {
		t.Errorf("personal account calendars = %+v, want the freshly discovered personal-cal entry", personal.Calendars)
	}
}

func TestWaybarCommandAlwaysEmitsJSON(t *testing.T) {
	var out, errOut bytes.Buffer
	// A deliberately broken config: the module must still print valid JSON.
	code := run([]string{"waybar", "-config", "/nonexistent/calterm.toml"}, &out, &errOut)
	if code != 0 {
		t.Errorf("exit code = %d, want 0: waybar degrades to a visible warning rather than failing", code)
	}
	var decoded map[string]any
	if err := json.Unmarshal(out.Bytes(), &decoded); err != nil {
		t.Fatalf("output is not valid JSON (%v): %s", err, out.String())
	}
	if decoded["class"] != "stale" {
		t.Errorf("class = %v, want stale", decoded["class"])
	}
}

// The three tests below pin the rule that index freshness is EARNED by a
// successful fetch. Before the fix, runSync stamped occurrences.json with
// `now` on every run that got past the "every account failed" guard -- and
// because a failed account's previous AccountMeta is carried forward, that
// guard never fires once any prior metadata exists. A user whose password
// expired or whose VPN dropped would see a permanently "fresh" cache full of
// last week's meetings, and waybar's staleness warning -- the only signal that
// syncing has stopped -- could never fire.

const oldSyncRFC3339 = "2026-06-01T00:00:00Z"

func seedStaleAccount(t *testing.T, s *store.Store, name string) time.Time {
	t.Helper()
	old, err := time.Parse(time.RFC3339, oldSyncRFC3339)
	if err != nil {
		t.Fatal(err)
	}
	seed := &store.Meta{
		LastSync: old,
		Accounts: []store.AccountMeta{{
			Name:     name,
			LastSync: old,
			Calendars: []store.CalendarMeta{
				{ID: "work-cal", Path: "/dav/work/", Name: "Work"},
			},
		}},
	}
	if err := s.SaveMeta(seed); err != nil {
		t.Fatalf("seeding metadata: %v", err)
	}
	if err := s.SaveOccurrences(&store.OccurrenceIndex{GeneratedAt: old}); err != nil {
		t.Fatalf("seeding occurrences: %v", err)
	}
	return old
}

func TestRunSyncTotalFailureDoesNotAdvanceGeneratedAt(t *testing.T) {
	s := store.New(t.TempDir())
	old := seedStaleAccount(t, s, "work")

	cfg := &config.Config{Accounts: []config.Account{
		{Name: "work", URL: "http://127.0.0.1:1", Username: "u", PasswordCmd: "echo hunter2"},
	}}

	now := time.Date(2026, 6, 20, 12, 0, 0, 0, time.UTC)
	var out bytes.Buffer
	if err := runSync(cfg, s, now, &out); err == nil {
		t.Fatal("runSync returned nil error, want an error when the only account is unreachable")
	}

	idx, err := s.LoadOccurrences()
	if err != nil {
		t.Fatalf("reloading occurrences: %v", err)
	}
	if !idx.GeneratedAt.Equal(old) {
		t.Errorf("GeneratedAt = %v, want it left at the last successful fetch %v; a failed sync must not mint freshness", idx.GeneratedAt, old)
	}
	if idx.Age(now) < 19*24*time.Hour {
		t.Errorf("Age = %v, want ~19 days: the staleness warning must still be able to fire", idx.Age(now))
	}
}

func TestRunSyncPartialFailureKeepsTheFailedAccountsOlderTimestamp(t *testing.T) {
	srv := newCalDAVTestServer(t)
	s := store.New(t.TempDir())
	old := seedStaleAccount(t, s, "work")

	cfg := &config.Config{Accounts: []config.Account{
		{Name: "work", URL: "http://127.0.0.1:1", Username: "u", PasswordCmd: "echo hunter2"},
		{Name: "personal", URL: srv.URL + "/dav/", Username: "u", PasswordCmd: "echo hunter2"},
	}}

	now := time.Date(2026, 6, 20, 12, 0, 0, 0, time.UTC)
	var out bytes.Buffer
	if err := runSync(cfg, s, now, &out); err == nil {
		t.Fatal("runSync returned nil error, want an error since the work account fails")
	}

	idx, err := s.LoadOccurrences()
	if err != nil {
		t.Fatalf("reloading occurrences: %v", err)
	}
	if !idx.GeneratedAt.Equal(old) {
		t.Errorf("GeneratedAt = %v, want the OLDEST account's last successful fetch %v; a healthy account must not vouch for a dead one", idx.GeneratedAt, old)
	}
}

func TestRunSyncSuccessAdvancesGeneratedAt(t *testing.T) {
	srv := newCalDAVTestServer(t)
	s := store.New(t.TempDir())

	cfg := &config.Config{Accounts: []config.Account{
		{Name: "personal", URL: srv.URL + "/dav/", Username: "u", PasswordCmd: "echo hunter2"},
	}}

	now := time.Date(2026, 6, 20, 12, 0, 0, 0, time.UTC)
	var out bytes.Buffer
	if err := runSync(cfg, s, now, &out); err != nil {
		t.Fatalf("runSync: %v (output: %s)", err, out.String())
	}

	idx, err := s.LoadOccurrences()
	if err != nil {
		t.Fatalf("reloading occurrences: %v", err)
	}
	if !idx.GeneratedAt.Equal(now) {
		t.Errorf("GeneratedAt = %v, want %v: a fully successful sync must advance freshness", idx.GeneratedAt, now)
	}
}

func TestRunSyncIndexesWebCalSubscription(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("method = %s, want GET", r.Method)
		}
		w.Header().Set("Content-Type", "text/calendar")
		w.Header().Set("ETag", `"waste-v1"`)
		_, _ = io.WriteString(w, "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nX-WR-CALNAME:Waste collection\r\n"+
			"BEGIN:VEVENT\r\nUID:waste-1\r\nDTSTART;VALUE=DATE:20260924\r\n"+
			"DTEND;VALUE=DATE:20260925\r\nSUMMARY:Recycling pickup\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n")
	}))
	defer server.Close()

	s := store.New(t.TempDir())
	cfg := &config.Config{WebCals: []config.WebCal{{
		Name: "waste", URL: server.URL + "/calendar.ics", Color: "#f5a97f",
	}}}
	now := time.Date(2026, 9, 11, 12, 0, 0, 0, time.Local)
	var out bytes.Buffer
	if err := runSync(cfg, s, now, &out); err != nil {
		t.Fatalf("runSync: %v (output: %s)", err, out.String())
	}
	if !strings.Contains(out.String(), "webcal/waste: 1 fetched") {
		t.Errorf("output = %q", out.String())
	}

	meta, err := s.LoadMeta()
	if err != nil {
		t.Fatal(err)
	}
	if len(meta.Accounts) != 1 || meta.Accounts[0].Name != "webcal" || len(meta.Accounts[0].Calendars) != 1 {
		t.Fatalf("metadata = %+v", meta)
	}
	cal := meta.Accounts[0].Calendars[0]
	if cal.ID != "waste" || cal.Name != "Waste collection" || cal.Color != "#f5a97f" {
		t.Errorf("calendar metadata = %+v", cal)
	}

	idx, err := s.LoadOccurrences()
	if err != nil {
		t.Fatal(err)
	}
	if len(idx.Occurrences) != 1 {
		t.Fatalf("occurrences = %+v", idx.Occurrences)
	}
	got := idx.Occurrences[0]
	if got.AccountID != "webcal" || got.CalendarID != "waste" || got.Summary != "Recycling pickup" || !got.AllDay {
		t.Errorf("occurrence = %+v", got)
	}
}

func TestRunCalendarSyncRefreshesKnownCollectionWithoutDiscovery(t *testing.T) {
	srv := newCalDAVTestServer(t)
	s := store.New(t.TempDir())
	old := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	meta := &store.Meta{Accounts: []store.AccountMeta{{
		Name: "personal", LastSync: old,
		Calendars: []store.CalendarMeta{{ID: "personal-cal", Path: "/dav/personal-cal/", Name: "Personal"}},
	}}}
	if err := s.SaveMeta(meta); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{Accounts: []config.Account{{
		Name: "personal", URL: srv.URL + "/dav/", Username: "u", PasswordCmd: "echo hunter2",
	}}}

	if err := runCalendarSync(context.Background(), cfg, s, "personal", "personal-cal", old.Add(time.Hour)); err != nil {
		t.Fatalf("runCalendarSync: %v", err)
	}
	idx, err := s.LoadOccurrences()
	if err != nil {
		t.Fatal(err)
	}
	if len(idx.Occurrences) != 1 || idx.Occurrences[0].Summary != "Alpha" {
		t.Fatalf("occurrences = %+v, want refreshed Alpha event", idx.Occurrences)
	}
	gotMeta, err := s.LoadMeta()
	if err != nil {
		t.Fatal(err)
	}
	if !gotMeta.Accounts[0].LastSync.Equal(old) {
		t.Errorf("targeted refresh changed whole-account freshness from %v to %v", old, gotMeta.Accounts[0].LastSync)
	}
}

func TestRunSyncSynchronizesCalendarCollectionsInParallel(t *testing.T) {
	started := make(chan struct{}, 2)
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bodyBytes, _ := io.ReadAll(r.Body)
		body := string(bodyBytes)
		w.Header().Set("Content-Type", "application/xml")
		w.WriteHeader(http.StatusMultiStatus)

		switch {
		case strings.Contains(body, "current-user-principal"):
			w.Write([]byte(`<?xml version="1.0"?><d:multistatus xmlns:d="DAV:"></d:multistatus>`))
		case strings.Contains(body, "getctag") && r.Header.Get("Depth") == "1":
			w.Write([]byte(`<?xml version="1.0"?>
<d:multistatus xmlns:d="DAV:" xmlns:cs="http://calendarserver.org/ns/" xmlns:c="urn:ietf:params:xml:ns:caldav">
  <d:response><d:href>/dav/a/</d:href><d:propstat><d:prop><d:displayname>A</d:displayname><cs:getctag>a1</cs:getctag><d:resourcetype><d:collection/><c:calendar/></d:resourcetype></d:prop><d:status>HTTP/1.1 200 OK</d:status></d:propstat></d:response>
  <d:response><d:href>/dav/b/</d:href><d:propstat><d:prop><d:displayname>B</d:displayname><cs:getctag>b1</cs:getctag><d:resourcetype><d:collection/><c:calendar/></d:resourcetype></d:prop><d:status>HTTP/1.1 200 OK</d:status></d:propstat></d:response>
</d:multistatus>`))
		case strings.Contains(body, "getctag") && r.Header.Get("Depth") == "0":
			started <- struct{}{}
			<-release
			ctag := strings.Trim(r.URL.Path, "/")
			w.Write([]byte(fmt.Sprintf(`<?xml version="1.0"?><d:multistatus xmlns:d="DAV:" xmlns:cs="http://calendarserver.org/ns/"><d:response><d:href>%s</d:href><d:propstat><d:prop><cs:getctag>%s</cs:getctag></d:prop><d:status>HTTP/1.1 200 OK</d:status></d:propstat></d:response></d:multistatus>`, r.URL.Path, ctag)))
		case strings.Contains(body, "getetag"):
			w.Write([]byte(`<?xml version="1.0"?><d:multistatus xmlns:d="DAV:"></d:multistatus>`))
		default:
			t.Errorf("unexpected CalDAV request %s %s: %s", r.Method, r.URL.Path, body)
		}
	}))
	defer srv.Close()

	s := store.New(t.TempDir())
	cfg := &config.Config{Accounts: []config.Account{{
		Name: "work", URL: srv.URL + "/dav/", Username: "u", PasswordCmd: "echo hunter2",
	}}}
	done := make(chan error, 1)
	go func() { done <- runSync(cfg, s, time.Now(), io.Discard) }()

	for i := 0; i < 2; i++ {
		select {
		case <-started:
		case <-time.After(3 * time.Second):
			close(release)
			t.Fatal("second calendar did not start while the first was blocked; sync is serial")
		}
	}
	close(release)
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("runSync: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("parallel sync did not finish after releasing both calendars")
	}
}
