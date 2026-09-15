package waybar

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"charm.land/lipgloss/v2"

	"github.com/tonobo/calterm/internal/config"
	"github.com/tonobo/calterm/internal/model"
	"github.com/tonobo/calterm/internal/store"
)

func testConfig() config.WaybarConfig {
	return config.WaybarConfig{
		LeadTime:       15 * time.Minute,
		NowDuration:    5 * time.Minute,
		StaleAfter:     2 * time.Hour,
		TextFormat:     "{start} {summary}",
		TooltipFormat:  "{summary}\n{start}–{end} · {location}\n{calendar}",
		TooltipDays:    7,
		TooltipMax:     10,
		TooltipHeading: "",
		TooltipDateFmt: "2 Jan",
		AllDayLabel:    "all day",
		TooltipFooter:  "",
	}
}

func testMeta() *store.Meta {
	return &store.Meta{Accounts: []store.AccountMeta{{
		Name:      "personal",
		Calendars: []store.CalendarMeta{{ID: "work", Name: "Work"}},
	}}}
}

func indexAt(gen time.Time, occs ...model.Occurrence) *store.OccurrenceIndex {
	return &store.OccurrenceIndex{GeneratedAt: gen, Occurrences: occs}
}

func standup(start time.Time) model.Occurrence {
	return model.Occurrence{
		UID: "a", Summary: "Standup", Location: "Room 2",
		Start: start, End: start.Add(30 * time.Minute),
		AccountID: "personal", CalendarID: "work",
	}
}

func TestRenderUpcoming(t *testing.T) {
	now := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	idx := indexAt(now, standup(now.Add(2*time.Hour)))
	got := Render(idx, testMeta(), testConfig(), nil, now, time.UTC)

	if got.Class != ClassUpcoming {
		t.Errorf("Class = %q, want %q", got.Class, ClassUpcoming)
	}
	if got.Text != "14:00 Standup" {
		t.Errorf("Text = %q, want \"14:00 Standup\"", got.Text)
	}
	if got.Alt != got.Class {
		t.Errorf("Alt = %q, want it to match Class %q", got.Alt, got.Class)
	}
	if got.Tooltip != "10 Jun\nStandup\n14:00–14:30 · Room 2\nWork" {
		t.Errorf("Tooltip = %q", got.Tooltip)
	}
	if got.Percentage != 0 {
		t.Errorf("Percentage = %d, want 0", got.Percentage)
	}
}

func TestRenderSoon(t *testing.T) {
	now := time.Date(2026, 6, 10, 13, 50, 0, 0, time.UTC)
	idx := indexAt(now, standup(time.Date(2026, 6, 10, 14, 0, 0, 0, time.UTC)))
	got := Render(idx, testMeta(), testConfig(), nil, now, time.UTC)
	if got.Class != ClassSoon {
		t.Errorf("Class = %q, want %q (10m out with a 15m lead time)", got.Class, ClassSoon)
	}
}

func TestRenderRelativeAppearsAtLeadTime(t *testing.T) {
	start := time.Date(2026, 6, 10, 14, 0, 0, 0, time.UTC)
	cfg := testConfig()
	cfg.TextFormat = "{start} {summary} · {relative}"

	beforeLeadTime := start.Add(-cfg.LeadTime - time.Minute)
	got := Render(indexAt(beforeLeadTime, standup(start)), testMeta(), cfg, nil, beforeLeadTime, time.UTC)
	if got.Class != ClassUpcoming || got.Text != "14:00 Standup" {
		t.Errorf("before lead time = class %q text %q, want upcoming without relative time", got.Class, got.Text)
	}

	atLeadTime := start.Add(-cfg.LeadTime)
	got = Render(indexAt(atLeadTime, standup(start)), testMeta(), cfg, nil, atLeadTime, time.UTC)
	if got.Class != ClassSoon || got.Text != "14:00 Standup · in 15m" {
		t.Errorf("at lead time = class %q text %q, want soon with relative time", got.Class, got.Text)
	}
}

func TestRenderSoonBoundaryIsInclusive(t *testing.T) {
	start := time.Date(2026, 6, 10, 14, 0, 0, 0, time.UTC)
	now := start.Add(-15 * time.Minute)
	got := Render(indexAt(now, standup(start)), testMeta(), testConfig(), nil, now, time.UTC)
	if got.Class != ClassSoon {
		t.Errorf("Class = %q at exactly the lead time, want %q", got.Class, ClassSoon)
	}
}

func TestRenderNowReportsProgress(t *testing.T) {
	start := time.Date(2026, 6, 10, 14, 0, 0, 0, time.UTC)
	now := start.Add(15 * time.Minute) // halfway through a 30-minute event
	cfg := testConfig()
	cfg.NowDuration = 20 * time.Minute
	got := Render(indexAt(now, standup(start)), testMeta(), cfg, nil, now, time.UTC)
	if got.Class != ClassNow {
		t.Errorf("Class = %q, want %q", got.Class, ClassNow)
	}
	if got.Percentage != 50 {
		t.Errorf("Percentage = %d, want 50", got.Percentage)
	}
}

func TestRenderNowDurationEndsAtConfiguredBoundary(t *testing.T) {
	start := time.Date(2026, 6, 10, 14, 0, 0, 0, time.UTC)
	cfg := testConfig()

	justBefore := start.Add(cfg.NowDuration - time.Nanosecond)
	got := Render(indexAt(justBefore, standup(start)), testMeta(), cfg, nil, justBefore, time.UTC)
	if got.Class != ClassNow {
		t.Errorf("Class just before now_duration = %q, want %q", got.Class, ClassNow)
	}

	atBoundary := start.Add(cfg.NowDuration)
	got = Render(indexAt(atBoundary, standup(start)), testMeta(), cfg, nil, atBoundary, time.UTC)
	if got.Class != ClassNone {
		t.Errorf("Class at now_duration boundary = %q, want %q", got.Class, ClassNone)
	}
}

func TestRenderAdvancesToNextEventAfterNowDuration(t *testing.T) {
	start := time.Date(2026, 6, 10, 14, 0, 0, 0, time.UTC)
	now := start.Add(10 * time.Minute)
	next := standup(start.Add(time.Hour))
	next.UID = "b"
	next.Summary = "Planning"

	got := Render(indexAt(now, standup(start), next), testMeta(), testConfig(), nil, now, time.UTC)
	if got.Class != ClassUpcoming || got.Text != "15:00 Planning" {
		t.Errorf("after now_duration got class %q text %q, want upcoming next event", got.Class, got.Text)
	}
}

func TestRenderZeroNowDurationDisablesNowState(t *testing.T) {
	start := time.Date(2026, 6, 10, 14, 0, 0, 0, time.UTC)
	now := start.Add(time.Minute)
	next := standup(start.Add(time.Hour))
	next.UID = "b"
	next.Summary = "Planning"
	cfg := testConfig()
	cfg.NowDuration = 0

	got := Render(indexAt(now, standup(start), next), testMeta(), cfg, nil, now, time.UTC)
	if got.Class != ClassUpcoming || got.Text != "15:00 Planning" {
		t.Errorf("zero now_duration got class %q text %q, want upcoming next event", got.Class, got.Text)
	}
}

func TestRenderOverlappingEventGetsItsOwnNowWindow(t *testing.T) {
	firstStart := time.Date(2026, 6, 10, 14, 0, 0, 0, time.UTC)
	now := firstStart.Add(11 * time.Minute)
	second := standup(firstStart.Add(10 * time.Minute))
	second.UID = "b"
	second.Summary = "Planning"

	got := Render(indexAt(now, standup(firstStart), second), testMeta(), testConfig(), nil, now, time.UTC)
	if got.Class != ClassNow || got.Text != "14:10 Planning" {
		t.Errorf("overlapping event got class %q text %q, want newly started event", got.Class, got.Text)
	}
}

func TestRenderNone(t *testing.T) {
	now := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	got := Render(indexAt(now), testMeta(), testConfig(), nil, now, time.UTC)
	if got.Class != ClassNone {
		t.Errorf("Class = %q, want %q", got.Class, ClassNone)
	}
	if got.Text != "" {
		t.Errorf("Text = %q, want empty so Waybar hides the module", got.Text)
	}
}

// The stale class is the visible signal that the sync timer has died. Without
// it the bar silently shows yesterday's meeting forever.
func TestRenderStaleWinsOverEverything(t *testing.T) {
	now := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	idx := indexAt(now.Add(-3*time.Hour), standup(now.Add(30*time.Minute)))
	got := Render(idx, testMeta(), testConfig(), nil, now, time.UTC)
	if got.Class != ClassStale {
		t.Errorf("Class = %q, want %q for a 3h-old index with a 2h threshold", got.Class, ClassStale)
	}
	if got.Tooltip == "" {
		t.Error("a stale module must explain itself in the tooltip")
	}
}

func TestRenderNeverGeneratedIndexIsStale(t *testing.T) {
	now := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	got := Render(&store.OccurrenceIndex{}, testMeta(), testConfig(), nil, now, time.UTC)
	if got.Class != ClassStale {
		t.Errorf("Class = %q, want %q for an index that was never generated", got.Class, ClassStale)
	}
}

func TestRenderSkipsPastEvents(t *testing.T) {
	now := time.Date(2026, 6, 10, 16, 0, 0, 0, time.UTC)
	idx := indexAt(now,
		standup(time.Date(2026, 6, 10, 9, 0, 0, 0, time.UTC)),
		model.Occurrence{UID: "b", Summary: "Retro",
			Start:     time.Date(2026, 6, 10, 17, 0, 0, 0, time.UTC),
			End:       time.Date(2026, 6, 10, 18, 0, 0, 0, time.UTC),
			AccountID: "personal", CalendarID: "work"},
	)
	got := Render(idx, testMeta(), testConfig(), nil, now, time.UTC)
	if got.Text != "17:00 Retro" {
		t.Errorf("Text = %q, want \"17:00 Retro\"", got.Text)
	}
}

// Waybar consumes exactly these keys; the shape is part of the contract.
func TestOutputJSONShape(t *testing.T) {
	now := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	out := Render(indexAt(now, standup(now.Add(2*time.Hour))), testMeta(), testConfig(), nil, now, time.UTC)
	b, err := json.Marshal(out)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(b, &decoded); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"text", "alt", "tooltip", "class", "percentage"} {
		if _, ok := decoded[key]; !ok {
			t.Errorf("output JSON is missing the %q key: %s", key, b)
		}
	}
	if len(decoded) != 5 {
		t.Errorf("output has %d keys, want exactly 5: %s", len(decoded), b)
	}
}

func TestRenderUsesCalendarDisplayName(t *testing.T) {
	now := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	cfg := testConfig()
	cfg.TextFormat = "{calendar}: {summary}"
	got := Render(indexAt(now, standup(now.Add(time.Hour))), testMeta(), cfg, nil, now, time.UTC)
	if got.Text != "Work: Standup" {
		t.Errorf("Text = %q, want \"Work: Standup\"", got.Text)
	}
}

// An occurrence whose calendar is not in meta still renders, falling back to
// the calendar ID rather than dropping the event.
func TestRenderUnknownCalendarFallsBackToID(t *testing.T) {
	now := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	o := standup(now.Add(time.Hour))
	o.CalendarID = "unlisted"
	cfg := testConfig()
	cfg.TextFormat = "{calendar}"
	got := Render(indexAt(now, o), testMeta(), cfg, nil, now, time.UTC)
	if got.Text != "unlisted" {
		t.Errorf("Text = %q, want \"unlisted\"", got.Text)
	}
}

// Staleness must beat every other state, including an event that is
// currently in progress -- otherwise a confidently "now" status would hide
// a dead sync timer for the one case where the ordering actually matters.
func TestRenderStaleBeatsInProgressEvent(t *testing.T) {
	now := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	inProgress := standup(now.Add(-15 * time.Minute)) // started 15m ago, 30m long: still running
	idx := indexAt(now.Add(-3*time.Hour), inProgress)
	got := Render(idx, testMeta(), testConfig(), nil, now, time.UTC)
	if got.Class != ClassStale {
		t.Errorf("Class = %q, want %q: a 3h-stale cache must win over an in-progress event", got.Class, ClassStale)
	}
}

// Calendar IDs are the last path segment of a collection href, so two accounts
// that each expose /personal/ share the ID "personal". The name lookup was
// keyed on that bare ID, so whichever account discovery happened to visit last
// silently supplied the display name for both.
func TestCalendarNamesAreScopedByAccount(t *testing.T) {
	meta := &store.Meta{Accounts: []store.AccountMeta{
		{Name: "home", Calendars: []store.CalendarMeta{{ID: "personal", Name: "Home Calendar"}}},
		{Name: "work", Calendars: []store.CalendarMeta{{ID: "personal", Name: "Work Calendar"}}},
	}}
	now := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)

	for _, tt := range []struct{ account, want string }{
		{"home", "10 Jun\nHome Calendar"},
		{"work", "10 Jun\nWork Calendar"},
	} {
		o := model.Occurrence{
			UID: "a", Summary: "Standup",
			Start: now.Add(2 * time.Hour), End: now.Add(150 * time.Minute),
			AccountID: tt.account, CalendarID: "personal",
		}
		cfg := testConfig()
		cfg.TooltipFormat = "{calendar}"
		got := Render(indexAt(now, o), meta, cfg, nil, now, time.UTC)
		if got.Tooltip != tt.want {
			t.Errorf("account %q: tooltip = %q, want %q", tt.account, got.Tooltip, tt.want)
		}
	}
}

func TestRenderSkipsHiddenCalendars(t *testing.T) {
	now := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	idx := indexAt(now, standup(now.Add(2*time.Hour)))

	shown := Render(idx, testMeta(), testConfig(), nil, now, time.UTC)
	if shown.Class != ClassUpcoming {
		t.Fatalf("baseline class = %q, want upcoming", shown.Class)
	}

	hiddenSet := model.HiddenSet([]string{"personal/work"})
	got := Render(idx, testMeta(), testConfig(), hiddenSet, now, time.UTC)
	if got.Class != ClassNone {
		t.Errorf("class = %q, want none once the only calendar is hidden", got.Class)
	}
	if got.Text != "" {
		t.Errorf("text = %q, want empty", got.Text)
	}
}

func TestRenderHidingIsPerAccount(t *testing.T) {
	now := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	idx := indexAt(now, standup(now.Add(2*time.Hour)))

	// an entry for a different account must not hide this one
	got := Render(idx, testMeta(), testConfig(), model.HiddenSet([]string{"other/work"}), now, time.UTC)
	if got.Class != ClassUpcoming {
		t.Errorf("class = %q, want upcoming -- other/work must not hide personal/work", got.Class)
	}
}

// --- tooltip list -----------------------------------------------------

func allDayOn(uid, summary string, day time.Time) model.Occurrence {
	y, m, d := day.Date()
	start := time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
	return model.Occurrence{
		UID: uid, Summary: summary, AllDay: true,
		Start: start, End: start.AddDate(0, 0, 1),
		AccountID: "personal", CalendarID: "work",
	}
}

func timedEvent(uid, summary, location string, start, end time.Time) model.Occurrence {
	return model.Occurrence{
		UID: uid, Summary: summary, Location: location,
		Start: start, End: end,
		AccountID: "personal", CalendarID: "work",
	}
}

// A non-UTC location, matching real installs: cmd/calterm/waybar.go always
// passes time.Local, and the previous version of this test ran at time.UTC,
// which happens to make groupDate's two branches (UTC-anchored for all-day,
// loc-anchored for timed) collapse onto the same instant and mask a real
// bug. See TestRenderTooltipAllDayAndTimedSameDay for the case that bug
// actually broke.
var testLoc = time.FixedZone("CEST", 2*60*60)

// negOffsetLoc is behind UTC (like US/Alaska), the only kind of location
// where groupDate's two anchors (UTC for all-day, loc for timed) can invert
// relative to Start-sorted order: a timed event late in loc's previous day
// can carry a UTC instant later than an all-day event's UTC-midnight anchor
// for the following day. A location ahead of UTC (like the CEST used
// elsewhere in this file, or the user's own real timezone) can never
// produce this, since a loc-date ahead of UTC never precedes the UTC-date
// of an earlier-or-equal instant.
var negOffsetLoc = time.FixedZone("negative-nine", -9*60*60)

func TestRenderTooltipGroupsByDayInOrder(t *testing.T) {
	now := time.Date(2026, 8, 27, 9, 0, 0, 0, time.UTC) // 11:00 CEST
	e1 := allDayOn("a", "Team vacation", now)
	e2 := timedEvent("b", "Design review", "Room 2",
		time.Date(2026, 8, 28, 14, 0, 0, 0, time.UTC), // 16:00 CEST
		time.Date(2026, 8, 28, 15, 0, 0, 0, time.UTC))

	cfg := testConfig()
	cfg.TooltipDateFmt = "02.01."
	cfg.TooltipFormat = "{start} · {summary} · {location}"

	got := Render(indexAt(now, e1, e2), testMeta(), cfg, nil, now, testLoc)
	// No "Fri" prefix on the 28.08. line: the tooltip prints its own date
	// header per group, so the pre-existing redundancy the brief calls out
	// ("Tue 09:00–11:00" under a "Tue 01.09." header) is fixed here too.
	want := "27.08.\nall day · Team vacation\n\n28.08.\n16:00 · Design review · Room 2"
	if got.Tooltip != want {
		t.Errorf("Tooltip = %q, want %q", got.Tooltip, want)
	}
}

// The case that groupDate's UTC/loc anchor mismatch actually broke: an
// all-day event and a timed event that fall on the same LOCAL day, at a
// non-UTC location. Before the fix, groupDate returned two instants that
// merely printed the same date string but were not Equal, so this rendered
// as two separate "27.08." blocks instead of one.
func TestRenderTooltipAllDayAndTimedSameDay(t *testing.T) {
	now := time.Date(2026, 8, 27, 9, 0, 0, 0, time.UTC) // 11:00 CEST
	allDay := allDayOn("a", "Holiday", now)
	timed := timedEvent("b", "Standup", "Room 2",
		time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC), // 14:00 CEST
		time.Date(2026, 8, 27, 12, 30, 0, 0, time.UTC))

	cfg := testConfig()
	cfg.TooltipDateFmt = "02.01."
	cfg.TooltipFormat = "{start} · {summary}"

	got := Render(indexAt(now, allDay, timed), testMeta(), cfg, nil, now, testLoc)
	want := "27.08.\nall day · Holiday\n14:00 · Standup"
	if got.Tooltip != want {
		t.Errorf("Tooltip = %q, want %q (one block for the local day, not two)", got.Tooltip, want)
	}
}

// A currently-ongoing multi-week event (a real, genuine absence in the
// user's actual cache, not a fixture edge case) has a Start weeks in the
// past. buildTooltip's inclusion filter (End after now) is right to include
// it -- it hasn't ended -- but grouping it under its own Start date filed a
// currently-running event under a heading that reads "Upcoming" with a date
// three weeks in the past. It must group under today instead, alongside
// whatever else is happening today, while the event's own line still says
// "all day" -- only the grouping key changes, not what the line reads.
func TestRenderTooltipInProgressMultiDayEventGroupsUnderToday(t *testing.T) {
	now := time.Date(2026, 8, 27, 9, 0, 0, 0, time.UTC) // 11:00 CEST

	ongoing := allDayOn("a", "Extended vacation", time.Date(2026, 8, 7, 0, 0, 0, 0, time.UTC))
	ongoing.End = time.Date(2026, 9, 3, 0, 0, 0, 0, time.UTC) // still running through now

	todayTimed := timedEvent("b", "Standup", "",
		time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC), // 14:00 CEST
		time.Date(2026, 8, 27, 13, 0, 0, 0, time.UTC))

	future := timedEvent("c", "Future thing", "",
		time.Date(2026, 8, 30, 10, 0, 0, 0, time.UTC), // 12:00 CEST
		time.Date(2026, 8, 30, 11, 0, 0, 0, time.UTC))

	cfg := testConfig()
	cfg.TooltipDateFmt = "02.01."
	cfg.TooltipFormat = "{start} · {summary}"

	got := Render(indexAt(now, ongoing, todayTimed, future), testMeta(), cfg, nil, now, testLoc)

	// No "Sun" prefix on the 30.08. line, for the same reason as
	// TestRenderTooltipGroupsByDayInOrder above: the tooltip's own date
	// header already says which day this is.
	want := "27.08.\nall day · Extended vacation\n14:00 · Standup\n\n30.08.\n12:00 · Future thing"
	if got.Tooltip != want {
		t.Errorf("Tooltip = %q, want %q", got.Tooltip, want)
	}
	if strings.Contains(got.Tooltip, "07.08.") {
		t.Errorf("Tooltip = %q, must not show a header dated before today for an in-progress event", got.Tooltip)
	}
}

// At a location behind UTC, groupDate's two anchors (UTC for all-day, loc
// for timed) can invert relative to Start-sorted order: a timed event late
// in loc's previous day can carry a UTC instant later than an all-day
// event's UTC-midnight anchor for the following day. Occurrences arrive
// sorted by absolute Start, so the day-keys are then NOT non-decreasing --
// the single-pass "flush when the key changes" loop, which assumes they
// are, splits one calendar day into two blocks and emits them out of
// chronological order. This is pre-existing: it does not depend on the
// in-progress clamp (today is well before every occurrence here, so the
// clamp never fires) and cannot be triggered from a location ahead of UTC
// (a loc-date ahead of UTC can never precede the UTC-date of an
// earlier-or-equal instant), which is why it wasn't caught by any test
// using testLoc (CEST, +2).
func TestRenderTooltipDayGroupsStayOrderedAtNegativeUTCOffset(t *testing.T) {
	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC) // well before every occurrence below

	allDay28 := allDayOn("a", "AllDay28", time.Date(2026, 8, 28, 0, 0, 0, 0, time.UTC))

	// UTC instant on the 28th, but 20:00 local the 27th at UTC-9 -- a later
	// absolute Start than allDay28's, yet an earlier local calendar day.
	timedLoc27 := timedEvent("b", "TimedLoc27", "",
		time.Date(2026, 8, 28, 5, 0, 0, 0, time.UTC),
		time.Date(2026, 8, 28, 6, 0, 0, 0, time.UTC))

	// Genuinely later, and on the 28th both in UTC and at UTC-9 (11:00 local).
	timedLoc28 := timedEvent("c", "TimedLoc28", "",
		time.Date(2026, 8, 28, 20, 0, 0, 0, time.UTC),
		time.Date(2026, 8, 28, 21, 0, 0, 0, time.UTC))

	cfg := testConfig()
	cfg.TooltipDays = 14
	cfg.TooltipDateFmt = "02.01."
	cfg.TooltipFormat = "{summary}"

	// Sorted by absolute Start, as the occurrence index guarantees.
	got := Render(indexAt(now, allDay28, timedLoc27, timedLoc28), testMeta(), cfg, nil, now, negOffsetLoc)

	want := "27.08.\nTimedLoc27\n\n28.08.\nAllDay28\nTimedLoc28"
	if got.Tooltip != want {
		t.Errorf("Tooltip = %q, want %q (one block per day, in chronological day order)", got.Tooltip, want)
	}
}

func TestRenderTooltipCapAndMoreLine(t *testing.T) {
	now := time.Date(2026, 8, 27, 9, 0, 0, 0, time.UTC)
	var occs []model.Occurrence
	for i := 0; i < 5; i++ {
		start := now.Add(time.Duration(i+1) * time.Hour)
		occs = append(occs, timedEvent("e", "Event", "", start, start.Add(30*time.Minute)))
	}
	cfg := testConfig()
	cfg.TooltipMax = 2
	cfg.TooltipFormat = "{start} · {summary}"

	got := Render(indexAt(now, occs...), testMeta(), cfg, nil, now, time.UTC)
	if !strings.Contains(got.Tooltip, "+3 more") {
		t.Errorf("Tooltip = %q, want it to report the 3 truncated events", got.Tooltip)
	}
	if strings.Count(got.Tooltip, "10:00") != 1 || strings.Count(got.Tooltip, "11:00") != 1 {
		t.Errorf("Tooltip = %q, want exactly the first two events listed", got.Tooltip)
	}
	if strings.Contains(got.Tooltip, "12:00") {
		t.Errorf("Tooltip = %q, must not list events past the cap", got.Tooltip)
	}
}

func TestRenderTooltipDaysWindowExcludesEventOutside(t *testing.T) {
	now := time.Date(2026, 8, 27, 9, 0, 0, 0, time.UTC)
	inWindow := timedEvent("a", "In window", "", now.AddDate(0, 0, 6), now.AddDate(0, 0, 6).Add(time.Hour))
	outside := timedEvent("b", "Outside window", "", now.AddDate(0, 0, 8), now.AddDate(0, 0, 8).Add(time.Hour))
	cfg := testConfig()
	cfg.TooltipDays = 7
	cfg.TooltipFormat = "{summary}"

	got := Render(indexAt(now, inWindow, outside), testMeta(), cfg, nil, now, time.UTC)
	if !strings.Contains(got.Tooltip, "In window") {
		t.Errorf("Tooltip = %q, want the in-window event listed", got.Tooltip)
	}
	if strings.Contains(got.Tooltip, "Outside window") {
		t.Errorf("Tooltip = %q, must exclude an event past tooltip_days", got.Tooltip)
	}
}

func TestRenderTooltipHiddenCalendarsExcluded(t *testing.T) {
	now := time.Date(2026, 8, 27, 9, 0, 0, 0, time.UTC)
	shown := timedEvent("a", "Shown", "", now.Add(time.Hour), now.Add(2*time.Hour))
	hidden := timedEvent("b", "Hidden", "", now.Add(3*time.Hour), now.Add(4*time.Hour))
	hidden.CalendarID = "personal-hidden"
	cfg := testConfig()
	cfg.TooltipFormat = "{summary}"

	got := Render(indexAt(now, shown, hidden), testMeta(), cfg,
		model.HiddenSet([]string{"personal/personal-hidden"}), now, time.UTC)
	if !strings.Contains(got.Tooltip, "Shown") {
		t.Errorf("Tooltip = %q, want the visible event listed", got.Tooltip)
	}
	if strings.Contains(got.Tooltip, "Hidden") {
		t.Errorf("Tooltip = %q, must not list a hidden calendar's event", got.Tooltip)
	}
}

func TestRenderTooltipEmptyHeadingAndFooterOmitted(t *testing.T) {
	now := time.Date(2026, 8, 27, 9, 0, 0, 0, time.UTC)
	e := timedEvent("a", "Standup", "", now.Add(time.Hour), now.Add(90*time.Minute))
	cfg := testConfig()
	cfg.TooltipHeading = ""
	cfg.TooltipFooter = ""
	cfg.TooltipDateFmt = "02.01."
	cfg.TooltipFormat = "{summary}"

	got := Render(indexAt(now, e), testMeta(), cfg, nil, now, time.UTC)
	want := "27.08.\nStandup"
	if got.Tooltip != want {
		t.Errorf("Tooltip = %q, want %q (no stray blank lines)", got.Tooltip, want)
	}
}

func TestRenderTooltipHeadingAndFooterPresent(t *testing.T) {
	now := time.Date(2026, 8, 27, 9, 0, 0, 0, time.UTC)
	e := timedEvent("a", "Standup", "", now.Add(time.Hour), now.Add(90*time.Minute))
	cfg := testConfig()
	cfg.TooltipHeading = "Upcoming"
	cfg.TooltipFooter = "Left-click: calendar"
	cfg.TooltipDateFmt = "02.01."
	cfg.TooltipFormat = "{summary}"

	got := Render(indexAt(now, e), testMeta(), cfg, nil, now, time.UTC)
	want := "Upcoming\n\n27.08.\nStandup\n\nLeft-click: calendar"
	if got.Tooltip != want {
		t.Errorf("Tooltip = %q, want %q", got.Tooltip, want)
	}
}

func TestRenderTooltipNoUpcomingEvents(t *testing.T) {
	now := time.Date(2026, 8, 27, 9, 0, 0, 0, time.UTC)
	cfg := testConfig()
	cfg.TooltipHeading = "Upcoming"
	got := Render(indexAt(now), testMeta(), cfg, nil, now, time.UTC)
	if got.Tooltip != "No upcoming events" {
		t.Errorf("Tooltip = %q, want \"No upcoming events\" without a heading when the body is empty", got.Tooltip)
	}
}

func TestRenderTooltipTodayAllDayEventIncludedAtMidday(t *testing.T) {
	// An all-day event's Start is midnight UTC, which is already "before
	// now" by midday -- a naive Start >= now filter would drop today's
	// all-day event from its own tooltip.
	now := time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)
	e := allDayOn("a", "Holiday", now)
	cfg := testConfig()
	cfg.TooltipFormat = "{summary}"

	got := Render(indexAt(now, e), testMeta(), cfg, nil, now, time.UTC)
	if !strings.Contains(got.Tooltip, "Holiday") {
		t.Errorf("Tooltip = %q, want today's all-day event included even though its Start is before now", got.Tooltip)
	}
}

func TestRenderStaleStillWinsWithUpcomingEventsPresent(t *testing.T) {
	now := time.Date(2026, 8, 27, 9, 0, 0, 0, time.UTC)
	idx := indexAt(now.Add(-3*time.Hour), timedEvent("a", "Standup", "", now.Add(time.Hour), now.Add(90*time.Minute)))
	cfg := testConfig()
	cfg.TooltipHeading = "Upcoming"

	got := Render(idx, testMeta(), cfg, nil, now, time.UTC)
	if got.Class != ClassStale {
		t.Fatalf("Class = %q, want %q", got.Class, ClassStale)
	}
	if strings.Contains(got.Tooltip, "Standup") || strings.Contains(got.Tooltip, "Upcoming") {
		t.Errorf("Tooltip = %q, a stale cache must show the stale message, never the list", got.Tooltip)
	}
}

func TestRenderTooltipEscapesPangoAndDoesNotDoubleEscapeAmpersand(t *testing.T) {
	now := time.Date(2026, 8, 27, 9, 0, 0, 0, time.UTC)
	e := timedEvent("a", "Tom & Jerry <live>", "R&D <lab>", now.Add(time.Hour), now.Add(90*time.Minute))
	cfg := testConfig()
	cfg.TooltipFormat = "{summary} · {location}"

	got := Render(indexAt(now, e), testMeta(), cfg, nil, now, time.UTC)
	if !strings.Contains(got.Tooltip, "Tom &amp; Jerry &lt;live&gt;") {
		t.Errorf("Tooltip = %q, want the summary escaped exactly once", got.Tooltip)
	}
	if !strings.Contains(got.Tooltip, "R&amp;D &lt;lab&gt;") {
		t.Errorf("Tooltip = %q, want the location escaped exactly once", got.Tooltip)
	}
	if strings.Contains(got.Tooltip, "&amp;amp;") || strings.Contains(got.Tooltip, "&amp;lt;") {
		t.Errorf("Tooltip = %q, ampersand was double-escaped", got.Tooltip)
	}
}

func TestRenderTextFieldNotPangoEscaped(t *testing.T) {
	now := time.Date(2026, 8, 27, 9, 0, 0, 0, time.UTC)
	e := timedEvent("a", "Tom & Jerry", "", now.Add(time.Hour), now.Add(90*time.Minute))
	cfg := testConfig()
	cfg.TextFormat = "{summary}"

	got := Render(indexAt(now, e), testMeta(), cfg, nil, now, time.UTC)
	if got.Text != "Tom & Jerry" {
		t.Errorf("Text = %q, want the raw unescaped summary -- Waybar's `text` field is not Pango markup", got.Text)
	}
}

// --- text keeps its day qualifier, tooltip drops it ----------------------
//
// A second deliberate asymmetry between the bar and the tooltip, pinned
// here the same way the Pango-escaping asymmetry above is: `text` shows a
// single event with no other context and still needs "which day is this",
// while the tooltip prints its own date header per group and would only
// repeat it. Both sides are pinned in this one file so the divergence is
// visible to whoever reads it next.

func TestRenderTextKeepsDayQualifierForNonTodayAllDayEvent(t *testing.T) {
	now := time.Date(2026, 8, 27, 9, 0, 0, 0, time.UTC) // Thursday
	e := allDayOn("a", "Summer horse party", now.AddDate(0, 0, 2))
	cfg := testConfig()
	cfg.TextFormat = "{start} {summary}"

	got := Render(indexAt(now, e), testMeta(), cfg, nil, now, time.UTC)
	if got.Text != "Sat all day Summer horse party" {
		t.Errorf("Text = %q, want the weekday-qualified all-day label", got.Text)
	}
}

func TestRenderTooltipDropsDayQualifierForNonTodayAllDayEvent(t *testing.T) {
	now := time.Date(2026, 8, 27, 9, 0, 0, 0, time.UTC) // Thursday
	e := allDayOn("a", "Summer horse party", now.AddDate(0, 0, 2))
	cfg := testConfig()
	cfg.TooltipFormat = "{start} · {summary}"

	got := Render(indexAt(now, e), testMeta(), cfg, nil, now, time.UTC)
	if !strings.Contains(got.Tooltip, "all day · Summer horse party") {
		t.Errorf("Tooltip = %q, want the bare all-day label with no weekday prefix", got.Tooltip)
	}
	if strings.Contains(got.Tooltip, "Sat all day") {
		t.Errorf("Tooltip = %q, weekday prefix must not appear -- the day header already says which day this is", got.Tooltip)
	}
}

// --- the new one-line default tooltip_format ("{start}–{end} · {summary}") ---
//
// These pin the shape from the brief's target output end to end, through
// Render, for every occurrence shape that has previously caused a defect in
// this module: a timed event, an all-day event (today and not-today), and a
// multi-week event already in progress -- the user's own calendar has a
// three-week absence of exactly this shape.

const oneLineTooltipFormat = "{start}–{end} · {summary}"

func TestRenderTooltipDefaultFormatTimedEvent(t *testing.T) {
	now := time.Date(2026, 9, 1, 7, 0, 0, 0, time.UTC) // Tuesday, 09:00 CEST
	e := timedEvent("a", "Team coffee (Optional)", "",
		time.Date(2026, 9, 1, 7, 0, 0, 0, time.UTC),
		time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC))
	cfg := testConfig()
	cfg.TooltipFormat = oneLineTooltipFormat

	got := Render(indexAt(now, e), testMeta(), cfg, nil, now, testLoc)
	want := "1 Sep\n09:00–11:00 · Team coffee (Optional)"
	if got.Tooltip != want {
		t.Errorf("Tooltip = %q, want %q", got.Tooltip, want)
	}
}

func TestRenderTooltipDefaultFormatAllDayEventToday(t *testing.T) {
	now := time.Date(2026, 8, 27, 9, 0, 0, 0, time.UTC) // Thursday, 11:00 CEST
	e := allDayOn("a", "Short vacation", now)
	cfg := testConfig()
	cfg.TooltipFormat = oneLineTooltipFormat

	got := Render(indexAt(now, e), testMeta(), cfg, nil, now, testLoc)
	want := "27 Aug\nall day · Short vacation"
	if got.Tooltip != want {
		t.Errorf("Tooltip = %q, want %q", got.Tooltip, want)
	}
}

// An all-day event on a later day: the tooltip line stays a bare "all day",
// with no weekday prefix duplicating the "29.08." group header above it.
func TestRenderTooltipDefaultFormatAllDayEventNotToday(t *testing.T) {
	now := time.Date(2026, 8, 27, 9, 0, 0, 0, time.UTC) // Thursday
	e := allDayOn("a", "Summer horse party", now.AddDate(0, 0, 2))
	cfg := testConfig()
	cfg.TooltipFormat = oneLineTooltipFormat

	got := Render(indexAt(now, e), testMeta(), cfg, nil, now, testLoc)
	want := "29 Aug\nall day · Summer horse party"
	if got.Tooltip != want {
		t.Errorf("Tooltip = %q, want %q", got.Tooltip, want)
	}
}

// The shape the user's own calendar has: a multi-week all-day absence
// already in progress. It must group under today, one bare "all day" line,
// same as any other today event -- not a stale three-week-old date.
func TestRenderTooltipDefaultFormatMultiWeekInProgressEvent(t *testing.T) {
	now := time.Date(2026, 8, 27, 9, 0, 0, 0, time.UTC) // Thursday
	e := allDayOn("a", "Extended vacation", time.Date(2026, 8, 6, 0, 0, 0, 0, time.UTC))
	e.End = time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC) // still running

	cfg := testConfig()
	cfg.TooltipFormat = oneLineTooltipFormat

	got := Render(indexAt(now, e), testMeta(), cfg, nil, now, testLoc)
	want := "27 Aug\nall day · Extended vacation"
	if got.Tooltip != want {
		t.Errorf("Tooltip = %q, want %q", got.Tooltip, want)
	}
}

func TestRenderTextShowsFutureAllDayRange(t *testing.T) {
	now := time.Date(2026, 9, 11, 9, 0, 0, 0, time.UTC)
	e := allDayOn("holiday", "Project vacation", time.Date(2026, 9, 14, 0, 0, 0, 0, time.UTC))
	e.End = time.Date(2026, 9, 19, 0, 0, 0, 0, time.UTC)
	cfg := testConfig()
	cfg.AllDayLabel = "full day"

	got := Render(indexAt(now, e), testMeta(), cfg, nil, now, time.UTC)
	if got.Text != "Mon–Fri full day Project vacation" {
		t.Errorf("Text = %q, want visible inclusive range", got.Text)
	}
}

// An empty summary must not leave a dangling " · " -- the exact defect this
// module has already shipped and fixed once (an empty {location} losing its
// leading separator).
func TestRenderTooltipDefaultFormatEmptySummaryNoDanglingSeparator(t *testing.T) {
	now := time.Date(2026, 9, 1, 7, 0, 0, 0, time.UTC)
	e := timedEvent("a", "", "",
		time.Date(2026, 9, 1, 7, 0, 0, 0, time.UTC),
		time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC))
	cfg := testConfig()
	cfg.TooltipFormat = oneLineTooltipFormat

	got := Render(indexAt(now, e), testMeta(), cfg, nil, now, testLoc)
	want := "1 Sep\n09:00–11:00"
	if got.Tooltip != want {
		t.Errorf("Tooltip = %q, want %q (no dangling separator)", got.Tooltip, want)
	}
}

// An empty location must not leave a dangling " · " either, even though the
// new default no longer includes {location} itself -- a user who adds it
// back per the example config's comment must still get the same collapsing
// behaviour the shipped default relied on.
func TestRenderTooltipCustomFormatWithLocationCollapsesEmptyLocation(t *testing.T) {
	now := time.Date(2026, 9, 1, 7, 0, 0, 0, time.UTC)
	e := timedEvent("a", "Standup", "",
		time.Date(2026, 9, 1, 7, 0, 0, 0, time.UTC),
		time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC))
	cfg := testConfig()
	cfg.TooltipFormat = "{start}–{end} · {summary} · {location}"

	got := Render(indexAt(now, e), testMeta(), cfg, nil, now, testLoc)
	want := "1 Sep\n09:00–11:00 · Standup"
	if got.Tooltip != want {
		t.Errorf("Tooltip = %q, want %q (no dangling separator)", got.Tooltip, want)
	}
}

// --- tooltip line width cap ------------------------------------------
//
// Follow-up from the user after seeing the compacted tooltip: a single
// event could still span 3 lines because Waybar itself wraps a long
// tooltip line at the window edge. Each tooltip line is now capped to a
// fixed display width (not a config option -- the one case that motivated
// this, a Gather meeting URL, already disappears with {location} dropped
// from the default format) so a genuine outlier gets truncated with an
// ellipsis instead of wrapping.
//
// The cap is measured in display cells via lipgloss.Width, not bytes: this
// project has already shipped one defect from that exact confusion, and the
// user's own calendar entries contain umlauts. The bar's `text` field is
// never truncated here -- Waybar truncates the bar itself, and a second
// truncation on top would take away text the user would otherwise see.

func TestRenderTooltipTruncatesLongSummary(t *testing.T) {
	now := time.Date(2026, 9, 1, 7, 0, 0, 0, time.UTC)
	e := timedEvent("a", strings.Repeat("A", 100), "",
		time.Date(2026, 9, 1, 7, 0, 0, 0, time.UTC),
		time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC))
	cfg := testConfig()
	cfg.TooltipFormat = "{summary}"

	got := Render(indexAt(now, e), testMeta(), cfg, nil, now, testLoc)
	line := strings.Split(got.Tooltip, "\n")[1]
	if !strings.HasSuffix(line, "…") {
		t.Errorf("tooltip line = %q, want it to end with the ellipsis mark", line)
	}
	if w := lipgloss.Width(line); w > 72 {
		t.Errorf("tooltip line width = %d, want <= 72 (line %q)", w, line)
	}
}

// A line landing exactly on the cap must be left alone -- no ellipsis
// appended to something that already fits.
func TestRenderTooltipLineAtExactLimitIsUnchanged(t *testing.T) {
	now := time.Date(2026, 9, 1, 7, 0, 0, 0, time.UTC)
	summary := strings.Repeat("A", 72)
	e := timedEvent("a", summary, "",
		time.Date(2026, 9, 1, 7, 0, 0, 0, time.UTC),
		time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC))
	cfg := testConfig()
	cfg.TooltipFormat = "{summary}"

	got := Render(indexAt(now, e), testMeta(), cfg, nil, now, testLoc)
	line := strings.Split(got.Tooltip, "\n")[1]
	if line != summary {
		t.Errorf("tooltip line = %q, want the untruncated summary %q", line, summary)
	}
}

// A summary made of multi-byte glyphs must be truncated by display cell, not
// by byte. Each "é" is 2 bytes but 1 cell, so a byte-based cap would both
// cut the string at the wrong visual width and risk splitting a multi-byte
// rune, producing invalid UTF-8.
func TestRenderTooltipTruncatesMultibyteRunesByDisplayCellNotByte(t *testing.T) {
	now := time.Date(2026, 9, 1, 7, 0, 0, 0, time.UTC)
	summary := strings.Repeat("é", 100) // 200 bytes, 100 cells
	e := timedEvent("a", summary, "",
		time.Date(2026, 9, 1, 7, 0, 0, 0, time.UTC),
		time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC))
	cfg := testConfig()
	cfg.TooltipFormat = "{summary}"

	got := Render(indexAt(now, e), testMeta(), cfg, nil, now, testLoc)
	line := strings.Split(got.Tooltip, "\n")[1]
	if !utf8.ValidString(line) {
		t.Fatalf("tooltip line is not valid UTF-8: %q", line)
	}
	if w := lipgloss.Width(line); w > 72 {
		t.Errorf("tooltip line width = %d, want <= 72 (line %q)", w, line)
	}
	if !strings.HasSuffix(line, "…") {
		t.Errorf("tooltip line = %q, want it to end with the ellipsis mark", line)
	}
	// Every rune before the ellipsis must be the original glyph -- proof the
	// cut landed on a rune boundary, not mid-character.
	for _, r := range strings.TrimSuffix(line, "…") {
		if r != 'é' {
			t.Errorf("tooltip line = %q, contains an unexpected rune before the ellipsis -- truncation split a multi-byte character", line)
			break
		}
	}
}

// The regression that matters most: truncation must happen before
// Pango-escaping, or a cut can land inside an entity like "&amp;" and
// produce invalid markup. A summary with a literal "&" positioned right at
// the cap must still yield a complete, well-formed entity (or none at all),
// never a dangling fragment such as "&am".
func TestRenderTooltipTruncationYieldsValidPangoMarkupAcrossAmpersand(t *testing.T) {
	now := time.Date(2026, 9, 1, 7, 0, 0, 0, time.UTC)
	// 70 filler chars + "&" as the 71st character (the last one that fits
	// under the 72-cell cap once the 1-cell ellipsis is reserved) + a long
	// tail that must be dropped.
	summary := strings.Repeat("A", 70) + "&" + strings.Repeat("B", 30)
	e := timedEvent("a", summary, "",
		time.Date(2026, 9, 1, 7, 0, 0, 0, time.UTC),
		time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC))
	cfg := testConfig()
	cfg.TooltipFormat = "{summary}"

	got := Render(indexAt(now, e), testMeta(), cfg, nil, now, testLoc)
	line := strings.Split(got.Tooltip, "\n")[1]
	want := strings.Repeat("A", 70) + "&amp;…"
	if line != want {
		t.Errorf("tooltip line = %q, want %q", line, want)
	}
	// No broken entity fragment anywhere in the line.
	for _, broken := range []string{"&am", "&l", "&g"} {
		if strings.Contains(line, broken) && !strings.Contains(line, broken+"p;") &&
			!strings.Contains(line, broken+"t;") {
			t.Errorf("tooltip line = %q, contains a broken Pango entity fragment %q", line, broken)
		}
	}
}

// Date headers and the "+N more" line are short and carry structure -- they
// must never be truncated, even if a user's own tooltip_date_fmt somehow
// produced something long.
func TestRenderTooltipCapAndMoreLineAndHeadersAreNeverTruncated(t *testing.T) {
	now := time.Date(2026, 8, 27, 9, 0, 0, 0, time.UTC)
	cfg := testConfig()
	cfg.TooltipMax = 1
	cfg.TooltipHeading = "Upcoming"
	e1 := timedEvent("a", "First", "", now.Add(time.Hour), now.Add(2*time.Hour))
	e2 := timedEvent("b", "Second", "", now.Add(3*time.Hour), now.Add(4*time.Hour))

	got := Render(indexAt(now, e1, e2), testMeta(), cfg, nil, now, testLoc)
	if !strings.Contains(got.Tooltip, "+1 more") {
		t.Errorf("Tooltip = %q, want an untruncated \"+1 more\" line", got.Tooltip)
	}
}

// A multi-line tooltip_format -- the old three-line default itself is
// still a legal, user-settable value -- must have each physical line
// capped independently, not the joined multi-line string treated as one
// 72-cell budget. This is exactly the shape that originally motivated
// truncation: a long location (e.g. a Gather meeting URL) on its own line.
func TestRenderTooltipTruncatesEachPhysicalLineOfMultiLineFormatIndependently(t *testing.T) {
	now := time.Date(2026, 9, 1, 7, 0, 0, 0, time.UTC)
	longURL := "https://meet.example.com/app/" + strings.Repeat("aB3", 40) + "/room"
	e := timedEvent("a", "Standup", longURL,
		time.Date(2026, 9, 1, 7, 0, 0, 0, time.UTC),
		time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC))
	cfg := testConfig()
	cfg.TooltipFormat = "{summary}\n{start}–{end} · {location}\n{calendar}"

	got := Render(indexAt(now, e), testMeta(), cfg, nil, now, testLoc)
	lines := strings.Split(got.Tooltip, "\n")
	// testConfig() sets no tooltip_heading, so the tooltip is just one
	// block: the day header, then this event's 3 physical lines.
	if len(lines) != 4 {
		t.Fatalf("Tooltip = %q, want exactly 4 lines (day header + 3 event lines)", got.Tooltip)
	}
	summaryLine, rangeLine, calendarLine := lines[1], lines[2], lines[3]

	if summaryLine != "Standup" {
		t.Errorf("summary line = %q, want it untouched (well under the cap)", summaryLine)
	}
	if calendarLine != "Work" {
		t.Errorf("calendar line = %q, want it untouched (well under the cap)", calendarLine)
	}
	if !strings.HasSuffix(rangeLine, "…") {
		t.Errorf("range/location line = %q, want it truncated with an ellipsis", rangeLine)
	}
	if w := lipgloss.Width(rangeLine); w > 72 {
		t.Errorf("range/location line width = %d, want <= 72 (line %q)", w, rangeLine)
	}
	if !strings.HasPrefix(rangeLine, "09:00–11:00 · ") {
		t.Errorf("range/location line = %q, want the time range kept at the start, only the location cut", rangeLine)
	}
}
