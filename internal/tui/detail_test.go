package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/tonobo/calterm/internal/model"
)

func detailFixture() model.Occurrence {
	return model.Occurrence{
		UID:         "a",
		Summary:     "Design review",
		Location:    "Room 2",
		Description: "Walk through the new onboarding flow and agree next steps.",
		URL:         "https://example.com/meeting",
		Start:       time.Date(2026, 6, 10, 14, 0, 0, 0, time.UTC),
		End:         time.Date(2026, 6, 10, 15, 0, 0, 0, time.UTC),
		AccountID:   "personal",
		CalendarID:  "work",
		RRuleText:   "FREQ=WEEKLY;INTERVAL=2;BYDAY=TU",
	}
}

func TestRenderDetailShowsEveryField(t *testing.T) {
	got := RenderDetail(detailFixture(), 80, 24, time.UTC, NewStyles(true), testNames)
	for _, want := range []string{
		"Design review", "Room 2", "onboarding flow",
		"https://example.com/meeting", "Work", "14:00", "15:00",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("detail view is missing %q:\n%s", want, got)
		}
	}
}

func TestRenderDetailDescribesRecurrence(t *testing.T) {
	got := RenderDetail(detailFixture(), 80, 24, time.UTC, NewStyles(true), testNames)
	if !strings.Contains(strings.ToLower(got), "every 2 weeks") {
		t.Errorf("recurrence should be described in words:\n%s", got)
	}
}

func TestDescribeRRule(t *testing.T) {
	tests := []struct{ rule, want string }{
		{"FREQ=DAILY", "Every day"},
		{"FREQ=DAILY;INTERVAL=3", "Every 3 days"},
		{"FREQ=WEEKLY", "Every week"},
		{"FREQ=WEEKLY;INTERVAL=2;BYDAY=TU", "Every 2 weeks"},
		{"FREQ=MONTHLY", "Every month"},
		{"FREQ=YEARLY", "Every year"},
		{"FREQ=WEEKLY;COUNT=5", "Every week, 5 times"},
		{"", ""},
		{"GIBBERISH", "Repeats"},
	}
	for _, tt := range tests {
		if got := DescribeRRule(tt.rule); !strings.HasPrefix(got, tt.want) {
			t.Errorf("DescribeRRule(%q) = %q, want it to start with %q", tt.rule, got, tt.want)
		}
	}
}

func TestRenderDetailOmitsEmptyFields(t *testing.T) {
	o := model.Occurrence{
		Summary: "Bare",
		Start:   time.Date(2026, 6, 10, 14, 0, 0, 0, time.UTC),
		End:     time.Date(2026, 6, 10, 15, 0, 0, 0, time.UTC),
	}
	got := RenderDetail(o, 80, 24, time.UTC, NewStyles(true), testNames)
	for _, absent := range []string{"Location", "Description", "URL", "Repeats"} {
		if strings.Contains(got, absent) {
			t.Errorf("detail view shows an empty %q label:\n%s", absent, got)
		}
	}
}

func TestRenderDetailWrapsLongDescription(t *testing.T) {
	o := detailFixture()
	o.Description = strings.Repeat("word ", 200)
	got := RenderDetail(o, 60, 24, time.UTC, NewStyles(true), testNames)
	for i, line := range strings.Split(got, "\n") {
		if w := lipglossWidth(line); w > 60 {
			t.Errorf("line %d is %d cells wide, want <= 60", i, w)
		}
	}
}

func TestRenderDetailShowsLongLocationURLAsClickableLabel(t *testing.T) {
	o := detailFixture()
	target := "https://app.example.com/meeting/anonymous-session?calendar=team&occurrence=20260915T140000"
	o.Location = "Gather invite link:\n" + target
	o.URL = ""
	got := RenderDetail(o, 60, 30, time.UTC, NewStyles(true), testNames)

	if count := strings.Count(got, ansi.SetHyperlink(target)); count != 1 {
		t.Errorf("URL has %d complete hyperlink targets, want one:\n%q", count, got)
	}
	plain := ansi.Strip(got)
	if !strings.Contains(plain, "↗ open link") || strings.Contains(plain, target) {
		t.Errorf("URL was not replaced by a compact clickable label:\n%s", plain)
	}
	for i, line := range strings.Split(got, "\n") {
		if width := lipglossWidth(line); width > 60 {
			t.Errorf("line %d is %d cells wide, want <= 60: %q", i, width, ansi.Strip(line))
		}
	}
}

func TestRenderDetailAllDay(t *testing.T) {
	o := model.Occurrence{
		Summary: "Holiday", AllDay: true,
		Start: time.Date(2026, 6, 11, 0, 0, 0, 0, time.UTC),
		End:   time.Date(2026, 6, 12, 0, 0, 0, 0, time.UTC),
	}
	got := RenderDetail(o, 80, 24, time.UTC, NewStyles(true), testNames)
	if !strings.Contains(got, "All day") {
		t.Errorf("an all-day event should say so rather than showing 00:00:\n%s", got)
	}
	if strings.Contains(got, "00:00") {
		t.Errorf("an all-day event must not render midnight times:\n%s", got)
	}
}

// Two accounts each exposing /personal/ share the calendar ID "personal", so
// a names map keyed on the bare ID lets one account's display name win for
// both. The key must be account-scoped.
func TestRenderDetailCalendarNameIsScopedByAccount(t *testing.T) {
	names := map[string]string{
		"home/personal": "Home Calendar",
		"work/personal": "Work Calendar",
	}
	start := time.Date(2026, 6, 10, 9, 0, 0, 0, time.UTC)
	for _, tt := range []struct{ account, want string }{
		{"home", "Home Calendar"},
		{"work", "Work Calendar"},
	} {
		o := model.Occurrence{
			UID: "a", Summary: "Standup", Start: start, End: start.Add(time.Hour),
			AccountID: tt.account, CalendarID: "personal",
		}
		got := RenderDetail(o, 60, 20, time.UTC, NewStyles(true), names)
		if !strings.Contains(got, tt.want) {
			t.Errorf("account %q: detail does not name %q:\n%s", tt.account, tt.want, got)
		}
	}
}

func TestRenderDetailShowsAttendeesAndOwnPendingResponse(t *testing.T) {
	o := detailFixture()
	o.Organizer = &model.Participant{Name: "Organizer", Email: "organizer@example.com"}
	o.Attendees = []model.Participant{
		{Name: "Organizer", Email: "organizer@example.com", Status: "ACCEPTED"},
		{Name: "User", Email: "user@example.com", Status: "NEEDS-ACTION", Self: true},
		{Name: "Other Attendee", Email: "attendee@example.com", Status: "DECLINED"},
	}
	o.AttendeeStatus = "NEEDS-ACTION"

	got := stripANSI(RenderDetail(o, 100, 30, time.UTC, NewStyles(true), testNames))
	for _, want := range []string{
		"Response", "needs action · space r a accept · space r d decline",
		"Organizer", "Organizer <organizer@example.com>", "Attendees (3)",
		"✓ Organizer", "? User <user@example.com> (you)", "× Other Attendee", "declined",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("detail view is missing %q:\n%s", want, got)
		}
	}
}

func TestRenderDetailHighlightsOwnAttendeeLabel(t *testing.T) {
	o := detailFixture()
	o.Attendees = []model.Participant{
		{Name: "User", Email: "user@example.com", Status: "NEEDS-ACTION", Self: true},
	}
	st := NewStyles(true)

	got := RenderDetail(o, 100, 30, time.UTC, st, testNames)
	want := st.Selected.Render("  ? User <user@example.com> (you) — ")
	if !strings.Contains(got, want) {
		t.Errorf("own attendee row is not highlighted with Selected style:\n%s", got)
	}
}

func TestRenderDetailColoursParticipationStatuses(t *testing.T) {
	o := detailFixture()
	o.Attendees = []model.Participant{
		{Name: "Accepted", Status: "ACCEPTED"},
		{Name: "Pending", Status: "NEEDS-ACTION"},
		{Name: "Declined", Status: "DECLINED"},
	}
	st := NewStyles(true)

	got := RenderDetail(o, 100, 30, time.UTC, st, testNames)
	for label, styled := range map[string]string{
		"accepted":     st.Success.Render("accepted"),
		"needs action": st.Pending.Render("needs action"),
		"declined":     st.Error.Render("declined"),
	} {
		if !strings.Contains(got, styled) {
			t.Errorf("%s status is not rendered with its semantic colour:\n%s", label, got)
		}
	}
}

// The same collision in the colour map: the server's own colour for one
// account's /personal/ must not be applied to the other's.
func TestCalendarColoursAreScopedByAccount(t *testing.T) {
	st := NewStyles(true)
	st.SetCalendarColors(map[string]string{"home/personal": "#ff0000"})

	home := st.Calendar(model.CalendarKey("home", "personal")).Render("x")
	work := st.Calendar(model.CalendarKey("work", "personal")).Render("x")
	if home == work {
		t.Errorf("both accounts' /personal/ rendered identically (%q); the colour map key must be account-scoped", home)
	}
	if !strings.Contains(home, "255;0;0") {
		t.Errorf("home/personal = %q, want the server-supplied red", home)
	}
}
