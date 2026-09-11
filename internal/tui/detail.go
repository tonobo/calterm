package tui

import (
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/tonobo/calterm/internal/model"
)

// RenderDetail draws every populated field of one occurrence. Empty fields are
// omitted entirely rather than shown with a blank value.
func RenderDetail(o model.Occurrence, width, height int, loc *time.Location, st Styles, names map[string]string) string {
	if loc == nil {
		loc = time.UTC
	}
	var lines []string

	lines = append(lines, st.DayHeader.Render(truncatePlain(nonEmpty(o.Summary, "(no title)"), width)))
	lines = append(lines, "")

	add := func(label, value string) {
		if value == "" {
			return
		}
		const labelWidth = 12
		valueWidth := width - labelWidth
		if valueWidth < 1 {
			valueWidth = 1
		}
		wrapped := detailValueLines(value, valueWidth, st.Summary)
		for i, line := range wrapped {
			lineLabel := ""
			if i == 0 {
				lineLabel = label
			}
			lines = append(lines, st.Dim.Render(pad(lineLabel, labelWidth))+line)
		}
	}

	add("When", formatWhen(o, loc))
	add("Location", o.Location)
	if name, ok := names[o.CalendarKey()]; ok && name != "" {
		add("Calendar", name)
	} else if o.CalendarID != "" {
		add("Calendar", o.CalendarID)
	}
	add("Repeats", DescribeRRule(o.RRuleText))
	if o.Status != "" && !strings.EqualFold(o.Status, "CONFIRMED") {
		add("Status", strings.ToLower(o.Status))
	}
	if o.AttendeeStatus != "" {
		label := participationLabel(o.AttendeeStatus)
		lines = append(lines, st.Dim.Render(pad("Response", 12))+
			participationStatusStyle(o.AttendeeStatus, st).Render(label)+
			st.Dim.Render(" · space r a accept · space r d decline"))
	}
	if o.Organizer != nil {
		add("Organizer", participantName(*o.Organizer))
	}
	add("URL", o.URL)

	if len(o.Attendees) > 0 {
		lines = append(lines, "")
		lines = append(lines, st.Dim.Render(fmt.Sprintf("Attendees (%d)", len(o.Attendees))))
		for _, attendee := range o.Attendees {
			lines = append(lines, renderParticipant(attendee, st))
		}
	}

	if o.Description != "" {
		lines = append(lines, "")
		lines = append(lines, detailValueLines(o.Description, width, st.Summary)...)
	}

	for i, line := range lines {
		lines[i] = truncate(line, width)
	}
	if len(lines) > height {
		lines = lines[:height]
	}
	return strings.Join(lines, "\n")
}

var webURLPattern = regexp.MustCompile(`(?i)https?://[^\s<>"\x00-\x20\x7f\x1b\x07]+`)

// detailValueLines wraps every logical value line instead of truncating it at
// the terminal edge. Web URLs become compact OSC-8 labels whose invisible
// target remains the complete address, like a Markdown link in a terminal.
func detailValueLines(value string, width int, style lipgloss.Style) []string {
	if width < 1 {
		width = 1
	}
	paragraphs := strings.Split(value, "\n")
	lines := make([]string, 0, len(paragraphs))
	for _, paragraph := range paragraphs {
		rendered := renderLinkedText(paragraph, style)
		lines = append(lines, strings.Split(ansi.Wrap(rendered, width, " "), "\n")...)
	}
	return lines
}

func renderLinkedText(text string, style lipgloss.Style) string {
	matches := webURLPattern.FindAllStringIndex(text, -1)
	if len(matches) == 0 {
		return style.Render(text)
	}

	var b strings.Builder
	last := 0
	for _, match := range matches {
		b.WriteString(style.Render(text[last:match[0]]))
		candidate := text[match[0]:match[1]]
		target, ok := safeWebURL(candidate)
		if !ok {
			b.WriteString(style.Render(candidate))
		} else {
			b.WriteString(style.Underline(true).Hyperlink(target).Render("↗ open link"))
		}
		last = match[1]
	}
	b.WriteString(style.Render(text[last:]))
	return b.String()
}

func safeWebURL(candidate string) (string, bool) {
	for _, r := range candidate {
		if unicode.IsControl(r) {
			return "", false
		}
	}
	u, err := url.ParseRequestURI(candidate)
	if err != nil || (strings.ToLower(u.Scheme) != "http" && strings.ToLower(u.Scheme) != "https") {
		return "", false
	}
	return candidate, true
}

func renderParticipant(attendee model.Participant, st Styles) string {
	statusStyle := participationStatusStyle(attendee.Status, st)
	status := participationLabel(attendee.Status)
	name := participantName(attendee)
	if attendee.Self {
		// Keep the complete identity portion visually connected while leaving
		// the final status in its semantic colour.
		return st.Selected.Render("  "+participationSymbol(attendee.Status)+" "+name+" (you) — ") +
			statusStyle.Bold(true).Render(status)
	}
	return st.Summary.Render("  ") + statusStyle.Render(participationSymbol(attendee.Status)) +
		st.Summary.Render(" "+name+" — ") + statusStyle.Render(status)
}

func participationStatusStyle(status string, st Styles) lipgloss.Style {
	switch strings.ToUpper(status) {
	case "ACCEPTED":
		return st.Success
	case "DECLINED":
		return st.Error
	case "NEEDS-ACTION", "TENTATIVE", "":
		return st.Pending
	case "DELEGATED":
		return st.Selected
	default:
		return st.Dim
	}
}

func participantName(p model.Participant) string {
	switch {
	case p.Name != "" && p.Email != "":
		return p.Name + " <" + p.Email + ">"
	case p.Name != "":
		return p.Name
	case p.Email != "":
		return p.Email
	default:
		return "unknown attendee"
	}
}

func participationLabel(status string) string {
	switch strings.ToUpper(status) {
	case "NEEDS-ACTION", "":
		return "needs action"
	case "ACCEPTED":
		return "accepted"
	case "DECLINED":
		return "declined"
	case "TENTATIVE":
		return "tentative"
	case "DELEGATED":
		return "delegated"
	default:
		return strings.ToLower(status)
	}
}

func participationSymbol(status string) string {
	switch strings.ToUpper(status) {
	case "ACCEPTED":
		return "✓"
	case "DECLINED":
		return "×"
	case "TENTATIVE":
		return "~"
	case "DELEGATED":
		return "→"
	default:
		return "?"
	}
}

func participationSummary(o model.Occurrence) string {
	if o.AttendeeStatus == "" {
		return o.Summary
	}
	return participationSymbol(o.AttendeeStatus) + " " + o.Summary
}

func formatWhen(o model.Occurrence, loc *time.Location) string {
	if o.AllDay {
		start := o.Start.UTC()
		// An all-day DTEND is exclusive, so subtract a day for display.
		end := o.End.UTC().AddDate(0, 0, -1)
		if start.Equal(end) || end.Before(start) {
			return "All day · " + start.Format("Mon 2 January 2006")
		}
		return "All day · " + start.Format("Mon 2 Jan") + " – " + end.Format("Mon 2 Jan 2006")
	}
	start := o.Start.In(loc)
	end := o.End.In(loc)
	if sameDay(start, end) {
		return start.Format("Mon 2 January 2006") + " · " + start.Format("15:04") + "–" + end.Format("15:04")
	}
	return start.Format("Mon 2 Jan 15:04") + " – " + end.Format("Mon 2 Jan 15:04")
}

func nonEmpty(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}

// DescribeRRule renders a recurrence rule in plain words. It covers the common
// shapes; anything it does not recognise falls back to "Repeats".
func DescribeRRule(rule string) string {
	if rule == "" {
		return ""
	}
	parts := map[string]string{}
	for _, kv := range strings.Split(rule, ";") {
		k, v, ok := strings.Cut(kv, "=")
		if ok {
			parts[strings.ToUpper(k)] = strings.ToUpper(v)
		}
	}

	unit, ok := map[string]string{
		"DAILY": "day", "WEEKLY": "week", "MONTHLY": "month", "YEARLY": "year",
	}[parts["FREQ"]]
	if !ok {
		return "Repeats"
	}

	interval := 1
	if n, err := strconv.Atoi(parts["INTERVAL"]); err == nil && n > 0 {
		interval = n
	}

	var b strings.Builder
	if interval == 1 {
		fmt.Fprintf(&b, "Every %s", unit)
	} else {
		fmt.Fprintf(&b, "Every %d %ss", interval, unit)
	}
	if n, err := strconv.Atoi(parts["COUNT"]); err == nil && n > 0 {
		fmt.Fprintf(&b, ", %d times", n)
	}
	if until := parts["UNTIL"]; until != "" {
		if t, err := time.Parse("20060102T150405Z", until); err == nil {
			fmt.Fprintf(&b, ", until %s", t.Format("2 Jan 2006"))
		}
	}
	return b.String()
}

// Filter narrows occurrences to those whose summary or location contains the
// query, case-insensitively. An empty query matches everything.
func Filter(occs []model.Occurrence, query string) []model.Occurrence {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return occs
	}
	var out []model.Occurrence
	for _, o := range occs {
		if strings.Contains(strings.ToLower(o.Summary), q) ||
			strings.Contains(strings.ToLower(o.Location), q) {
			out = append(out, o)
		}
	}
	return out
}
