package rsvp

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/tonobo/calterm/internal/config"
	"github.com/tonobo/calterm/internal/model"
	"github.com/tonobo/calterm/internal/store"
)

const invitation = "BEGIN:VCALENDAR\r\n" +
	"VERSION:2.0\r\n" +
	"PRODID:-//test//EN\r\n" +
	"BEGIN:VEVENT\r\n" +
	"UID:invite-1\r\n" +
	"DTSTAMP:20260901T080000Z\r\n" +
	"DTSTART:20260915T110000Z\r\n" +
	"DTEND:20260915T114500Z\r\n" +
	"SUMMARY:Planning session\r\n" +
	"ORGANIZER;CN=Organizer:mailto:organizer@example.com\r\n" +
	"ATTENDEE;CN=User;PARTSTAT=NEEDS-ACTION:mailto:user@example.com\r\n" +
	"ATTENDEE;CN=Other Attendee;PARTSTAT=ACCEPTED:mailto:attendee@example.com\r\n" +
	"END:VEVENT\r\n" +
	"END:VCALENDAR\r\n"

func TestRespondUpdatesOriginalCalDAVObject(t *testing.T) {
	var gotBody, gotIfMatch, gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		gotBody = string(body)
		gotIfMatch = r.Header.Get("If-Match")
		user, pass, _ := r.BasicAuth()
		gotAuth = user + ":" + pass
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	s := store.New(t.TempDir())
	if err := s.WriteObject("work", "personal", "invite-1", []byte(invitation)); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveIndex("work", "personal", &store.Index{CTag: "old-ctag", Objects: map[string]store.ObjectRef{
		"/dav/personal/invite.ics": {Href: "/dav/personal/invite.ics", ETag: "etag-42", UID: "invite-1"},
	}}); err != nil {
		t.Fatal(err)
	}
	acct := config.Account{
		Name: "work", URL: srv.URL, Username: "calendar-user", PasswordCmd: "printf hunter2", Email: "user@example.com",
	}
	o := model.Occurrence{UID: "invite-1", AccountID: "work", CalendarID: "personal"}
	if err := Respond(context.Background(), acct, s, o, Accepted); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"PARTSTAT=ACCEPTED", "mailto:user@example.com", "mailto:attendee@example.com"} {
		if !strings.Contains(gotBody, want) {
			t.Errorf("PUT body missing %q:\n%s", want, gotBody)
		}
	}
	if strings.Contains(gotBody, "METHOD:") {
		t.Errorf("PUT body unexpectedly contains METHOD:\n%s", gotBody)
	}
	if gotIfMatch != `"etag-42"` || gotAuth != "calendar-user:hunter2" {
		t.Errorf("If-Match=%q auth=%q", gotIfMatch, gotAuth)
	}
	idx, err := s.LoadIndex("work", "personal")
	if err != nil {
		t.Fatal(err)
	}
	if idx.CTag != "" {
		t.Errorf("CTag = %q, want invalidated for follow-up sync", idx.CTag)
	}
}

func TestUpdateParticipationRejectsClientSideDelivery(t *testing.T) {
	raw := strings.Replace(invitation, "ORGANIZER;CN=Organizer", "ORGANIZER;CN=Organizer;SCHEDULE-AGENT=CLIENT", 1)
	_, err := updateParticipation([]byte(raw), "invite-1", "user@example.com", Declined)
	if err == nil || !strings.Contains(err.Error(), "automatic server delivery is disabled") {
		t.Fatalf("error = %v", err)
	}
}

func TestUpdateParticipationRejectsNoSchedulingDelivery(t *testing.T) {
	raw := strings.Replace(invitation, "ORGANIZER;CN=Organizer", "ORGANIZER;CN=Organizer;SCHEDULE-AGENT=NONE", 1)
	_, err := updateParticipation([]byte(raw), "invite-1", "user@example.com", Declined)
	if err == nil || !strings.Contains(err.Error(), "automatic server delivery is disabled") {
		t.Fatalf("error = %v", err)
	}
}

func TestUpdateParticipationRejectsTransportedITIPMessage(t *testing.T) {
	raw := strings.Replace(invitation, "PRODID:-//test//EN\r\n", "PRODID:-//test//EN\r\nMETHOD:REQUEST\r\n", 1)
	_, err := updateParticipation([]byte(raw), "invite-1", "user@example.com", Declined)
	if err == nil || !strings.Contains(err.Error(), "contains METHOD") {
		t.Fatalf("error = %v", err)
	}
}

func TestUpdateParticipationRejectsIdentityNotOnInvitation(t *testing.T) {
	_, err := updateParticipation([]byte(invitation), "invite-1", "nobody@example.com", Declined)
	if err == nil || !strings.Contains(err.Error(), "not an attendee") {
		t.Fatalf("error = %v, want attendee error", err)
	}
}
