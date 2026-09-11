// Package rsvp updates invitation participation through the original CalDAV
// scheduling object. The calendar server is responsible for notifying the
// organizer; calterm never opens or sends a personal email.
package rsvp

import (
	"bytes"
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/emersion/go-ical"

	"github.com/tonobo/calterm/internal/caldav"
	"github.com/tonobo/calterm/internal/config"
	"github.com/tonobo/calterm/internal/model"
	"github.com/tonobo/calterm/internal/store"
)

// Decision is an iCalendar ATTENDEE PARTSTAT value supported by calterm.
type Decision string

const (
	Accepted Decision = "ACCEPTED"
	Declined Decision = "DECLINED"
)

// Respond changes the configured identity's PARTSTAT on the invitation's
// existing CalDAV resource. The conditional PUT prevents a stale cached copy
// from overwriting an organizer update.
func Respond(ctx context.Context, acct config.Account, s *store.Store, occurrence model.Occurrence, decision Decision) error {
	if decision != Accepted && decision != Declined {
		return fmt.Errorf("unsupported invitation response %q", decision)
	}
	if s == nil {
		return fmt.Errorf("calendar cache is unavailable")
	}
	if acct.Email == "" {
		return fmt.Errorf("account %q needs email in config.toml", acct.Name)
	}

	idx, err := s.LoadIndex(occurrence.AccountID, occurrence.CalendarID)
	if err != nil {
		return fmt.Errorf("loading calendar object index: %w", err)
	}
	ref, err := objectRefForUID(idx, occurrence.UID)
	if err != nil {
		return err
	}
	raw, err := s.ReadObject(occurrence.AccountID, occurrence.CalendarID, occurrence.UID)
	if err != nil {
		return fmt.Errorf("reading cached invitation: %w", err)
	}
	updated, err := updateParticipation(raw, occurrence.UID, acct.Email, decision)
	if err != nil {
		return err
	}

	password, err := acct.Password()
	if err != nil {
		return err
	}
	client, err := caldav.NewClient(acct.URL, acct.Username, password, acct.InsecureSkipVerify)
	if err != nil {
		return fmt.Errorf("opening CalDAV account %q: %w", acct.Name, err)
	}

	// Force the follow-up sync through its ETag comparison even if this
	// server's CTag update is delayed. Doing this before PUT is conservative:
	// a failed PUT merely causes one harmless full comparison next time.
	idx.CTag = ""
	if err := s.SaveIndex(occurrence.AccountID, occurrence.CalendarID, idx); err != nil {
		return fmt.Errorf("preparing cache refresh: %w", err)
	}
	if err := client.PutCalendarObject(ctx, ref.Href, ref.ETag, updated); err != nil {
		return fmt.Errorf("updating invitation: %w", err)
	}
	return nil
}

func objectRefForUID(idx *store.Index, uid string) (store.ObjectRef, error) {
	var matches []store.ObjectRef
	for _, ref := range idx.Objects {
		if ref.UID == uid {
			matches = append(matches, ref)
		}
	}
	if len(matches) == 0 {
		return store.ObjectRef{}, fmt.Errorf("invitation UID %q is missing from the calendar object index", uid)
	}
	sort.Slice(matches, func(i, j int) bool { return matches[i].Href < matches[j].Href })
	if len(matches) > 1 {
		return store.ObjectRef{}, fmt.Errorf("invitation UID %q has multiple calendar resources", uid)
	}
	if matches[0].ETag == "" {
		return store.ObjectRef{}, fmt.Errorf("invitation UID %q has no cached ETag; sync and retry", uid)
	}
	return matches[0], nil
}

func updateParticipation(raw []byte, uid, identity string, decision Decision) ([]byte, error) {
	cal, err := ical.NewDecoder(bytes.NewReader(raw)).Decode()
	if err != nil {
		return nil, fmt.Errorf("parsing cached invitation: %w", err)
	}
	if cal.Name != ical.CompCalendar {
		return nil, fmt.Errorf("cached invitation is not a VCALENDAR")
	}
	// Calendar object resources fetched from CalDAV do not carry an iTIP
	// METHOD. Reject a transported mail payload rather than silently changing
	// anything except the attendee's PARTSTAT below.
	if cal.Props.Get(ical.PropMethod) != nil {
		return nil, fmt.Errorf("cached invitation contains METHOD; sync the CalDAV calendar and retry")
	}
	identity = model.EmailAddress(identity)
	matched := 0
	for _, event := range cal.Events() {
		eventUID, _ := event.Props.Text(ical.PropUID)
		if eventUID != uid {
			continue
		}
		organizer := event.Props.Get(ical.PropOrganizer)
		if organizer == nil {
			return nil, fmt.Errorf("invitation has no organizer")
		}
		agent := strings.ToUpper(organizer.Params.Get("SCHEDULE-AGENT"))
		if agent == "CLIENT" || agent == "NONE" {
			return nil, fmt.Errorf("invitation organizer uses SCHEDULE-AGENT=%s; automatic server delivery is disabled", agent)
		}
		for i, attendee := range event.Props.Values(ical.PropAttendee) {
			address := attendee.Params.Get(ical.ParamEmail)
			if address == "" {
				address = attendee.Value
			}
			if model.EmailAddress(address) != identity {
				continue
			}
			attendee.Params.Set(ical.ParamParticipationStatus, string(decision))
			event.Props[ical.PropAttendee][i] = attendee
			matched++
		}
	}
	if matched == 0 {
		return nil, fmt.Errorf("%s is not an attendee of this invitation", identity)
	}

	var out bytes.Buffer
	if err := ical.NewEncoder(&out).Encode(cal); err != nil {
		return nil, fmt.Errorf("encoding updated invitation: %w", err)
	}
	return out.Bytes(), nil
}
