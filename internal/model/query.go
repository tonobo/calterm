package model

import "time"

// Between returns the occurrences overlapping [from, to), preserving order.
func Between(occs []Occurrence, from, to time.Time) []Occurrence {
	var out []Occurrence
	for _, o := range occs {
		if o.Overlaps(from, to) {
			out = append(out, o)
		}
	}
	return out
}

// OnDay returns the occurrences falling on the calendar day containing day, as
// seen from loc. All-day occurrences are matched by date, since they are dates
// rather than instants and must not shift with the viewer's timezone.
func OnDay(occs []Occurrence, day time.Time, loc *time.Location) []Occurrence {
	if loc == nil {
		loc = time.UTC
	}
	y, m, d := day.In(loc).Date()
	dayStart := time.Date(y, m, d, 0, 0, 0, 0, loc)
	dayEnd := dayStart.AddDate(0, 0, 1)

	var out []Occurrence
	for _, o := range occs {
		if o.AllDay {
			// Compare calendar dates in UTC, where all-day values are anchored.
			oy, om, od := o.Start.UTC().Date()
			start := time.Date(oy, om, od, 0, 0, 0, 0, time.UTC)
			ey, em, ed := o.End.UTC().Date()
			end := time.Date(ey, em, ed, 0, 0, 0, 0, time.UTC)
			target := time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
			if !target.Before(start) && target.Before(end) {
				out = append(out, o)
			}
			continue
		}
		if o.Overlaps(dayStart, dayEnd) {
			out = append(out, o)
		}
	}
	return out
}

// NextAfter returns the first occurrence starting at or after now.
// occs must be sorted by start, which Expand guarantees.
func NextAfter(occs []Occurrence, now time.Time) (Occurrence, bool) {
	for _, o := range occs {
		if !o.Start.Before(now) {
			return o, true
		}
	}
	return Occurrence{}, false
}

// InProgress returns the timed occurrence currently running, if any. All-day
// occurrences are excluded: reporting one as in progress would mask every timed
// event for the whole day.
func InProgress(occs []Occurrence, now time.Time) (Occurrence, bool) {
	for _, o := range occs {
		if o.AllDay || o.End.Equal(o.Start) {
			continue
		}
		if !now.Before(o.Start) && now.Before(o.End) {
			return o, true
		}
	}
	return Occurrence{}, false
}
