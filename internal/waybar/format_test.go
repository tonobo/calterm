package waybar

import (
	"testing"
	"time"

	"github.com/tonobo/calterm/internal/model"
)

func TestFormatPlaceholders(t *testing.T) {
	now := time.Date(2026, 6, 10, 13, 45, 0, 0, time.UTC)
	o := model.Occurrence{
		Summary:  "Standup",
		Location: "Room 2",
		Start:    time.Date(2026, 6, 10, 14, 0, 0, 0, time.UTC),
		End:      time.Date(2026, 6, 10, 14, 30, 0, 0, time.UTC),
	}
	tests := []struct{ tmpl, want string }{
		{"{summary}", "Standup"},
		{"{start} {summary}", "14:00 Standup"},
		{"{start}–{end}", "14:00–14:30"},
		{"{location}", "Room 2"},
		{"{calendar}", "Work"},
		{"{relative}", "in 15m"},
		{"{summary} ({relative})", "Standup (in 15m)"},
		{"no placeholders", "no placeholders"},
	}
	for _, tt := range tests {
		if got := Format(tt.tmpl, o, "Work", "all day", now, time.UTC, ViewBar); got != tt.want {
			t.Errorf("Format(%q) = %q, want %q", tt.tmpl, got, tt.want)
		}
	}
}

func TestFormatAllDayAndFutureDays(t *testing.T) {
	now := time.Date(2026, 6, 10, 13, 45, 0, 0, time.UTC)
	allDay := model.Occurrence{
		Summary: "Holiday",
		AllDay:  true,
		Start:   time.Date(2026, 6, 11, 0, 0, 0, 0, time.UTC),
		End:     time.Date(2026, 6, 12, 0, 0, 0, 0, time.UTC),
	}
	// allDay starts the day after now, so it now (correctly) gets a weekday
	// prefix, same as any other non-today occurrence -- this is the exact
	// defect fixed elsewhere in this file: an all-day event used to be the
	// one kind of occurrence that never said which day it was on.
	if got := Format("{start}", allDay, "Home", "all day", now, time.UTC, ViewBar); got != "Thu all day" {
		t.Errorf("all-day {start} = %q, want \"Thu all day\"", got)
	}

	// A timed event on a later day shows its weekday, so "14:00" is never
	// ambiguous between today and next Tuesday.
	future := model.Occurrence{
		Summary: "Review",
		Start:   time.Date(2026, 6, 12, 14, 0, 0, 0, time.UTC),
		End:     time.Date(2026, 6, 12, 15, 0, 0, 0, time.UTC),
	}
	if got := Format("{start}", future, "Work", "all day", now, time.UTC, ViewBar); got != "Fri 14:00" {
		t.Errorf("future {start} = %q, want \"Fri 14:00\"", got)
	}
}

func TestFormatRelative(t *testing.T) {
	base := time.Date(2026, 6, 10, 14, 0, 0, 0, time.UTC)
	tests := []struct {
		name string
		now  time.Time
		want string
	}{
		{"minutes", base.Add(-15 * time.Minute), "in 15m"},
		{"under a minute", base.Add(-30 * time.Second), "now"},
		{"hours and minutes", base.Add(-(2*time.Hour + 20*time.Minute)), "in 2h20m"},
		{"exact hours", base.Add(-3 * time.Hour), "in 3h"},
		{"days", base.Add(-50 * time.Hour), "in 2d"},
		{"already started", base.Add(10 * time.Minute), "now"},
	}
	o := model.Occurrence{Start: base, End: base.Add(time.Hour)}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Format("{relative}", o, "Work", "all day", tt.now, time.UTC, ViewBar); got != tt.want {
				t.Errorf("relative = %q, want %q", got, tt.want)
			}
		})
	}
}

// A missing location must not leave dangling separators in the output.
func TestFormatCollapsesEmptyLocation(t *testing.T) {
	now := time.Date(2026, 6, 10, 13, 45, 0, 0, time.UTC)
	o := model.Occurrence{
		Summary: "Standup",
		Start:   time.Date(2026, 6, 10, 14, 0, 0, 0, time.UTC),
		End:     time.Date(2026, 6, 10, 14, 30, 0, 0, time.UTC),
	}
	got := Format("{summary}\n{start}–{end} · {location}\n{calendar}", o, "Work", "all day", now, time.UTC, ViewBar)
	want := "Standup\n14:00–14:30\nWork"
	if got != want {
		t.Errorf("Format = %q, want %q", got, want)
	}
}

// Separator-like characters that are part of the user's own event content
// must survive Format unchanged; tidy() must only remove separators that a
// placeholder left dangling by expanding to nothing.
func TestFormatPreservesSeparatorsInUserContent(t *testing.T) {
	now := time.Date(2026, 6, 10, 13, 45, 0, 0, time.UTC)
	base := model.Occurrence{
		Start: time.Date(2026, 6, 10, 14, 0, 0, 0, time.UTC),
		End:   time.Date(2026, 6, 10, 14, 30, 0, 0, time.UTC),
	}
	summaries := []string{"- Kickoff", "· Retro", "Standup -", "| Sprint |", "Design | review"}
	for _, summary := range summaries {
		o := base
		o.Summary = summary
		if got := Format("{summary}", o, "Work", "all day", now, time.UTC, ViewBar); got != summary {
			t.Errorf("Format(%q) = %q, want unchanged %q", summary, got, summary)
		}
	}

	// A non-empty location must keep its separator: the fix must not
	// over-correct into stripping legitimate separators too.
	o := base
	o.Summary = "Standup"
	o.Location = "Room 2"
	got := Format("{start}–{end} · {location}", o, "Work", "all day", now, time.UTC, ViewBar)
	want := "14:00–14:30 · Room 2"
	if got != want {
		t.Errorf("Format with non-empty location = %q, want %q", got, want)
	}
}

// The "all day" text is configurable so users can set their
// own wording; it must not be a hardcoded literal or a package variable.
func TestFormatAllDayLabelIsConfigurable(t *testing.T) {
	now := time.Date(2026, 6, 10, 13, 45, 0, 0, time.UTC)
	o := model.Occurrence{
		Summary: "Vacation",
		AllDay:  true,
		Start:   time.Date(2026, 6, 11, 0, 0, 0, 0, time.UTC),
		End:     time.Date(2026, 6, 12, 0, 0, 0, 0, time.UTC),
	}
	// o starts the day after now, so the configurable label still comes
	// through, now with the weekday prefix that fixing the reported defect
	// adds for any non-today all-day occurrence.
	if got := Format("{start}", o, "Work", "full day", now, time.UTC, ViewBar); got != "Thu full day" {
		t.Errorf("Format with custom all-day label = %q, want \"Thu full day\"", got)
	}
}

func TestEscapePango(t *testing.T) {
	tests := []struct{ in, want string }{
		{"Tom & Jerry", "Tom &amp; Jerry"},
		{"<standup>", "&lt;standup&gt;"},
		{"a < b & c > d", "a &lt; b &amp; c &gt; d"},
		{"plain text", "plain text"},
		// & must be escaped first, or "&lt;" produced from "<" would itself
		// get its "&" re-escaped into "&amp;lt;".
		{"<a & b>", "&lt;a &amp; b&gt;"},
	}
	for _, tt := range tests {
		if got := escapePango(tt.in); got != tt.want {
			t.Errorf("escapePango(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// The brief's target output shows a single "full day" for an all-day
// event, not "full day–full day". formatTime has always returned the
// all-day label for both {start} and {end} (ignoring which instant was
// passed), and the shipped default tooltip_format renders
// "{start}–{end} · {location}" -- so every default install duplicated the
// label on every all-day event. {end} must collapse away for an all-day
// occurrence so the range reads as one value, without disturbing a genuine
// timed range (including the degenerate case where a timed event's start
// and end happen to coincide).
func TestFormatAllDayCollapsesStartEndRange(t *testing.T) {
	now := time.Date(2026, 6, 10, 13, 45, 0, 0, time.UTC)
	allDay := model.Occurrence{
		Summary: "Team vacation",
		AllDay:  true,
		Start:   time.Date(2026, 6, 11, 0, 0, 0, 0, time.UTC),
		End:     time.Date(2026, 6, 12, 0, 0, 0, 0, time.UTC),
	}
	// allDay starts the day after now, so the collapsed value now carries
	// the weekday prefix fixing the reported defect adds.
	if got := Format("{start}–{end}", allDay, "Work", "full day", now, time.UTC, ViewBar); got != "Thu full day" {
		t.Errorf("Format(%q) = %q, want %q", "{start}–{end}", got, "Thu full day")
	}

	// The default tooltip_format, exercised end to end: the dangling "–"
	// and the empty {location} separator must both collapse, leaving no
	// stray punctuation.
	want := "Team vacation\nThu full day\nWork"
	if got := Format("{summary}\n{start}–{end} · {location}\n{calendar}", allDay, "Work", "full day", now, time.UTC, ViewBar); got != want {
		t.Errorf("default tooltip_format for an all-day event = %q, want %q", got, want)
	}
}

// A timed event whose start and end happen to coincide is a real,
// independently-computed range -- it must keep showing both sides, not be
// swept up by the all-day collapsing rule.
func TestFormatTimedZeroDurationRangeIsUnaffected(t *testing.T) {
	now := time.Date(2026, 6, 10, 13, 45, 0, 0, time.UTC)
	instant := time.Date(2026, 6, 10, 14, 0, 0, 0, time.UTC)
	o := model.Occurrence{Summary: "Blocked slot", Start: instant, End: instant}
	got := Format("{start}–{end}", o, "Work", "all day", now, time.UTC, ViewBar)
	if got != "14:00–14:00" {
		t.Errorf("Format(%q) = %q, want \"14:00–14:00\" (a real, if degenerate, range)", "{start}–{end}", got)
	}
}

// The all-day range collapse must only touch the "{start}<separator>{end}"
// pairing itself, not any separator that follows -- an earlier
// implementation routed {end} through the generic empty-placeholder
// collapsing in tidy(), which also ate the " · " before {summary}, merging
// the range straight into the summary text with no separator at all.
func TestFormatAllDayCollapseDoesNotEatFollowingSeparator(t *testing.T) {
	now := time.Date(2026, 6, 10, 13, 45, 0, 0, time.UTC)
	allDay := model.Occurrence{
		Summary: "Team vacation",
		AllDay:  true,
		Start:   time.Date(2026, 6, 11, 0, 0, 0, 0, time.UTC),
		End:     time.Date(2026, 6, 12, 0, 0, 0, 0, time.UTC),
	}
	// allDay starts the day after now, so the collapsed value carries the
	// weekday prefix fixing the reported defect adds.
	got := Format("{start}–{end} · {summary} · {location}", allDay, "Work", "full day", now, time.UTC, ViewBar)
	want := "Thu full day · Team vacation"
	if got != want {
		t.Errorf("Format(%q) = %q, want %q", "{start}–{end} · {summary} · {location}", got, want)
	}
}

// The wording between {start} and {end} is whatever the user wrote --
// tooltip_format is configurable specifically so a user can
// set their own wording, and "{start} until {end}" or a plain hyphen are
// exactly the kind of thing they would write. The collapse must not be
// pinned to the one literal en-dash idiom the shipped default happens to
// use.
func TestFormatAllDayCollapsesAnySeparatorBetweenStartAndEnd(t *testing.T) {
	now := time.Date(2026, 6, 10, 13, 45, 0, 0, time.UTC)
	allDay := model.Occurrence{
		Summary: "Team vacation",
		AllDay:  true,
		Start:   time.Date(2026, 6, 11, 0, 0, 0, 0, time.UTC),
		End:     time.Date(2026, 6, 12, 0, 0, 0, 0, time.UTC),
	}
	timed := model.Occurrence{
		Summary: "Standup",
		Start:   time.Date(2026, 6, 10, 14, 0, 0, 0, time.UTC),
		End:     time.Date(2026, 6, 10, 14, 0, 0, 0, time.UTC), // coincide, like the reviewer's control
	}

	// allDay starts the day after now, so every collapsed/uncollapsed value
	// below carries the weekday prefix fixing the reported defect adds.
	tests := []struct {
		name string
		o    model.Occurrence
		tmpl string
		want string
	}{
		{"en dash, no spaces", allDay, "{start}–{end} · {summary}", "Thu full day · Team vacation"},
		{"spaced hyphen", allDay, "{start} - {end} · {summary}", "Thu full day · Team vacation"},
		{"word separator", allDay, "{start} until {end} · {summary}", "Thu full day · Team vacation"},
		{"plain hyphen, no spaces", allDay, "{start}-{end} · {summary}", "Thu full day · Team vacation"},
		{"mirrored order", allDay, "{end} until {start} · {summary}", "Thu full day · Team vacation"},
		{"non-all-day control: untouched even with identical start/end", timed, "{start} until {end} · {summary}", "14:00 until 14:00 · Standup"},
		// {location} sits between {start} and {end} here, so the separator
		// run contains a placeholder -- the collapse must not reach across
		// it. {location} is empty, so it still collapses via tidy()'s
		// ordinary empty-placeholder handling on its own " · " side; the
		// " – " on its other side isn't one of tidy()'s recognised
		// separators, so it survives, and both {start} and {end} remain.
		{"a placeholder between start and end must not be swallowed",
			allDay, "{start} · {location} – {end} · {summary}",
			"Thu full day – Thu full day · Team vacation"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Format(tt.tmpl, tt.o, "Work", "full day", now, time.UTC, ViewBar); got != tt.want {
				t.Errorf("Format(%q) = %q, want %q", tt.tmpl, got, tt.want)
			}
		})
	}
}

// formatTime adds a weekday prefix for any day other than today, applied to
// {start} and {end} independently -- so a range entirely within one
// non-today day repeated the weekday on both ends ("Tue 09:00–Tue 11:00"
// instead of "Tue 09:00–11:00"). Found by running the real binary against
// a real cache.
func TestFormatSameDayRangeDropsRedundantWeekdayPrefixOnEnd(t *testing.T) {
	now := time.Date(2026, 8, 27, 9, 0, 0, 0, time.UTC) // Thursday
	o := model.Occurrence{
		Summary: "Team coffee (Optional)",
		Start:   time.Date(2026, 8, 25, 9, 0, 0, 0, time.UTC),  // Tuesday
		End:     time.Date(2026, 8, 25, 11, 0, 0, 0, time.UTC), // same Tuesday
	}
	got := Format("{start}–{end}", o, "Work", "all day", now, time.UTC, ViewBar)
	want := "Tue 09:00–11:00"
	if got != want {
		t.Errorf("Format(%q) = %q, want %q", "{start}–{end}", got, want)
	}
}

// A range that genuinely spans midnight must keep the weekday prefix on
// both ends -- dropping it from the end here would make "Tue 23:30–00:30"
// look like a 23-hour meeting instead of a 1-hour one crossing into
// Wednesday.
func TestFormatMidnightSpanningRangeKeepsBothWeekdayPrefixes(t *testing.T) {
	now := time.Date(2026, 8, 27, 9, 0, 0, 0, time.UTC) // Thursday
	o := model.Occurrence{
		Summary: "Overnight",
		Start:   time.Date(2026, 8, 25, 23, 30, 0, 0, time.UTC), // Tuesday 23:30
		End:     time.Date(2026, 8, 26, 0, 30, 0, 0, time.UTC),  // Wednesday 00:30
	}
	got := Format("{start}–{end}", o, "Work", "all day", now, time.UTC, ViewBar)
	want := "Tue 23:30–Wed 00:30"
	if got != want {
		t.Errorf("Format(%q) = %q, want %q", "{start}–{end}", got, want)
	}
}

// The previous two tests both pass loc = time.UTC, which cannot tell "same
// day decided in loc" apart from "same day decided in UTC" -- loc and UTC
// are the same zone there. jst, ahead of UTC, can produce a UTC-day
// boundary crossing that is NOT a local-day boundary crossing, and vice
// versa, so these two are the cases that actually exercise "in loc".
var jst = time.FixedZone("JST", 9*60*60)

// Same local (JST) day, but different UTC calendar days (23:00 UTC on the
// 25th is already 08:00 JST on the 26th) -- the end's weekday prefix must
// still drop, because "same day" is decided in loc, not UTC.
func TestFormatSameDayRangeDropsRedundantWeekdayPrefixAtNonUTCLocation(t *testing.T) {
	now := time.Date(2026, 8, 20, 9, 0, 0, 0, time.UTC) // well before this range
	o := model.Occurrence{
		Summary: "Team coffee (Optional)",
		Start:   time.Date(2026, 8, 25, 23, 0, 0, 0, time.UTC), // Wed 08:00 JST
		End:     time.Date(2026, 8, 26, 1, 0, 0, 0, time.UTC),  // Wed 10:00 JST, same JST day
	}
	got := Format("{start}–{end}", o, "Work", "all day", now, jst, ViewBar)
	want := "Wed 08:00–10:00"
	if got != want {
		t.Errorf("Format(%q) = %q, want %q", "{start}–{end}", got, want)
	}
}

// Same UTC calendar day (both instants fall on the 25th UTC), but different
// local (JST) days -- the range crosses local midnight, so both ends must
// keep their weekday prefix. A UTC-based (buggy) same-day check would
// wrongly see one UTC day and drop the end's prefix; this only fails
// against that bug because loc != UTC here.
func TestFormatLocalMidnightSpanningRangeKeepsBothWeekdayPrefixesAtNonUTCLocation(t *testing.T) {
	now := time.Date(2026, 8, 20, 9, 0, 0, 0, time.UTC)
	o := model.Occurrence{
		Summary: "Overnight",
		Start:   time.Date(2026, 8, 25, 14, 0, 0, 0, time.UTC), // Tue 23:00 JST
		End:     time.Date(2026, 8, 25, 16, 0, 0, 0, time.UTC), // Wed 01:00 JST
	}
	got := Format("{start}–{end}", o, "Work", "all day", now, jst, ViewBar)
	want := "Tue 23:00–Wed 01:00"
	if got != want {
		t.Errorf("Format(%q) = %q, want %q", "{start}–{end}", got, want)
	}
}

// --- ViewTooltip must suppress only what the tooltip's own date header
// already carries (the start's day), not a genuine midnight crossing on
// the end -- reported after the initial View fix suppressed
// unconditionally. Both directions, both views, at a fixed non-UTC
// location, since that combination is exactly what let the bug through:
// the existing same-day/midnight pair only exercised ViewBar.

// Same local day, non-UTC location: both views must render the range bare
// -- the tooltip because its header already says the day, the bar because
// isEnd's same-day rule already collapses it regardless of view.
func TestFormatViewTooltipSameLocalDayRangeStaysBareAtNonUTCLocation(t *testing.T) {
	now := time.Date(2026, 8, 20, 9, 0, 0, 0, time.UTC) // well before this range
	o := model.Occurrence{
		Summary: "Team coffee (Optional)",
		Start:   time.Date(2026, 8, 25, 23, 0, 0, 0, time.UTC), // Wed 08:00 JST
		End:     time.Date(2026, 8, 26, 1, 0, 0, 0, time.UTC),  // Wed 10:00 JST, same JST day
	}
	if got := Format("{start}–{end}", o, "Work", "all day", now, jst, ViewBar); got != "Wed 08:00–10:00" {
		t.Errorf("ViewBar {start}-{end} = %q, want %q", got, "Wed 08:00–10:00")
	}
	if got := Format("{start}–{end}", o, "Work", "all day", now, jst, ViewTooltip); got != "08:00–10:00" {
		t.Errorf("ViewTooltip {start}-{end} = %q, want %q", got, "08:00–10:00")
	}
}

// A range genuinely crossing local midnight, non-UTC location: the bar
// keeps its full weekday prefix on both ends, unchanged. The tooltip drops
// the prefix only from the start (its day is the header's day) and keeps
// it on the end, whose day the header does NOT cover -- dropping it there
// would read as a same-day range with negative duration ("23:00-01:00").
func TestFormatViewTooltipMidnightSpanningRangeKeepsEndsDayQualifierAtNonUTCLocation(t *testing.T) {
	now := time.Date(2026, 8, 20, 9, 0, 0, 0, time.UTC)
	o := model.Occurrence{
		Summary: "Overnight",
		Start:   time.Date(2026, 8, 25, 14, 0, 0, 0, time.UTC), // Tue 23:00 JST
		End:     time.Date(2026, 8, 25, 16, 0, 0, 0, time.UTC), // Wed 01:00 JST
	}
	if got := Format("{start}–{end}", o, "Work", "all day", now, jst, ViewBar); got != "Tue 23:00–Wed 01:00" {
		t.Errorf("ViewBar {start}-{end} = %q, want %q", got, "Tue 23:00–Wed 01:00")
	}
	if got := Format("{start}–{end}", o, "Work", "all day", now, jst, ViewTooltip); got != "23:00–Wed 01:00" {
		t.Errorf("ViewTooltip {start}-{end} = %q, want %q", got, "23:00–Wed 01:00")
	}
}

// A range on TODAY already has no weekday prefix on either side -- this is
// the pre-existing, unaffected case, pinned so a future change can't
// silently add a prefix here while fixing the same-day-but-not-today case.
func TestFormatTodayRangeHasNoWeekdayPrefixOnEitherSide(t *testing.T) {
	now := time.Date(2026, 8, 27, 9, 0, 0, 0, time.UTC)
	o := model.Occurrence{
		Summary: "Standup",
		Start:   time.Date(2026, 8, 27, 14, 0, 0, 0, time.UTC),
		End:     time.Date(2026, 8, 27, 14, 30, 0, 0, time.UTC),
	}
	got := Format("{start}–{end}", o, "Work", "all day", now, time.UTC, ViewBar)
	want := "14:00–14:30"
	if got != want {
		t.Errorf("Format(%q) = %q, want %q", "{start}–{end}", got, want)
	}
}

// --- "when is it" for all-day occurrences (the reported defect) -------
//
// All of these run at jst (+9), a non-UTC location: tests written at
// time.UTC cannot tell "the day computed in loc" apart from "the day
// computed in UTC" and have hidden three separate defects in this module
// for exactly that reason. now is chosen so its UTC and JST calendar dates
// agree (2026-08-27, a Thursday), so the day arithmetic below is easy to
// follow by hand while still exercising real loc conversion for every
// occurrence (each Start is given in UTC and crosses into a different JST
// clock time).

// now = 2026-08-27T02:00:00Z = 11:00 JST, Thursday, both zones.
var whenNow = time.Date(2026, 8, 27, 2, 0, 0, 0, time.UTC)

// negLoc is behind UTC, like waybar_test.go's negOffsetLoc. model.Occurrence
// documents that an all-day Start/End is anchored to midnight UTC and must
// be compared as a date, never converted through a local zone -- converting
// it into a location behind UTC pulls the clock back into the previous UTC
// calendar day, which is exactly the kind of defect a location ahead of UTC
// (jst, testLoc/CEST) cannot expose: UTC midnight converted into a zone
// ahead of UTC never crosses back into the previous day.
var negLoc = time.FixedZone("negative-nine", -9*60*60)

// now = 2026-08-27T12:00:00Z = 03:00 negLoc, both Aug 27 (today in either
// zone) -- isolates the bug to the all-day date itself, not to "now" also
// disagreeing between zones.
var negWhenNow = time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)

// An all-day event is a date, anchored in UTC -- converting its UTC-midnight
// Start into negLoc rolls the clock back to 15:00 on the *previous* UTC day,
// so a loc-converted day comparison wrongly reads tomorrow's all-day event
// as if it were today's.
func TestFormatAllDayTomorrowAtNegativeUTCOffset(t *testing.T) {
	o := model.Occurrence{
		Summary: "All day tomorrow", AllDay: true,
		Start: time.Date(2026, 8, 28, 0, 0, 0, 0, time.UTC), // Aug 28 UTC date; 15:00 Aug 27 in negLoc
		End:   time.Date(2026, 8, 29, 0, 0, 0, 0, time.UTC),
	}
	if got := Format("{start}", o, "Work", "full day", negWhenNow, negLoc, ViewBar); got != "Fri full day" {
		t.Errorf("all-day tomorrow at negative UTC offset {start} = %q, want %q", got, "Fri full day")
	}
}

// The far-tier case at the same negative offset: an all-day event's UTC
// date, not a loc-shifted one, must appear in the weekday+day+month prefix.
func TestFormatAllDayFarFutureAtNegativeUTCOffset(t *testing.T) {
	o := model.Occurrence{
		Summary: "All day in 8 days", AllDay: true,
		Start: time.Date(2026, 9, 4, 0, 0, 0, 0, time.UTC), // 8 days out, UTC date
		End:   time.Date(2026, 9, 5, 0, 0, 0, 0, time.UTC),
	}
	if got := Format("{start}", o, "Work", "full day", negWhenNow, negLoc, ViewBar); got != "Fri 04 Sep full day" {
		t.Errorf("all-day far future at negative UTC offset {start} = %q, want %q", got, "Fri 04 Sep full day")
	}
}

func TestFormatAllDayToday(t *testing.T) {
	o := model.Occurrence{
		Summary: "All day today", AllDay: true,
		Start: time.Date(2026, 8, 27, 0, 0, 0, 0, time.UTC), // 09:00 JST Thu (today)
		End:   time.Date(2026, 8, 28, 0, 0, 0, 0, time.UTC),
	}
	if got := Format("{start}", o, "Work", "full day", whenNow, jst, ViewBar); got != "full day" {
		t.Errorf("all-day today {start} = %q, want %q", got, "full day")
	}
}

func TestFormatAllDayTomorrow(t *testing.T) {
	o := model.Occurrence{
		Summary: "All day tomorrow", AllDay: true,
		Start: time.Date(2026, 8, 28, 0, 0, 0, 0, time.UTC), // 09:00 JST Fri
		End:   time.Date(2026, 8, 29, 0, 0, 0, 0, time.UTC),
	}
	if got := Format("{start}", o, "Work", "full day", whenNow, jst, ViewBar); got != "Fri full day" {
		t.Errorf("all-day tomorrow {start} = %q, want %q", got, "Fri full day")
	}
}

func TestFormatAllDayIn3Days(t *testing.T) {
	o := model.Occurrence{
		Summary: "All day in 3 days", AllDay: true,
		Start: time.Date(2026, 8, 30, 0, 0, 0, 0, time.UTC), // 09:00 JST Sun
		End:   time.Date(2026, 8, 31, 0, 0, 0, 0, time.UTC),
	}
	if got := Format("{start}", o, "Work", "full day", whenNow, jst, ViewBar); got != "Sun full day" {
		t.Errorf("all-day in 3 days {start} = %q, want %q", got, "Sun full day")
	}
}

func TestFormatAllDayIn3Weeks(t *testing.T) {
	o := model.Occurrence{
		Summary: "Summer horse party", AllDay: true,
		Start: time.Date(2026, 9, 17, 0, 0, 0, 0, time.UTC), // 09:00 JST Thu, 21 days out
		End:   time.Date(2026, 9, 18, 0, 0, 0, 0, time.UTC),
	}
	if got := Format("{start}", o, "Work", "full day", whenNow, jst, ViewBar); got != "Thu 17 Sep full day" {
		t.Errorf("all-day in 3 weeks {start} = %q, want %q", got, "Thu 17 Sep full day")
	}
}

func TestFormatTimedToday(t *testing.T) {
	o := model.Occurrence{
		Summary: "Standup",
		Start:   time.Date(2026, 8, 27, 5, 0, 0, 0, time.UTC), // 14:00 JST Thu (today)
		End:     time.Date(2026, 8, 27, 6, 0, 0, 0, time.UTC),
	}
	if got := Format("{start}", o, "Work", "full day", whenNow, jst, ViewBar); got != "14:00" {
		t.Errorf("timed today {start} = %q, want %q", got, "14:00")
	}
}

func TestFormatTimedTomorrow(t *testing.T) {
	o := model.Occurrence{
		Summary: "Standup",
		Start:   time.Date(2026, 8, 28, 5, 0, 0, 0, time.UTC), // 14:00 JST Fri
		End:     time.Date(2026, 8, 28, 6, 0, 0, 0, time.UTC),
	}
	if got := Format("{start}", o, "Work", "full day", whenNow, jst, ViewBar); got != "Fri 14:00" {
		t.Errorf("timed tomorrow {start} = %q, want %q", got, "Fri 14:00")
	}
}

func TestFormatTimedIn3Days(t *testing.T) {
	o := model.Occurrence{
		Summary: "Standup",
		Start:   time.Date(2026, 8, 30, 5, 0, 0, 0, time.UTC), // 14:00 JST Sun
		End:     time.Date(2026, 8, 30, 6, 0, 0, 0, time.UTC),
	}
	if got := Format("{start}", o, "Work", "full day", whenNow, jst, ViewBar); got != "Sun 14:00" {
		t.Errorf("timed in 3 days {start} = %q, want %q", got, "Sun 14:00")
	}
}

func TestFormatTimedIn3Weeks(t *testing.T) {
	o := model.Occurrence{
		Summary: "Standup",
		Start:   time.Date(2026, 9, 17, 5, 0, 0, 0, time.UTC), // 14:00 JST Thu, 21 days out
		End:     time.Date(2026, 9, 17, 6, 0, 0, 0, time.UTC),
	}
	if got := Format("{start}", o, "Work", "full day", whenNow, jst, ViewBar); got != "Thu 17 Sep 14:00" {
		t.Errorf("timed in 3 weeks {start} = %q, want %q", got, "Thu 17 Sep 14:00")
	}
}

// The shape the user's own calendar actually has, and which has already
// caught two separate defects in this module: a multi-week all-day event
// that started weeks ago and is still running. Its Start is 21 days in the
// past -- far past the 7-day threshold -- but since it hasn't ended yet,
// "when is it" must read as today (mirroring buildTooltip's own
// groupDate clamp), not as a date three weeks stale.
func TestFormatAllDayInProgressMultiWeekEventReadsAsToday(t *testing.T) {
	o := model.Occurrence{
		Summary: "Extended vacation", AllDay: true,
		Start: time.Date(2026, 8, 6, 0, 0, 0, 0, time.UTC),  // 21 days ago
		End:   time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC), // still running
	}
	if got := Format("{start}", o, "Work", "full day", whenNow, jst, ViewBar); got != "full day" {
		t.Errorf("in-progress multi-week all-day {start} = %q, want %q", got, "full day")
	}
}

func TestFormatFutureMultiDayAllDayEventShowsInclusiveSpan(t *testing.T) {
	now := time.Date(2026, 9, 11, 9, 0, 0, 0, time.FixedZone("Europe/Berlin", 2*60*60))
	o := model.Occurrence{
		Summary: "Project vacation", AllDay: true,
		Start: time.Date(2026, 9, 14, 0, 0, 0, 0, time.UTC),
		End:   time.Date(2026, 9, 19, 0, 0, 0, 0, time.UTC), // exclusive Saturday
	}
	for _, tmpl := range []string{"{start}", "{start}–{end}"} {
		if got := Format(tmpl, o, "Work", "full day", now, now.Location(), ViewBar); got != "Mon–Fri full day" {
			t.Errorf("Format(%q) = %q, want inclusive multi-day span", tmpl, got)
		}
	}
}

func TestFormatDistantMultiDayAllDayEventIncludesDates(t *testing.T) {
	now := time.Date(2026, 9, 11, 9, 0, 0, 0, time.UTC)
	o := model.Occurrence{
		AllDay: true,
		Start:  time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC),
		End:    time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC),
	}
	if got := Format("{start}", o, "Work", "all day", now, time.UTC, ViewBar); got != "Mon 05 Oct–Fri 09 Oct all day" {
		t.Errorf("distant all-day span = %q", got)
	}
}

// The all-day range collapse rewrites "{start}<sep>{end}" down to a single
// {start} on the template text itself, before either placeholder is
// substituted -- proving it still works once {start} carries a date (not
// just the bare allDayLabel) is the point of this test: a regression here
// would render "Fri full day–Fri full day" instead of one value.
func TestFormatAllDayCollapseStillWorksWhenStartCarriesADate(t *testing.T) {
	o := model.Occurrence{
		Summary: "Summer horse party", AllDay: true,
		Start: time.Date(2026, 8, 28, 0, 0, 0, 0, time.UTC), // tomorrow, JST
		End:   time.Date(2026, 8, 29, 0, 0, 0, 0, time.UTC),
	}
	got := Format("{start}–{end} · {summary}", o, "Work", "full day", whenNow, jst, ViewBar)
	want := "Fri full day · Summer horse party"
	if got != want {
		t.Errorf("Format(%q) = %q, want %q", "{start}–{end} · {summary}", got, want)
	}
}

// isEnd's same-day rule must still apply once the far-future (weekday, day,
// month) case exists: a same-day range far in the future must render the
// long form once, not twice.
func TestFormatIsEndDropsLongFormOnFarFutureSameDayRange(t *testing.T) {
	o := model.Occurrence{
		Summary: "Team coffee (Optional)",
		Start:   time.Date(2026, 9, 17, 0, 0, 0, 0, time.UTC), // 09:00 JST Thu, 21 days out
		End:     time.Date(2026, 9, 17, 2, 0, 0, 0, time.UTC), // 11:00 JST, same JST day
	}
	got := Format("{start}–{end}", o, "Work", "full day", whenNow, jst, ViewBar)
	want := "Thu 17 Sep 09:00–11:00"
	if got != want {
		t.Errorf("Format(%q) = %q, want %q", "{start}–{end}", got, want)
	}
}

// The 7-day boundary itself, both sides of it, for both kinds of
// occurrence: 6 days out still gets the weekday-only prefix, 7 days out
// switches to weekday+day+month. Off-by-ones live at boundaries, and the
// brief states the threshold explicitly ("7 days or more away").
func TestFormatSevenDayBoundary(t *testing.T) {
	sixDaysAllDay := model.Occurrence{
		Summary: "6 days out, all day", AllDay: true,
		Start: time.Date(2026, 9, 2, 0, 0, 0, 0, time.UTC), // 09:00 JST Wed
		End:   time.Date(2026, 9, 3, 0, 0, 0, 0, time.UTC),
	}
	if got := Format("{start}", sixDaysAllDay, "Work", "full day", whenNow, jst, ViewBar); got != "Wed full day" {
		t.Errorf("all-day 6 days out {start} = %q, want %q", got, "Wed full day")
	}

	sevenDaysAllDay := model.Occurrence{
		Summary: "7 days out, all day", AllDay: true,
		Start: time.Date(2026, 9, 3, 0, 0, 0, 0, time.UTC), // 09:00 JST Thu
		End:   time.Date(2026, 9, 4, 0, 0, 0, 0, time.UTC),
	}
	if got := Format("{start}", sevenDaysAllDay, "Work", "full day", whenNow, jst, ViewBar); got != "Thu 03 Sep full day" {
		t.Errorf("all-day 7 days out {start} = %q, want %q", got, "Thu 03 Sep full day")
	}

	sixDaysTimed := model.Occurrence{
		Summary: "6 days out, timed",
		Start:   time.Date(2026, 9, 2, 5, 0, 0, 0, time.UTC), // 14:00 JST Wed
		End:     time.Date(2026, 9, 2, 6, 0, 0, 0, time.UTC),
	}
	if got := Format("{start}", sixDaysTimed, "Work", "full day", whenNow, jst, ViewBar); got != "Wed 14:00" {
		t.Errorf("timed 6 days out {start} = %q, want %q", got, "Wed 14:00")
	}

	sevenDaysTimed := model.Occurrence{
		Summary: "7 days out, timed",
		Start:   time.Date(2026, 9, 3, 5, 0, 0, 0, time.UTC), // 14:00 JST Thu
		End:     time.Date(2026, 9, 3, 6, 0, 0, 0, time.UTC),
	}
	if got := Format("{start}", sevenDaysTimed, "Work", "full day", whenNow, jst, ViewBar); got != "Thu 03 Sep 14:00" {
		t.Errorf("timed 7 days out {start} = %q, want %q", got, "Thu 03 Sep 14:00")
	}
}

// A timed (not all-day) multi-day event already in progress -- started on a
// previous calendar day, still running -- must also read as today, not as
// a stale weekday: this is a deliberate behaviour change (previously it
// would have shown "Tue 07:00"), made for the same reason as the all-day
// clamp -- "when is it" for something already underway is "now", not the
// day it began.
func TestFormatTimedInProgressMultiDayEventReadsAsToday(t *testing.T) {
	o := model.Occurrence{
		Summary: "Conference",
		Start:   time.Date(2026, 8, 25, 22, 0, 0, 0, time.UTC), // 07:00 JST Wed (2 days before now)
		End:     time.Date(2026, 8, 29, 10, 0, 0, 0, time.UTC), // still running past whenNow
	}
	if got := Format("{start}", o, "Work", "full day", whenNow, jst, ViewBar); got != "07:00" {
		t.Errorf("timed in-progress multi-day {start} = %q, want %q", got, "07:00")
	}
}

// --- View: ViewTooltip drops the day qualifier, ViewBar keeps it --------
//
// This is the fix that was in flight when the previous session was
// interrupted (see the brief): the tooltip prints its own date header per
// group, so repeating the day on every line is noise there, but the bar
// shows a single event with no other context and still needs it.

// A weekday-tier all-day occurrence (tomorrow): ViewBar renders the
// "Thu full day" prefix+label pair; ViewTooltip renders the bare label
// only.
func TestFormatViewTooltipDropsWeekdayPrefixOnAllDayOccurrence(t *testing.T) {
	o := model.Occurrence{
		Summary: "All day tomorrow", AllDay: true,
		Start: time.Date(2026, 8, 28, 0, 0, 0, 0, time.UTC), // tomorrow, JST
		End:   time.Date(2026, 8, 29, 0, 0, 0, 0, time.UTC),
	}
	if got := Format("{start}", o, "Work", "full day", whenNow, jst, ViewBar); got != "Fri full day" {
		t.Errorf("ViewBar {start} = %q, want %q", got, "Fri full day")
	}
	if got := Format("{start}", o, "Work", "full day", whenNow, jst, ViewTooltip); got != "full day" {
		t.Errorf("ViewTooltip {start} = %q, want %q", got, "full day")
	}
}

// A far-tier (7+ days out) timed occurrence: ViewBar renders the full
// weekday+day+month+time prefix; ViewTooltip renders the bare time, exactly
// as it would for something happening today.
func TestFormatViewTooltipDropsFarFuturePrefixOnTimedOccurrence(t *testing.T) {
	o := model.Occurrence{
		Summary: "Standup",
		Start:   time.Date(2026, 9, 17, 5, 0, 0, 0, time.UTC), // 14:00 JST Thu, 21 days out
		End:     time.Date(2026, 9, 17, 6, 0, 0, 0, time.UTC),
	}
	if got := Format("{start}", o, "Work", "full day", whenNow, jst, ViewBar); got != "Thu 17 Sep 14:00" {
		t.Errorf("ViewBar {start} = %q, want %q", got, "Thu 17 Sep 14:00")
	}
	if got := Format("{start}", o, "Work", "full day", whenNow, jst, ViewTooltip); got != "14:00" {
		t.Errorf("ViewTooltip {start} = %q, want %q", got, "14:00")
	}
}

// The pre-existing same-day-range case, now the older instance of the same
// redundancy per the brief: "Tue 09:00–11:00" under a "Tue 01.09." tooltip
// header still repeats "Tue" needlessly today; ViewTooltip must render the
// bare range, while ViewBar keeps the weekday prefix on the start.
func TestFormatViewTooltipDropsWeekdayPrefixOnSameDayTimedRange(t *testing.T) {
	now := time.Date(2026, 8, 27, 9, 0, 0, 0, time.UTC) // Thursday
	o := model.Occurrence{
		Summary: "Team coffee (Optional)",
		Start:   time.Date(2026, 8, 25, 9, 0, 0, 0, time.UTC),  // Tuesday
		End:     time.Date(2026, 8, 25, 11, 0, 0, 0, time.UTC), // same Tuesday
	}
	if got := Format("{start}–{end}", o, "Work", "all day", now, time.UTC, ViewBar); got != "Tue 09:00–11:00" {
		t.Errorf("ViewBar {start}-{end} = %q, want %q", got, "Tue 09:00–11:00")
	}
	if got := Format("{start}–{end}", o, "Work", "all day", now, time.UTC, ViewTooltip); got != "09:00–11:00" {
		t.Errorf("ViewTooltip {start}-{end} = %q, want %q", got, "09:00–11:00")
	}
}
