package model

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/emersion/go-ical"
)

// EventReference is the single VEVENT carried by an external iCalendar
// object. Occurrence contains everything the detail view can display, while
// Timezone retains the DTSTART timezone explicitly for matching diagnostics
// and callers that need to distinguish UTC from a floating date-time.
//
// It deliberately has no account or calendar identity: opening an external
// file is not an import, and those fields only become known after matching the
// reference against the synced occurrence index.
type EventReference struct {
	Occurrence
	Timezone  string
	Recurring bool
}

// ParseEventReference decodes exactly one VEVENT using the same go-ical
// parser and timing helpers as normal cache expansion. Calendar invitations
// containing multiple VEVENTs are rejected because choosing one implicitly
// would make aerc open a potentially unrelated event.
func ParseEventReference(r io.Reader, loc *time.Location) (EventReference, error) {
	cal, err := ical.NewDecoder(r).Decode()
	if err != nil {
		return EventReference{}, fmt.Errorf("decoding iCalendar: %w", err)
	}
	events := cal.Events()
	if len(events) != 1 {
		return EventReference{}, fmt.Errorf("iCalendar contains %d VEVENTs; want exactly one", len(events))
	}
	if loc == nil {
		loc = time.UTC
	}

	ev := &events[0]
	uid, err := ev.Props.Text(ical.PropUID)
	if err != nil || strings.TrimSpace(uid) == "" {
		return EventReference{}, fmt.Errorf("VEVENT has no UID")
	}
	start, dur, allDay, err := eventTiming(ev, loc)
	if err != nil {
		return EventReference{}, fmt.Errorf("reading DTSTART: %w", err)
	}

	o := newOccurrence(uid, ev, start, start.Add(dur), allDay)
	if p := ev.Props.Get(ical.PropRecurrenceRule); p != nil {
		o.RRuleText = p.Value
	}
	if p := ev.Props.Get(ical.PropRecurrenceID); p != nil {
		rid, err := propTime(p, loc)
		if err != nil {
			return EventReference{}, fmt.Errorf("reading RECURRENCE-ID: %w", err)
		}
		o.RecurrenceID = RecurrenceKey(rid)
	}

	startProp := ev.Props.Get(ical.PropDateTimeStart)
	tz := startProp.Params.Get(ical.PropTimezoneID)
	if tz == "" && !allDay {
		if strings.HasSuffix(strings.ToUpper(startProp.Value), "Z") {
			tz = "UTC"
		} else {
			tz = loc.String()
		}
	}

	return EventReference{
		Occurrence: o,
		Timezone:   tz,
		Recurring:  o.RecurrenceID != "" || o.RRuleText != "" || ev.Props.Get(ical.PropRecurrenceDates) != nil,
	}, nil
}
