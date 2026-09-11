// Package sync brings one calendar's cached objects up to date with the
// server, using a CTag fast path and an ETag diff.
package sync

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"path"
	"strings"

	"github.com/emersion/go-ical"

	"github.com/tonobo/calterm/internal/caldav"
	"github.com/tonobo/calterm/internal/store"
)

// Result reports what one calendar's sync did.
type Result struct {
	Calendar  string
	Fetched   int
	Deleted   int
	Missing   int
	Unchanged bool
}

// Calendar synchronises a single calendar collection into the store.
//
// The steps are: check the CTag and stop if it is unchanged; PROPFIND the
// object ETags and diff them against the cached index; multiget only the
// objects whose ETag moved; apply deletions. The index is written last, so a
// failure at any point leaves the previous cache and CTag intact.
func Calendar(ctx context.Context, c *caldav.Client, s *store.Store, account, calendarID, calendarPath string) (Result, error) {
	res := Result{Calendar: calendarID}

	idx, err := s.LoadIndex(account, calendarID)
	if err != nil {
		return res, fmt.Errorf("loading index for %s: %w", calendarID, err)
	}

	props, err := c.CalendarProps(ctx, calendarPath)
	if err != nil {
		return res, fmt.Errorf("reading properties of %s: %w", calendarID, err)
	}
	// An empty CTag proves nothing: a server that does not implement getctag
	// would otherwise look permanently unchanged.
	if props.CTag != "" && props.CTag == idx.CTag {
		res.Unchanged = true
		return res, nil
	}

	remote, err := c.ListObjectETags(ctx, calendarPath)
	if err != nil {
		return res, fmt.Errorf("listing objects in %s: %w", calendarID, err)
	}

	remoteByHref := make(map[string]caldav.Resource, len(remote))
	var toFetch []string
	for _, r := range remote {
		remoteByHref[r.Href] = r
		cached, ok := idx.Objects[r.Href]
		if !ok || cached.ETag != r.ETag {
			toFetch = append(toFetch, r.Href)
		}
	}

	fetched, err := c.MultiGet(ctx, calendarPath, toFetch)
	if err != nil {
		return res, fmt.Errorf("fetching objects from %s: %w", calendarID, err)
	}

	// Work on a copy of the index so a failure above leaves the stored one
	// untouched.
	next := &store.Index{
		CTag:      props.CTag,
		SyncToken: idx.SyncToken,
		Objects:   make(map[string]store.ObjectRef, len(remote)),
	}
	for href, ref := range idx.Objects {
		if _, stillThere := remoteByHref[href]; stillThere {
			next.Objects[href] = ref
		}
	}

	// committed tracks which of toFetch actually landed. caldav.MultiGet
	// silently drops any requested href whose response carried no
	// calendar-data (a delete racing the multiget, or a per-object server
	// hiccup inside an otherwise-200 multistatus) -- it returns no error for
	// that. If we let the CTag advance anyway, the dropped object keeps its
	// stale ETag and content forever: the next sync's ctag fast path will
	// see props.CTag == idx.CTag and report Unchanged without ever diffing
	// again. So the CTag may only advance once every intended fetch has been
	// written.
	committed := make(map[string]bool, len(toFetch))

	// superseded collects UIDs that some href no longer refers to -- either
	// because a still-present href was rewritten to a different UID (below),
	// or because an href vanished from the server entirely (further down).
	// We do NOT delete these as we notice them. UIDs are a namespace shared
	// across hrefs, not owned by one: the server can swap which href holds
	// which UID (A<->B), or move a UID from a vanished href to a fresh one
	// (a rename), and in both cases the "new" owner of a UID may already
	// have been written to disk by the time we notice the "old" owner lost
	// it -- or may be written later in this same sync. Deleting eagerly, in
	// href-processing order, means whichever href is processed second can
	// delete the file the first one just wrote (or is about to write),
	// destroying an object that is still legitimately referenced by the
	// index we are about to save. So we only decide what to delete once
	// next.Objects is final, checked against which UIDs it still uses.
	superseded := make(map[string]bool)

	for _, obj := range fetched {
		uid := uidFromICS(obj.CalendarData)
		if uid == "" {
			uid = uidFromHref(obj.Href)
		}
		if uid == "" {
			slog.Warn("skipping object with no usable UID", "calendar", calendarID, "href", obj.Href)
			continue
		}
		// Objects are keyed on disk by UID but tracked by href. If the server
		// rewrote this href with a different UID -- another client replacing
		// the event, or a body that has since grown a real UID where
		// uidFromHref was used -- the old UID may no longer be referenced by
		// anything once this sync finishes. Record it; do not delete yet
		// (see the comment on `superseded` above).
		if cached, ok := idx.Objects[obj.Href]; ok && cached.UID != "" && cached.UID != uid {
			superseded[cached.UID] = true
		}
		if err := s.WriteObject(account, calendarID, uid, []byte(obj.CalendarData)); err != nil {
			return res, fmt.Errorf("writing object %s: %w", obj.Href, err)
		}
		etag := obj.ETag
		if etag == "" {
			etag = remoteByHref[obj.Href].ETag
		}
		next.Objects[obj.Href] = store.ObjectRef{Href: obj.Href, ETag: etag, UID: uid}
		committed[obj.Href] = true
		res.Fetched++
	}

	for _, href := range toFetch {
		if !committed[href] {
			res.Missing++
		}
	}
	if res.Missing > 0 {
		// Withhold the new CTag so the next sync falls through the fast
		// path's equality check, re-diffs by ETag, and retries exactly the
		// missing objects -- self-healing with no user intervention and no
		// full resync. Persisting props.CTag here is precisely the bug: it
		// would make the stale object's staleness permanent and silent.
		next.CTag = ""
		slog.Warn("sync left objects unfetched; withholding CTag so the next sync retries them",
			"calendar", calendarID, "missing", res.Missing)
	}

	for href, ref := range idx.Objects {
		if _, stillThere := remoteByHref[href]; stillThere {
			continue
		}
		// Same reasoning as `superseded` above: this href vanished, but its
		// UID may have moved to a different href that this same sync fetched
		// (a rename), so it can already be a live entry in next.Objects.
		// Record it and defer the deletion decision to the check below.
		superseded[ref.UID] = true
		delete(next.Objects, href)
		res.Deleted++
	}

	// next.Objects is now final. Only now do we know, for certain, which
	// UIDs are still legitimately referenced -- so only now is it safe to
	// delete anything. Never delete a UID that next.Objects still uses.
	surviving := make(map[string]bool, len(next.Objects))
	for _, ref := range next.Objects {
		surviving[ref.UID] = true
	}
	for uid := range superseded {
		if surviving[uid] {
			continue
		}
		if err := s.DeleteObject(account, calendarID, uid); err != nil {
			return res, fmt.Errorf("deleting superseded object (uid %s): %w", uid, err)
		}
	}

	if err := s.SaveIndex(account, calendarID, next); err != nil {
		return res, fmt.Errorf("saving index for %s: %w", calendarID, err)
	}
	return res, nil
}

// uidFromICS reads the UID of the first component that has one. Parsing is
// cheap here and gives a stable on-disk filename across href changes.
func uidFromICS(data string) string {
	cal, err := ical.NewDecoder(bytes.NewReader([]byte(data))).Decode()
	if err != nil {
		return ""
	}
	for _, ev := range cal.Events() {
		if uid, err := ev.Props.Text(ical.PropUID); err == nil && uid != "" {
			return uid
		}
	}
	return ""
}

// uidFromHref falls back to the object's filename when the body has no UID.
func uidFromHref(href string) string {
	base := path.Base(href)
	return strings.TrimSuffix(base, ".ics")
}
