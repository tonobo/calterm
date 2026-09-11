package model

import (
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/emersion/go-ical"
	"github.com/teambition/rrule-go"
)

// errNoStart marks an event with no DTSTART, which cannot be placed on a
// calendar and is therefore skipped.
var errNoStart = errors.New("event has no DTSTART")

// Expand materialises every occurrence of every event in cal that overlaps
// win. loc is the fallback location for date-times that carry no TZID.
//
// An event that cannot be interpreted is skipped rather than failing the
// calendar: one bad object written by another client must not blank the view.
func Expand(cal *ical.Calendar, win Window, loc *time.Location) ([]Occurrence, error) {
	if loc == nil {
		loc = time.UTC
	}

	// Group components by UID so that a master and its RECURRENCE-ID overrides
	// are expanded together. Order of first appearance is preserved so the
	// output is deterministic before sorting.
	type group struct {
		master    *ical.Event
		overrides []*ical.Event
	}
	var order []string
	groups := map[string]*group{}

	for i := range cal.Events() {
		ev := cal.Events()[i]
		uid, err := ev.Props.Text(ical.PropUID)
		if err != nil || uid == "" {
			continue
		}
		g, ok := groups[uid]
		if !ok {
			g = &group{}
			groups[uid] = g
			order = append(order, uid)
		}
		if ev.Props.Get(ical.PropRecurrenceID) != nil {
			g.overrides = append(g.overrides, &ev)
		} else if g.master == nil {
			g.master = &ev
		}
	}

	var out []Occurrence
	for _, uid := range order {
		occs := expandGroup(uid, groups[uid].master, groups[uid].overrides, win, loc)
		out = append(out, occs...)
	}

	sort.SliceStable(out, func(i, j int) bool {
		if !out[i].Start.Equal(out[j].Start) {
			return out[i].Start.Before(out[j].Start)
		}
		if out[i].UID != out[j].UID {
			return out[i].UID < out[j].UID
		}
		return out[i].RecurrenceID < out[j].RecurrenceID
	})
	return out, nil
}

func expandGroup(uid string, master *ical.Event, overrides []*ical.Event, win Window, loc *time.Location) []Occurrence {
	// Orphan overrides (no master in this object) still describe real events.
	if master == nil {
		var out []Occurrence
		for _, ov := range overrides {
			start, dur, allDay, err := eventTiming(ov, loc)
			if err != nil {
				continue
			}
			o := newOccurrence(uid, ov, start, start.Add(dur), allDay)
			if ridProp := ov.Props.Get(ical.PropRecurrenceID); ridProp != nil {
				if rid, err := propTime(ridProp, loc); err == nil {
					o.RecurrenceID = RecurrenceKey(rid)
				}
			}
			if !strings.EqualFold(o.Status, "CANCELLED") && o.Overlaps(win.From, win.To) {
				out = append(out, o)
			}
		}
		return out
	}

	if status, err := master.Props.Text(ical.PropStatus); err == nil && strings.EqualFold(status, "CANCELLED") {
		return nil
	}

	start, dur, allDay, err := eventTiming(master, loc)
	if err != nil {
		return nil
	}

	rruleText := ""
	if p := master.Props.Get(ical.PropRecurrenceRule); p != nil {
		rruleText = p.Value
	}

	instants := generateInstants(master, start, dur, win, loc)

	// Key generated instances by recurrence instant so overrides can find them.
	byKey := make(map[string]Occurrence, len(instants))
	var keyOrder []string
	for _, inst := range instants {
		key := RecurrenceKey(inst)
		o := newOccurrence(uid, master, inst, inst.Add(dur), allDay)
		o.RRuleText = rruleText
		if rruleText != "" || len(instants) > 1 {
			o.RecurrenceID = key
		}
		byKey[key] = o
		keyOrder = append(keyOrder, key)
	}

	applyOverrides(uid, byKey, &keyOrder, overrides, rruleText, loc)

	out := make([]Occurrence, 0, len(byKey))
	for _, key := range keyOrder {
		o, ok := byKey[key]
		if !ok {
			continue // removed by a cancelling override
		}
		if o.Overlaps(win.From, win.To) {
			out = append(out, o)
		}
	}
	return out
}

// generateInstants returns the recurrence instants of the master event that
// could overlap win. The window start is widened by the event duration so that
// an event which began before the window but is still running is included.
func generateInstants(master *ical.Event, start time.Time, dur time.Duration, win Window, loc *time.Location) []time.Time {
	ropt, err := master.Props.RecurrenceRule()
	if err != nil {
		ropt = nil
	}
	rdates := multiDates(master.Props, ical.PropRecurrenceDates, loc)
	exdates := multiDates(master.Props, ical.PropExceptionDates, loc)

	if ropt == nil && len(rdates) == 0 {
		return []time.Time{start}
	}

	set := &rrule.Set{}
	set.DTStart(start)
	if ropt != nil {
		ropt.Dtstart = start
		if r, err := rrule.NewRRule(*ropt); err == nil {
			set.RRule(r)
		}
	}
	for _, t := range rdates {
		set.RDate(t)
	}
	// rrule.Set matches EXDATE by instant, so no normalisation is required.
	for _, t := range exdates {
		set.ExDate(t)
	}
	return set.Between(win.From.Add(-dur), win.To, true)
}

// applyOverrides replaces or removes generated instances according to the
// event's RECURRENCE-ID components. This runs after generation because an
// override may move an instance outside the series' own pattern.
func applyOverrides(uid string, byKey map[string]Occurrence, keyOrder *[]string, overrides []*ical.Event, rruleText string, loc *time.Location) {
	for _, ov := range overrides {
		ridProp := ov.Props.Get(ical.PropRecurrenceID)
		if ridProp == nil {
			continue
		}
		rid, err := propTime(ridProp, loc)
		if err != nil {
			continue
		}
		key := RecurrenceKey(rid)
		thisAndFuture := strings.EqualFold(ridProp.Params.Get(ical.ParamRange), "THISANDFUTURE")

		ovStart, ovDur, ovAllDay, err := eventTiming(ov, loc)
		if err != nil {
			continue
		}
		cancelled := false
		if status, err := ov.Props.Text(ical.PropStatus); err == nil && strings.EqualFold(status, "CANCELLED") {
			cancelled = true
		}

		if !thisAndFuture {
			delete(byKey, key)
			if cancelled {
				continue
			}
			o := newOccurrence(uid, ov, ovStart, ovStart.Add(ovDur), ovAllDay)
			o.RecurrenceID = key
			o.RRuleText = rruleText
			if _, seen := indexOf(*keyOrder, key); !seen {
				*keyOrder = append(*keyOrder, key)
			}
			byKey[key] = o
			continue
		}

		// THISANDFUTURE: the override applies to this instance and every later
		// one. Shift each affected instance by the same delta and adopt the
		// override's fields.
		//
		// Both the selection and the arithmetic go through the recurrence KEY
		// -- the instance's ORIGINAL time -- and never through its current
		// start. RFC 5545 defines RANGE=THISANDFUTURE against the recurrence
		// SET, not against instances as some earlier override may already have
		// moved them. Selecting by current start gets both directions wrong:
		// an instance an earlier override pushed past the boundary is wrongly
		// swept in (silently discarding that override), and one pulled back
		// before it wrongly escapes. It also made the result depend on the
		// order the server happened to serialise the overrides in, since they
		// are applied in document order over a mutating map.
		//
		// Document order still decides who WINS where two overrides cover the
		// same instance -- the later component supersedes the earlier -- which
		// is the intended reading; what must not depend on order is which
		// instances a range covers.
		shift := ovStart.Sub(rid)
		for _, k := range *keyOrder {
			if _, ok := byKey[k]; !ok {
				continue
			}
			original, err := time.Parse(time.RFC3339, k)
			if err != nil || original.Before(rid) {
				continue
			}
			if cancelled {
				delete(byKey, k)
				continue
			}
			shifted := original.Add(shift)
			o := newOccurrence(uid, ov, shifted, shifted.Add(ovDur), ovAllDay)
			o.RecurrenceID = k
			o.RRuleText = rruleText
			byKey[k] = o
		}
	}
}

func indexOf(ss []string, s string) (int, bool) {
	for i, v := range ss {
		if v == s {
			return i, true
		}
	}
	return 0, false
}

// newOccurrence copies the descriptive fields of ev onto a materialised
// instance running from start to end.
func newOccurrence(uid string, ev *ical.Event, start, end time.Time, allDay bool) Occurrence {
	text := func(name string) string {
		v, err := ev.Props.Text(name)
		if err != nil {
			return ""
		}
		return v
	}
	uri := func(name string) string {
		p := ev.Props.Get(name)
		if p == nil {
			return ""
		}
		u, err := p.URI()
		if err != nil {
			return ""
		}
		return u.String()
	}
	o := Occurrence{
		UID:         uid,
		Start:       start,
		End:         end,
		AllDay:      allDay,
		Summary:     text(ical.PropSummary),
		Location:    text(ical.PropLocation),
		Description: text(ical.PropDescription),
		URL:         uri(ical.PropURL),
		Status:      text(ical.PropStatus),
	}
	if p := ev.Props.Get(ical.PropOrganizer); p != nil {
		organizer := participantFromProp(*p, false)
		o.Organizer = &organizer
	}
	for _, p := range ev.Props.Values(ical.PropAttendee) {
		o.Attendees = append(o.Attendees, participantFromProp(p, true))
	}
	return o
}

func participantFromProp(p ical.Prop, attendee bool) Participant {
	email := p.Params.Get(ical.ParamEmail)
	if email == "" {
		email = EmailAddress(p.Value)
	}
	status := strings.ToUpper(p.Params.Get(ical.ParamParticipationStatus))
	if attendee && status == "" {
		status = "NEEDS-ACTION"
	}
	return Participant{
		Name:   p.Params.Get(ical.ParamCommonName),
		Email:  email,
		Status: status,
		Role:   strings.ToUpper(p.Params.Get(ical.ParamRole)),
		RSVP:   strings.EqualFold(p.Params.Get(ical.ParamRSVP), "TRUE"),
	}
}

// eventTiming resolves an event's start, duration, and all-day flag.
// All-day values are anchored to midnight UTC so that date arithmetic never
// crosses a DST boundary.
func eventTiming(ev *ical.Event, loc *time.Location) (start time.Time, dur time.Duration, allDay bool, err error) {
	startProp := ev.Props.Get(ical.PropDateTimeStart)
	if startProp == nil {
		return time.Time{}, 0, false, errNoStart
	}
	allDay = isDateValued(startProp)
	start, err = propTime(startProp, loc)
	if err != nil {
		return time.Time{}, 0, false, err
	}

	if endProp := ev.Props.Get(ical.PropDateTimeEnd); endProp != nil {
		end, err := propTime(endProp, loc)
		if err == nil && end.After(start) {
			return start, end.Sub(start), allDay, nil
		}
	}
	if durProp := ev.Props.Get(ical.PropDuration); durProp != nil {
		if d, err := durProp.Duration(); err == nil && d > 0 {
			return start, d, allDay, nil
		}
	}
	if allDay {
		return start, 24 * time.Hour, allDay, nil
	}
	return start, 0, allDay, nil
}

// propTime parses a date or date-time property, anchoring date values to
// midnight UTC.
func propTime(p *ical.Prop, loc *time.Location) (time.Time, error) {
	t, err := p.DateTime(loc)
	if err != nil {
		return time.Time{}, err
	}
	if isDateValued(p) {
		y, m, d := t.Date()
		return time.Date(y, m, d, 0, 0, 0, 0, time.UTC), nil
	}
	return t, nil
}

func isDateValued(p *ical.Prop) bool {
	if p.ValueType() == ical.ValueDate {
		return true
	}
	// A bare value of exactly YYYYMMDD is a date even without VALUE=DATE.
	return p.ValueType() == ical.ValueDefault && len(p.Value) == 8
}

// multiDates parses a possibly repeated, possibly comma-separated date list
// property such as EXDATE or RDATE.
func multiDates(props ical.Props, name string, loc *time.Location) []time.Time {
	var out []time.Time
	for _, p := range props.Values(name) {
		for _, v := range strings.Split(p.Value, ",") {
			v = strings.TrimSpace(v)
			if v == "" {
				continue
			}
			sub := ical.Prop{Name: p.Name, Params: p.Params, Value: v}
			t, err := propTime(&sub, loc)
			if err != nil {
				continue
			}
			out = append(out, t)
		}
	}
	return out
}
