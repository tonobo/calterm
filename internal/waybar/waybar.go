// Package waybar renders the cached occurrence index into the JSON object that
// Waybar's custom module consumes. It is a pure function of the index and the
// current time, which is what makes it golden-testable.
package waybar

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"charm.land/lipgloss/v2"

	"github.com/tonobo/calterm/internal/config"
	"github.com/tonobo/calterm/internal/model"
	"github.com/tonobo/calterm/internal/store"
)

// Class values, used for both `class` and `alt`, and therefore as CSS hooks.
const (
	ClassNow      = "now"
	ClassSoon     = "soon"
	ClassUpcoming = "upcoming"
	ClassNone     = "none"
	ClassStale    = "stale"
)

// Output is the JSON object Waybar reads. The field set is the module's
// contract and must not gain or lose keys casually.
type Output struct {
	Text       string `json:"text"`
	Alt        string `json:"alt"`
	Tooltip    string `json:"tooltip"`
	Class      string `json:"class"`
	Percentage int    `json:"percentage"`
}

// Render turns the cached index into a status line.
//
// Staleness is checked first and beats every other state: a cache older than
// the threshold means the sync timer has stopped, and showing a confidently
// wrong "next event" would hide that.
//
// hidden is the set of calendars the user has deselected; occurrences on them
// are skipped. Because waybar runs as a separate process reading the config,
// it reflects the saved selection, not an unsaved one.
func Render(idx *store.OccurrenceIndex, meta *store.Meta, cfg config.WaybarConfig, hidden map[string]bool, now time.Time, loc *time.Location) Output {
	if loc == nil {
		loc = time.UTC
	}
	if idx.Age(now) > cfg.StaleAfter {
		return Output{
			Text:    "calendar stale",
			Alt:     ClassStale,
			Class:   ClassStale,
			Tooltip: "calterm: the calendar cache is out of date.\nIs the calterm-sync timer running?",
		}
	}

	names := calendarNames(meta)

	occs := idx.Occurrences
	if len(hidden) > 0 {
		kept := make([]model.Occurrence, 0, len(occs))
		for _, o := range occs {
			if !model.IsHidden(hidden, o.AccountID, o.CalendarID) {
				kept = append(kept, o)
			}
		}
		occs = kept
	}

	tooltip := buildTooltip(occs, names, cfg, now, loc)

	if current, ok := activeNowOccurrence(occs, now, cfg.NowDuration); ok {
		out := build(current, names, cfg, now, loc, ClassNow)
		out.Percentage = progress(current, now)
		out.Tooltip = tooltip
		return out
	}

	next, ok := model.NextAfter(occs, now)
	if !ok {
		return Output{Alt: ClassNone, Class: ClassNone, Tooltip: tooltip}
	}

	class := ClassUpcoming
	if next.Start.Sub(now) <= cfg.LeadTime {
		class = ClassSoon
	}
	out := build(next, names, cfg, now, loc, class)
	out.Tooltip = tooltip
	return out
}

// activeNowOccurrence returns the most recently started timed event whose
// configurable highlight window is still open. The event must also still be
// running, so a short event never remains highlighted past its actual end.
// Choosing the latest start matters when events overlap: a newly started
// meeting gets its own highlight even while an older one continues running.
func activeNowOccurrence(occs []model.Occurrence, now time.Time, window time.Duration) (model.Occurrence, bool) {
	if window <= 0 {
		return model.Occurrence{}, false
	}

	var active model.Occurrence
	found := false
	for _, o := range occs {
		if o.AllDay || o.End.Equal(o.Start) || o.Start.After(now) || !o.End.After(now) {
			continue
		}
		if now.Sub(o.Start) >= window {
			continue
		}
		if !found || o.Start.After(active.Start) {
			active = o
			found = true
		}
	}
	return active, found
}

func build(o model.Occurrence, names map[string]string, cfg config.WaybarConfig, now time.Time, loc *time.Location, class string) Output {
	name := calendarNameFor(o, names)
	return Output{
		Text:  Format(cfg.TextFormat, o, name, cfg.AllDayLabel, now, loc, ViewBar),
		Alt:   class,
		Class: class,
	}
}

// buildTooltip renders the list of upcoming appointments shown on hover.
//
// An occurrence is included if it has not yet ended (End after now) and
// starts before the tooltip_days window closes. "not yet ended", rather than
// "starts at or after now", is deliberate: an all-day event for today has a
// Start at midnight, already before any midday `now`, and a naive Start-based
// filter would drop today's own all-day entries from their own tooltip.
//
// Events are grouped by calendar day (in loc, except all-day events which are
// grouped by their UTC-anchored date, matching model.OnDay's convention),
// with a blank line between groups. tooltip_heading and tooltip_footer are
// each their own block, present only when non-empty. A cap truncation is
// reported as a "+N more" block. An empty result is reported as "No upcoming
// events" -- no heading is emitted over an empty body.
//
// occs must be sorted by Start, the same precondition model.NextAfter
// documents: buildTooltip relies on that order for which events survive the
// tooltip_max cap (the first N by Start) and for the display order within
// each resulting day group. It does NOT rely on groupDate's results being
// non-decreasing along that order -- they are not, in general: an all-day
// occurrence's grouping day is anchored in UTC while a timed occurrence's is
// anchored in loc (see groupDate), so at a location behind UTC a timed
// event late in loc's previous day can carry a later absolute Start than an
// all-day event's UTC-midnight anchor for the following day, without its
// group key being later too. buildTooltip corrects for that itself, by
// stable-sorting the (already Start-capped) list by group key -- stable so
// that occurrences sharing a key keep their Start order -- before grouping
// consecutive equal keys in a single pass.
func buildTooltip(occs []model.Occurrence, names map[string]string, cfg config.WaybarConfig, now time.Time, loc *time.Location) string {
	windowEnd := now.AddDate(0, 0, cfg.TooltipDays)

	var upcoming []model.Occurrence
	for _, o := range occs {
		if !o.End.After(now) {
			continue
		}
		if !o.Start.Before(windowEnd) {
			continue
		}
		upcoming = append(upcoming, o)
	}
	if len(upcoming) == 0 {
		return "No upcoming events"
	}

	total := len(upcoming)
	truncated := total > cfg.TooltipMax
	if truncated {
		upcoming = upcoming[:cfg.TooltipMax]
	}

	// Which N events survive the cap, and their order within a day, both
	// still come from Start order (via the slice above and the stability of
	// this sort); only the grouping into day-blocks is reordered.
	sort.SliceStable(upcoming, func(i, j int) bool {
		return groupDate(upcoming[i], now, loc).Before(groupDate(upcoming[j], now, loc))
	})

	var blocks []string
	if cfg.TooltipHeading != "" {
		blocks = append(blocks, escapePango(cfg.TooltipHeading))
	}

	var curDay time.Time
	var lines []string
	flush := func() {
		if len(lines) > 0 {
			header := escapePango(curDay.Format(cfg.TooltipDateFmt))
			blocks = append(blocks, header+"\n"+strings.Join(lines, "\n"))
		}
	}
	for i, o := range upcoming {
		day := groupDate(o, now, loc)
		if i == 0 || !day.Equal(curDay) {
			flush()
			curDay = day
			lines = nil
		}
		name := calendarNameFor(o, names)
		lines = append(lines, tooltipLine(o, name, cfg, now, loc))
	}
	flush()

	if truncated {
		blocks = append(blocks, fmt.Sprintf("+%d more", total-cfg.TooltipMax))
	}
	if cfg.TooltipFooter != "" {
		blocks = append(blocks, escapePango(cfg.TooltipFooter))
	}
	return strings.Join(blocks, "\n\n")
}

// groupDate returns the calendar day an occurrence belongs to, for grouping
// purposes. All-day occurrences are anchored to UTC (per model.Occurrence's
// AllDay contract); timed occurrences are grouped by their local day in loc.
//
// The result is always normalized into time.UTC, regardless of which zone
// supplied the y/m/d components. Returning it anchored in loc instead (as an
// earlier version of this function did) produces a time.Time whose calendar
// date prints identically to the UTC-anchored one from an all-day event on
// the same day, but is a different instant -- so comparing two groupDate
// results with Equal, as the caller does, silently splits one local day
// into two groups at any non-UTC location. Comparing calendar-date
// components (y, m, d) would work too; normalizing to a single zone is the
// same idea expressed as a time.Time so callers can still Format it.
//
// The result is then clamped up to today (in loc) when the occurrence has
// already started. buildTooltip's inclusion filter is "not yet ended", so an
// occurrence reaching this function with a start date before today is, by
// construction, still running today (a genuinely past occurrence would
// already have been filtered out) -- a multi-week absence that began three
// weeks ago must not be filed under a heading that reads "Upcoming" dated
// three weeks in the past. This changes only the grouping key: the line
// tooltipLine renders for the occurrence is untouched, so an all-day event
// still reads "all day".
//
// groupDate's results are NOT guaranteed non-decreasing along Start-sorted
// input, clamped or not: the UTC-vs-loc anchor split above means a timed
// event just after midnight UTC, at a location far enough behind UTC, can
// have a group key a full day earlier than an all-day event with a smaller
// (earlier) Start. buildTooltip does not assume otherwise -- it
// stable-sorts by this function's result before grouping, precisely because
// the result can be out of Start order.
func groupDate(o model.Occurrence, now time.Time, loc *time.Location) time.Time {
	var y int
	var m time.Month
	var d int
	if o.AllDay {
		y, m, d = o.Start.UTC().Date()
	} else {
		y, m, d = o.Start.In(loc).Date()
	}
	key := time.Date(y, m, d, 0, 0, 0, 0, time.UTC)

	ny, nm, nd := now.In(loc).Date()
	today := time.Date(ny, nm, nd, 0, 0, 0, 0, time.UTC)
	if key.Before(today) {
		return today
	}
	return key
}

// tooltipLineMaxWidth caps a single tooltip line's display width. It is a
// fixed constant, not a config option: the one case that motivated it (a
// Gather meeting URL wrapping across three lines on its own) already
// disappears with {location} dropped from the default tooltip_format, so
// this only needs to catch genuine outliers. 72 comfortably fits the
// longest line in the brief's own target output --
// "14:00–15:00 · Planning meeting (optional)"
// (69 cells) -- untouched.
const tooltipLineMaxWidth = 72

// tooltipTruncationMark is appended to a line cut short by
// tooltipLineMaxWidth. It counts as 1 display cell, already accounted for
// in truncateLine's budget.
const tooltipTruncationMark = "…"

// tooltipLine renders one event's tooltip line: Format against the raw
// (unescaped) occurrence, width-cap the result, and only then Pango-escape
// it. That order matters -- escaping first and truncating second could cut
// a byte-cap or even a rune-cap right through an entity like "&amp;",
// leaving invalid markup behind; truncating the raw text first and escaping
// the (already short) result afterward cannot do that, since escaping
// itself is a single whole-string replace pass. The bar's `text` field must
// never go through this path -- Waybar does not treat it as markup, and
// Waybar itself already truncates the bar, so capping it here too would
// take away text the user would otherwise see.
func tooltipLine(o model.Occurrence, calendarName string, cfg config.WaybarConfig, now time.Time, loc *time.Location) string {
	raw := Format(cfg.TooltipFormat, o, calendarName, cfg.AllDayLabel, now, loc, ViewTooltip)
	return escapePango(truncateTooltipLine(raw))
}

// truncateTooltipLine caps each physical line of s (tooltip_format can
// itself contain "\n") to tooltipLineMaxWidth display cells, independently.
func truncateTooltipLine(s string) string {
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		lines[i] = truncateLine(line)
	}
	return strings.Join(lines, "\n")
}

// truncateLine caps one line to tooltipLineMaxWidth display cells, measured
// with lipgloss.Width rather than len(): len() counts bytes, and a byte cap
// both measures the wrong thing for a wide or multi-byte rune (this
// module's users write German summaries with umlauts) and risks slicing a
// multi-byte rune in half, producing invalid UTF-8. Ranging over s (rather
// than indexing into it) decodes one rune at a time, so a cut can only ever
// land between runes, never inside one.
func truncateLine(s string) string {
	if lipgloss.Width(s) <= tooltipLineMaxWidth {
		return s
	}
	budget := tooltipLineMaxWidth - lipgloss.Width(tooltipTruncationMark)
	var b strings.Builder
	w := 0
	for _, r := range s {
		rw := lipgloss.Width(string(r))
		if w+rw > budget {
			break
		}
		b.WriteRune(r)
		w += rw
	}
	return b.String() + tooltipTruncationMark
}

// calendarNameFor looks up an occurrence's display name, falling back to the
// bare calendar ID when the calendar is not (or no longer) in meta.
func calendarNameFor(o model.Occurrence, names map[string]string) string {
	name, ok := names[o.CalendarKey()]
	if !ok || name == "" {
		return o.CalendarID
	}
	return name
}

func calendarNames(meta *store.Meta) map[string]string {
	names := map[string]string{}
	for _, acct := range meta.Accounts {
		for _, cal := range acct.Calendars {
			names[model.CalendarKey(acct.Name, cal.ID)] = cal.Name
		}
	}
	return names
}

// progress reports how far through an in-progress event we are, as a
// percentage clamped to [0, 100].
func progress(o model.Occurrence, now time.Time) int {
	total := o.End.Sub(o.Start)
	if total <= 0 {
		return 0
	}
	pct := int(now.Sub(o.Start) * 100 / total)
	if pct < 0 {
		return 0
	}
	if pct > 100 {
		return 100
	}
	return pct
}
