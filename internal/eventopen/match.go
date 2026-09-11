// Package eventopen matches an externally supplied iCalendar event against
// calterm's derived occurrence cache.
package eventopen

import (
	"strings"
	"time"

	"github.com/tonobo/calterm/internal/config"
	"github.com/tonobo/calterm/internal/model"
	"github.com/tonobo/calterm/internal/store"
)

// Match returns the cached occurrence described by ref. A UID plus an exact
// RECURRENCE-ID wins. Non-recurring events match an empty RECURRENCE-ID, and a
// recurring reference may fall back to the occurrence whose DTSTART is
// nearest to the DTSTART in the supplied file.
//
// The complete index is searched intentionally; calendar visibility is a UI
// preference and must not prevent an invitation opened from aerc from being
// found. When otherwise equivalent copies exist in more than one account,
// the account whose configured email appears in the invitation is preferred.
func Match(ref model.EventReference, idx *store.OccurrenceIndex, cfg *config.Config) (model.Occurrence, bool) {
	if idx == nil || strings.TrimSpace(ref.UID) == "" {
		return model.Occurrence{}, false
	}

	uidCandidates := make([]model.Occurrence, 0)
	for _, occurrence := range idx.Occurrences {
		if occurrence.UID == ref.UID {
			uidCandidates = append(uidCandidates, occurrence)
		}
	}
	if len(uidCandidates) == 0 {
		return model.Occurrence{}, false
	}

	// A recurring master has no RECURRENCE-ID in the invitation but every
	// materialised cache occurrence does. It therefore goes directly to the
	// DTSTART fallback instead of accidentally preferring an unrelated
	// non-recurring copy whose recurrence ID also happens to be empty.
	if ref.RecurrenceID != "" || !ref.Recurring {
		exact := make([]model.Occurrence, 0, len(uidCandidates))
		for _, occurrence := range uidCandidates {
			if occurrence.RecurrenceID == ref.RecurrenceID {
				exact = append(exact, occurrence)
			}
		}
		if len(exact) > 0 {
			return chooseByIdentity(exact, ref, cfg), true
		}
	}

	if !ref.Recurring {
		return model.Occurrence{}, false
	}

	nearestDistance := time.Duration(1<<63 - 1)
	nearest := make([]model.Occurrence, 0, len(uidCandidates))
	for _, occurrence := range uidCandidates {
		distance := absoluteDuration(occurrence.Start.Sub(ref.Start))
		switch {
		case distance < nearestDistance:
			nearestDistance = distance
			nearest = append(nearest[:0], occurrence)
		case distance == nearestDistance:
			nearest = append(nearest, occurrence)
		}
	}
	return chooseByIdentity(nearest, ref, cfg), true
}

func absoluteDuration(d time.Duration) time.Duration {
	if d < 0 {
		return -d
	}
	return d
}

func chooseByIdentity(candidates []model.Occurrence, ref model.EventReference, cfg *config.Config) model.Occurrence {
	if len(candidates) == 0 {
		return model.Occurrence{}
	}

	incomingAttendees := participantEmails(ref.Attendees)
	accountEmails := map[string]string{}
	if cfg != nil {
		for _, account := range cfg.Accounts {
			accountEmails[account.Name] = model.EmailAddress(account.Email)
		}
	}

	best := candidates[0]
	bestScore := identityScore(best, incomingAttendees, accountEmails)
	for _, candidate := range candidates[1:] {
		score := identityScore(candidate, incomingAttendees, accountEmails)
		if score > bestScore {
			best, bestScore = candidate, score
		}
	}
	return best
}

func identityScore(candidate model.Occurrence, incomingAttendees map[string]bool, accountEmails map[string]string) int {
	email := accountEmails[candidate.AccountID]
	if email == "" {
		return 0
	}
	score := 1 // a configured account is preferable to an orphaned cache entry
	if incomingAttendees[email] {
		score += 4
	}
	if participantEmails(candidate.Attendees)[email] {
		score += 2
	}
	return score
}

func participantEmails(participants []model.Participant) map[string]bool {
	out := make(map[string]bool, len(participants))
	for _, participant := range participants {
		if email := model.EmailAddress(participant.Email); email != "" {
			out[email] = true
		}
	}
	return out
}
