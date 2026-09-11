// Package index rebuilds the derived occurrence index from the raw iCalendar
// objects held in the store.
package index

import (
	"bytes"
	"log/slog"
	"sort"
	"time"

	"github.com/emersion/go-ical"

	"github.com/tonobo/calterm/internal/model"
	"github.com/tonobo/calterm/internal/store"
)

// Window bounds, in months relative to now.
const (
	WindowBack    = -1
	WindowForward = 12
)

// DefaultWindow is the rolling range the index covers.
func DefaultWindow(now time.Time) model.Window {
	return model.Window{
		From: now.AddDate(0, WindowBack, 0),
		To:   now.AddDate(0, WindowForward, 0),
	}
}

// Build expands every cached object of every calendar named in meta into a
// single sorted occurrence index. Objects that cannot be parsed are logged and
// skipped: one malformed event written by another client must not blank the
// calendar.
func Build(s *store.Store, meta *store.Meta, now time.Time, loc *time.Location) (*store.OccurrenceIndex, error) {
	if loc == nil {
		loc = time.UTC
	}
	win := DefaultWindow(now)

	var all []model.Occurrence
	for _, acct := range meta.Accounts {
		for _, cal := range acct.Calendars {
			// Iterate the calendar's own index rather than listing the objects
			// directory: the index is the single authority on what this
			// calendar contains. A stray .ics on disk -- an object whose href
			// was rewritten with a new UID, a leftover from an interrupted
			// sync -- is then simply inert rather than a ghost event that
			// reappears on every rebuild.
			calIdx, err := s.LoadIndex(acct.Name, cal.ID)
			if err != nil {
				return nil, err
			}
			uids := make([]string, 0, len(calIdx.Objects))
			seen := make(map[string]bool, len(calIdx.Objects))
			for _, ref := range calIdx.Objects {
				if ref.UID == "" || seen[ref.UID] {
					continue
				}
				seen[ref.UID] = true
				uids = append(uids, ref.UID)
			}
			// Map iteration order is random; the final sort is by start time
			// and would otherwise leave same-instant events shuffling between
			// rebuilds.
			sort.Strings(uids)
			for _, uid := range uids {
				body, err := s.ReadObject(acct.Name, cal.ID, uid)
				if err != nil {
					slog.Warn("skipping unreadable calendar object",
						"account", acct.Name, "calendar", cal.ID, "uid", uid, "err", err)
					continue
				}
				parsed, err := ical.NewDecoder(bytes.NewReader(body)).Decode()
				if err != nil {
					slog.Warn("skipping unparseable calendar object",
						"account", acct.Name, "calendar", cal.ID, "uid", uid, "err", err)
					continue
				}
				occs, err := model.Expand(parsed, win, loc)
				if err != nil {
					slog.Warn("skipping object that failed to expand",
						"account", acct.Name, "calendar", cal.ID, "uid", uid, "err", err)
					continue
				}
				for i := range occs {
					occs[i].AccountID = acct.Name
					occs[i].CalendarID = cal.ID
				}
				all = append(all, occs...)
			}
		}
	}

	sort.SliceStable(all, func(i, j int) bool {
		if !all[i].Start.Equal(all[j].Start) {
			return all[i].Start.Before(all[j].Start)
		}
		if all[i].CalendarID != all[j].CalendarID {
			return all[i].CalendarID < all[j].CalendarID
		}
		return all[i].UID < all[j].UID
	})

	return &store.OccurrenceIndex{
		GeneratedAt: oldestLastSync(meta),
		From:        win.From,
		To:          win.To,
		Occurrences: all,
	}, nil
}

// oldestLastSync reports the least recent instant at which any account in meta
// completed a fully successful sync.
//
// The index's freshness is EARNED by a successful fetch, not by the rebuild
// that follows one. Stamping `now` here -- which is what this code used to do
// -- meant that a run in which every account failed still wrote a brand-new
// timestamp over identical content, so waybar's staleness warning could never
// fire for the one failure mode it exists to catch: the sync timer running AND
// failing. An expired password or a dropped VPN would leave the user staring
// at last week's meetings, indefinitely and confidently.
//
// The OLDEST rather than the newest, so a healthy account cannot vouch for a
// dead one. An account that has never successfully synced (zero LastSync)
// makes the whole index count as never generated, which Age reports as
// maximally stale.
func oldestLastSync(meta *store.Meta) time.Time {
	if len(meta.Accounts) == 0 {
		return time.Time{}
	}
	// The zero time is naturally the minimum, so an account that has never
	// synced propagates out of this as the zero result with no special case.
	oldest := meta.Accounts[0].LastSync
	for _, acct := range meta.Accounts[1:] {
		if acct.LastSync.Before(oldest) {
			oldest = acct.LastSync
		}
	}
	return oldest
}
