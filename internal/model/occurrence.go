package model

import (
	"net/url"
	"strings"
	"time"
)

// Window is a half-open time range [From, To).
type Window struct {
	From time.Time
	To   time.Time
}

// Occurrence is a single materialised instance of a calendar event. A
// non-recurring event yields exactly one; a recurring event yields one per
// generated instant within the query window.
type Occurrence struct {
	UID string `json:"uid"`
	// RecurrenceID identifies which instance of a recurring series this is,
	// as RFC 3339 in UTC. Empty for a non-recurring event.
	RecurrenceID string `json:"recurrence_id,omitempty"`

	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
	// AllDay marks a date-valued event. All-day Start and End are anchored to
	// midnight UTC and must be compared as dates, never as instants: treating
	// them as instants makes them drift across DST boundaries.
	AllDay bool `json:"all_day"`

	Summary     string   `json:"summary"`
	Location    string   `json:"location,omitempty"`
	Description string   `json:"description,omitempty"`
	Categories  []string `json:"categories,omitempty"`
	URL         string   `json:"url,omitempty"`
	Status      string   `json:"status,omitempty"`
	// RRuleText is the raw RRULE of the series, kept for the detail view.
	RRuleText string `json:"rrule,omitempty"`

	// Organizer and Attendees retain the scheduling information that used to
	// be discarded while building the occurrence index. Participant status is
	// not the same thing as VEVENT STATUS: a confirmed event may still be
	// waiting for this user to accept or decline it.
	Organizer *Participant  `json:"organizer,omitempty"`
	Attendees []Participant `json:"attendees,omitempty"`
	// AttendeeStatus and RSVPRequested describe the configured identity for
	// this account. They are populated by MarkIdentity when the index is
	// loaded, so changing an account's email does not require a network sync.
	AttendeeStatus string `json:"attendee_status,omitempty"`
	RSVPRequested  bool   `json:"rsvp_requested,omitempty"`

	AccountID  string `json:"account"`
	CalendarID string `json:"calendar"`
}

// Participant is an ORGANIZER or ATTENDEE from the source VEVENT.
type Participant struct {
	Name   string `json:"name,omitempty"`
	Email  string `json:"email,omitempty"`
	Status string `json:"status,omitempty"`
	Role   string `json:"role,omitempty"`
	RSVP   bool   `json:"rsvp,omitempty"`
	Self   bool   `json:"self,omitempty"`
}

// EmailAddress normalizes an iCalendar CAL-ADDRESS for identity comparisons.
// Mail addresses are case-insensitive for the purpose of invitation matching;
// a percent-encoded address is decoded when possible.
func EmailAddress(address string) string {
	address = strings.TrimSpace(address)
	if len(address) >= len("mailto:") && strings.EqualFold(address[:len("mailto:")], "mailto:") {
		address = address[len("mailto:"):]
	}
	if decoded, err := url.PathUnescape(address); err == nil {
		address = decoded
	}
	return strings.ToLower(strings.TrimSpace(address))
}

// MarkIdentity identifies the attendee belonging to the account owner and
// lifts that attendee's response state onto the occurrence for compact views.
func (o *Occurrence) MarkIdentity(email string) {
	identity := EmailAddress(email)
	o.AttendeeStatus = ""
	o.RSVPRequested = false
	for i := range o.Attendees {
		a := &o.Attendees[i]
		a.Self = identity != "" && EmailAddress(a.Email) == identity
		if !a.Self || o.AttendeeStatus != "" {
			continue
		}
		o.AttendeeStatus = strings.ToUpper(a.Status)
		if o.AttendeeStatus == "" {
			// NEEDS-ACTION is ATTENDEE's RFC 5545 default PARTSTAT.
			o.AttendeeStatus = "NEEDS-ACTION"
		}
		o.RSVPRequested = a.RSVP
	}
}

// CalendarKey identifies a calendar globally.
//
// A calendar's ID is the last path segment of its collection href, so two
// accounts that each expose /personal/ share the ID "personal". Anything that
// maps a calendar to a display name or a colour must therefore key on the
// account as well, or one account's calendar silently wins for both. The
// on-disk store is already account-scoped and needs no such care.
func CalendarKey(accountID, calendarID string) string {
	return accountID + "/" + calendarID
}

// CalendarKey is the account-scoped identity of this occurrence's calendar.
func (o Occurrence) CalendarKey() string {
	return CalendarKey(o.AccountID, o.CalendarID)
}

// HiddenSet turns a config's hidden list into a lookup.
//
// Entries are normally qualified as "account/calendar" so that two accounts
// each exposing a calendar with the same ID can be hidden independently. A bare
// calendar ID is also accepted, because a config written by hand -- or written
// before this feature existed -- may contain one; it matches that calendar in
// every account.
func HiddenSet(hidden []string) map[string]bool {
	if len(hidden) == 0 {
		return nil
	}
	set := make(map[string]bool, len(hidden))
	for _, h := range hidden {
		set[h] = true
	}
	return set
}

// IsHidden reports whether a calendar is hidden, accepting either the
// qualified "account/calendar" form or a bare calendar ID.
func IsHidden(set map[string]bool, accountID, calendarID string) bool {
	if len(set) == 0 {
		return false
	}
	return set[CalendarKey(accountID, calendarID)] || set[calendarID]
}

// RecurrenceKey renders an instant as the canonical key used to match
// RECURRENCE-ID overrides against generated occurrences.
func RecurrenceKey(t time.Time) string {
	return t.UTC().Format(time.RFC3339)
}

// Overlaps reports whether the occurrence intersects [from, to).
// A zero-length occurrence counts as overlapping when it starts in range.
func (o Occurrence) Overlaps(from, to time.Time) bool {
	if !o.Start.Before(to) {
		return false
	}
	if o.End.Equal(o.Start) {
		return !o.Start.Before(from)
	}
	return o.End.After(from)
}
