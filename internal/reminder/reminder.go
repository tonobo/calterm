// Package reminder selects calendar occurrences that are due for a desktop
// notification and persists enough state to emit each one only once.
package reminder

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strings"
	"time"

	"github.com/tonobo/calterm/internal/model"
)

// StartGrace lets a minute-based timer still deliver an alert when scheduling
// jitter starts the command just after its target threshold. It is
// intentionally short: resuming a laptop hours later must not replay a backlog
// of old reminders.
const StartGrace = 90 * time.Second

// Rule enables one exact notification threshold for an account-scoped
// calendar.
type Rule struct {
	Calendar string
	Before   time.Duration
	Sound    string
}

// Delivery pairs a due occurrence with the rule that made it due.
type Delivery struct {
	Occurrence model.Occurrence
	Before     time.Duration
	Sound      string
}

// Due returns reminders whose exact delivery threshold was reached within
// StartGrace. A zero threshold means the event start. The result is ordered by
// target delivery time regardless of the order of occs or rules.
func Due(occs []model.Occurrence, state *State, rules []Rule, now time.Time) []Delivery {
	rulesByCalendar := make(map[string][]Rule)
	for _, rule := range rules {
		rulesByCalendar[rule.Calendar] = append(rulesByCalendar[rule.Calendar], rule)
	}
	windowStart := now.Add(-StartGrace)
	due := make([]Delivery, 0)
	pending := make(map[string]bool)
	for _, occurrence := range occs {
		if occurrence.AllDay || strings.EqualFold(occurrence.Status, "cancelled") || strings.EqualFold(occurrence.AttendeeStatus, "declined") {
			continue
		}
		calendarRules := rulesByCalendar[occurrence.CalendarKey()]
		if len(calendarRules) == 0 {
			continue
		}
		if occurrence.End.Before(now) {
			continue
		}
		for _, rule := range calendarRules {
			target := occurrence.Start.Add(-rule.Before)
			if target.Before(windowStart) || target.After(now) {
				continue
			}
			key := occurrenceKey(occurrence, rule.Before)
			if pending[key] || state != nil && state.Has(occurrence, rule.Before) {
				continue
			}
			pending[key] = true
			due = append(due, Delivery{
				Occurrence: occurrence,
				Before:     rule.Before,
				Sound:      rule.Sound,
			})
		}
	}
	sort.SliceStable(due, func(i, j int) bool {
		leftTarget := due[i].Occurrence.Start.Add(-due[i].Before)
		rightTarget := due[j].Occurrence.Start.Add(-due[j].Before)
		if leftTarget.Equal(rightTarget) {
			if due[i].Occurrence.Start.Equal(due[j].Occurrence.Start) {
				return due[i].Occurrence.UID < due[j].Occurrence.UID
			}
			return due[i].Occurrence.Start.Before(due[j].Occurrence.Start)
		}
		return leftTarget.Before(rightTarget)
	})
	return due
}

// occurrenceKey deliberately hashes the scheduling identity. The state file
// only needs a stable deduplication token, not another readable copy of event
// UIDs or account metadata.
func occurrenceKey(occurrence model.Occurrence, before time.Duration) string {
	identity := occurrence.UID + "\x00" + occurrence.RecurrenceID + "\x00" + occurrence.Start.UTC().Format(time.RFC3339Nano) + "\x00" + before.String()
	sum := sha256.Sum256([]byte(identity))
	return hex.EncodeToString(sum[:])
}
