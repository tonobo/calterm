package waybar

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/tonobo/calterm/internal/model"
)

// emptyMark marks a placeholder that expanded to nothing. It is substituted
// in place of an empty value so tidy() can tell "a separator left dangling
// by an empty placeholder" apart from "user content that happens to look
// like a separator" -- \x00 cannot appear in calendar text.
const emptyMark = "\x00"

// allDayRangeRe matches "{start}" and "{end}" (in either order) joined by
// any run of separator text that itself contains neither placeholder --
// i.e. it is genuinely the wording between the two, such as "\u2013",
// " - ", or the word " until ". Requiring the separator to be brace-free is
// what stops a template like "{start} · {location} \u2013 {end}" from being
// swallowed whole: {location} sitting between them means there is no bare
// separator to collapse, so the match must not span it.
var allDayRangeRe = regexp.MustCompile(`\{start\}[^{}]*\{end\}|\{end\}[^{}]*\{start\}`)

// View distinguishes which of waybar's two surfaces Format is rendering
// for. The bar (ViewBar) shows a single event with no surrounding context,
// so formatTime's day qualifier ("Sat", "Sat 30 Aug") is load-bearing
// there. The tooltip (ViewTooltip) groups events under its own date
// header, so the same qualifier on each line would just repeat that
// header -- formatTime renders the bare time or bare all-day label
// instead.
type View int

const (
	ViewBar View = iota
	ViewTooltip
)

// Format renders a template against one occurrence. Supported placeholders are
// {start}, {end}, {summary}, {location}, {calendar}, and {relative}.
//
// allDayLabel is substituted for {start}/{end} on a date-valued occurrence,
// threaded through as a parameter (rather than a package variable) because
// its wording is user-configurable.
func Format(tmpl string, o model.Occurrence, calendarName, allDayLabel string, now time.Time, loc *time.Location, view View) string {
	if loc == nil {
		loc = time.UTC
	}
	if o.AllDay {
		// {start} and {end} render the same allDayLabel for an all-day
		// occurrence (there is no meaningful distinct "end" to show next to
		// "start"), so a template joining them with any separator -- the
		// shipped default's "{start}–{end}", but just as plausibly a
		// user's own "{start} until {end}" or a plain hyphen
		// -- would otherwise duplicate the label (e.g.
		// "full day–full day", "full day until full day"). Collapsing the
		// pairing here, on the template text itself before substitution,
		// only touches that one specific run of text between the two
		// placeholders; it leaves every other separator in the template
		// (e.g. the " · " before {summary}) alone, unlike routing {end}
		// through the general empty-placeholder collapsing in tidy(),
		// which also eats separators that have nothing to do with the
		// range.
		tmpl = allDayRangeRe.ReplaceAllString(tmpl, "{start}")
	}
	r := strings.NewReplacer(
		"{summary}", markIfEmpty(o.Summary),
		"{location}", markIfEmpty(o.Location),
		"{calendar}", markIfEmpty(calendarName),
		"{start}", markIfEmpty(formatTime(o, o.Start, now, loc, allDayLabel, false, view)),
		"{end}", markIfEmpty(formatTime(o, o.End, now, loc, allDayLabel, true, view)),
		"{relative}", markIfEmpty(formatRelative(o.Start, now)),
	)
	return tidy(r.Replace(tmpl))
}

// markIfEmpty substitutes emptyMark for an empty placeholder value so tidy()
// can distinguish it from real content later.
func markIfEmpty(s string) string {
	if s == "" {
		return emptyMark
	}
	return s
}

// Tiers formatTime's day comparison can land in: today (bare value), within
// the next 7 days (weekday prefix), or 7 days or more away (weekday, day,
// and month -- a bare weekday for something three weeks out reads as *this*
// week and is misleading).
const (
	tierToday = iota
	tierWithinWeek
	tierFar
)

// formatTime renders an instant relative to today, in loc: bare time (or,
// for a date-valued occurrence, allDayLabel) for today; a weekday prefix
// ("Tue 09:00", "Sa full day") for anything else within the next 7 days;
// and a weekday+day+month prefix ("Sat 30 Aug 09:00", "Sat 30 Aug
// full day") from 7 days away onward. This applies equally to all-day and
// timed occurrences -- an all-day event is the one kind with no time of its
// own, which used to make it also the one kind that never said which day it
// was on.
//
// A future multi-day all-day occurrence is rendered as one compact inclusive
// span in the bar ("Mon–Fri all day"). Single-day and already-running
// all-day occurrences keep the ordinary bare/qualified label.
//
// isEnd drops the prefix entirely from the end of a range that falls on the
// same local day as its own start, even when that day isn't today: without
// this, "{start}–{end}" independently applied the "day other than today"
// rule to each side, repeating it ("Tue 09:00–Tue 11:00", or "Sat 30 Aug
// 09:00–Sat 30 Aug 11:00" in the far-future case, instead of "Tue
// 09:00–11:00" / "Sat 30 Aug 09:00–11:00"). A range that genuinely spans
// midnight still gets a prefix on both sides, since the end's local day
// then differs from the start's.
//
// view gates the day-qualifying prefix, but only for what the tooltip's own
// date header already covers -- the start's day. ViewTooltip renders {start}
// (and an {end} that falls on the same local day as {start}, already bare
// via the isEnd rule above) with no prefix, regardless of tier: the header
// says which day that is. An {end} that genuinely falls on a DIFFERENT
// local day than {start} -- a range crossing midnight -- is not covered by
// that header, so it keeps its normal tiered prefix even in ViewTooltip;
// suppressing it there would make "23:30-00:30" read as a same-day range
// with negative duration instead of a range crossing into the next day.
// ViewBar always keeps the tiered prefix described above -- it shows a
// single event with no other context.
func formatTime(o model.Occurrence, t, now time.Time, loc *time.Location, allDayLabel string, isEnd bool, view View) string {
	if o.AllDay {
		if !isEnd && view == ViewBar {
			if span := formatFutureAllDaySpan(o, now, loc, allDayLabel); span != "" {
				return span
			}
		}
		// An all-day Start/End is a date anchored to midnight UTC (see
		// model.Occurrence's AllDay doc) and must be compared as a date, not
		// converted through loc first: at a location behind UTC, converting
		// midnight UTC into loc rolls the clock back into the previous UTC
		// calendar day, which would misreport tomorrow's all-day event as
		// today's. o.Start.UTC() is used regardless of which instant (o.Start
		// or o.End) was passed in as t, since there is no meaningful distinct
		// "end" to show next to "start" for an all-day occurrence.
		t = o.Start.UTC()
	} else if isEnd {
		sy, sm, sd := o.Start.In(loc).Date()
		ey, em, ed := o.End.In(loc).Date()
		if sy == ey && sm == em && sd == ed {
			return t.In(loc).Format("15:04")
		}
	}

	local := t
	if !o.AllDay {
		local = t.In(loc)
	}
	tier := dayTier(o, t, now, loc)
	// Reaching here with isEnd and a timed (non-all-day) occurrence means
	// the same-day short-circuit above did NOT fire, i.e. the end falls on
	// a different local day than the start -- exactly the case the
	// tooltip's date header does not cover, so it must keep its tier-based
	// prefix even in ViewTooltip.
	endCrossesToADifferentDay := isEnd && !o.AllDay
	if view == ViewTooltip && !endCrossesToADifferentDay {
		tier = tierToday
	}
	switch tier {
	case tierToday:
		if o.AllDay {
			return allDayLabel
		}
		return local.Format("15:04")
	case tierWithinWeek:
		if o.AllDay {
			return local.Format("Mon") + " " + allDayLabel
		}
		return local.Format("Mon 15:04")
	default:
		if o.AllDay {
			return local.Format("Mon 02 Jan") + " " + allDayLabel
		}
		return local.Format("Mon 02 Jan 15:04")
	}
}

// formatFutureAllDaySpan returns an inclusive display range for an all-day
// event that has not started yet (or starts today). DTEND is exclusive, so a
// cached Mon 14 Sep through Sat 19 Sep occurrence is shown as Mon–Fri. Short,
// nearby spans use weekdays; longer or more distant spans include dates so a
// multi-week absence cannot masquerade as a few days in the current week.
func formatFutureAllDaySpan(o model.Occurrence, now time.Time, loc *time.Location, allDayLabel string) string {
	start := o.Start.UTC()
	end := o.End.UTC().AddDate(0, 0, -1)
	if !end.After(start) {
		return ""
	}
	ny, nm, nd := now.In(loc).Date()
	today := time.Date(ny, nm, nd, 0, 0, 0, 0, time.UTC)
	if start.Before(today) {
		return ""
	}

	layout := "Mon"
	if dayTier(o, start, now, loc) == tierFar || end.Sub(start) >= 7*24*time.Hour {
		layout = "Mon 02 Jan"
	}
	if start.Year() != end.Year() {
		layout = "Mon 02 Jan 2006"
	}
	return start.Format(layout) + "–" + end.Format(layout) + " " + allDayLabel
}

// dayTier classifies t's calendar day relative to now: today, within the
// next 7 days, or 7 days or more away. The comparison is distance-based
// (either direction), not forward-only, since formatTime is also asked to
// render a same-day range a couple of days in the past (the {start}/{end}
// of an already-finished occurrence still owes a plain weekday, not a
// fully-dated one).
//
// t's own day is read in UTC for an all-day occurrence and in loc for a
// timed one, mirroring buildTooltip's groupDate -- an all-day date must
// never be converted through loc (see formatTime). "Today" itself is always
// the caller's local today (now.In(loc)), for both kinds: a user's "today"
// is their own local day regardless of which zone an occurrence's date
// happens to be anchored in.
//
// An occurrence already in progress -- started before today but not yet
// ended -- has its day clamped to today first, mirroring buildTooltip's own
// groupDate: a three-week-old absence that is still running reads as
// "today", not as a date three weeks in the past.
func dayTier(o model.Occurrence, t, now time.Time, loc *time.Location) int {
	var ly int
	var lm time.Month
	var ld int
	if o.AllDay {
		ly, lm, ld = t.UTC().Date()
	} else {
		ly, lm, ld = t.In(loc).Date()
	}
	ny, nm, nd := now.In(loc).Date()
	day := time.Date(ly, lm, ld, 0, 0, 0, 0, time.UTC)
	today := time.Date(ny, nm, nd, 0, 0, 0, 0, time.UTC)
	if day.Before(today) && o.End.After(now) {
		day = today
	}
	if day.Equal(today) {
		return tierToday
	}
	diff := int(day.Sub(today).Hours() / 24)
	if diff < 0 {
		diff = -diff
	}
	if diff < 7 {
		return tierWithinWeek
	}
	return tierFar
}

// formatRelative renders the gap until start. Anything under a minute, or
// already begun, reads as "now".
func formatRelative(start, now time.Time) string {
	d := start.Sub(now)
	if d < time.Minute {
		return "now"
	}
	switch {
	case d >= 24*time.Hour:
		return fmt.Sprintf("in %dd", int(d.Hours())/24)
	case d >= time.Hour:
		h := int(d.Hours())
		m := int(d.Minutes()) % 60
		if m == 0 {
			return fmt.Sprintf("in %dh", h)
		}
		return fmt.Sprintf("in %dh%dm", h, m)
	default:
		return fmt.Sprintf("in %dm", int(d.Minutes()))
	}
}

// escapePango escapes the characters Pango markup treats specially. Waybar
// renders tooltips as Pango markup, so any calendar text reaching the
// tooltip must be escaped or an event titled e.g. "Tom & Jerry" or
// "<standup>" breaks rendering.
//
// & must be escaped first: escaping it after < and > would turn the "&lt;"
// and "&gt;" those produced back into "&amp;lt;" and "&amp;gt;".
//
// The bar's `text` field is NOT Pango markup and must never be passed
// through this function -- doing so would show a literal "&amp;" in the bar.
func escapePango(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	return s
}

// tidy removes the separators left behind when a placeholder expanded to
// nothing, so an event with no location does not render a dangling "·". Only
// separators adjacent to an emptyMark are removed; separators that are part
// of the user's own content are left untouched.
func tidy(s string) string {
	lines := strings.Split(s, "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		for _, sep := range []string{" · ", " — ", " - ", " | "} {
			for strings.Contains(line, sep+emptyMark) || strings.Contains(line, emptyMark+sep) {
				line = strings.ReplaceAll(line, sep+emptyMark, emptyMark)
				line = strings.ReplaceAll(line, emptyMark+sep, emptyMark)
			}
		}
		line = strings.ReplaceAll(line, emptyMark, "")
		line = strings.TrimSpace(line)
		if line != "" {
			out = append(out, line)
		}
	}
	return strings.Join(out, "\n")
}
