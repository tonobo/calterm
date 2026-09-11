package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	stdsync "sync"
	"time"

	"github.com/tonobo/calterm/internal/caldav"
	"github.com/tonobo/calterm/internal/config"
	"github.com/tonobo/calterm/internal/index"
	"github.com/tonobo/calterm/internal/store"
	calendarsync "github.com/tonobo/calterm/internal/sync"
	"github.com/tonobo/calterm/internal/webcal"
)

// Keep network concurrency bounded: calendars are independent and benefit
// from parallel I/O, but opening an unbounded number of requests can overload
// a self-hosted CalDAV server.
const maxConcurrentSyncRequests = 4

func syncCommand(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("sync", flag.ContinueOnError)
	fs.SetOutput(stderr)
	configPath := fs.String("config", "", "configuration file")
	quiet := fs.Bool("quiet", false, "print nothing on success")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	cfg, s, _, err := loadEnvironment(*configPath)
	if err != nil {
		fmt.Fprintf(stderr, "calterm: %v\n", err)
		return 1
	}

	out := stdout
	if *quiet {
		out = io.Discard
	}
	if err := runSync(cfg, s, time.Now(), out); err != nil {
		fmt.Fprintf(stderr, "calterm: %v\n", err)
		return 1
	}
	return 0
}

// loadEnvironment resolves the config file and opens the cache store. It is
// shared by every subcommand that needs both. It also returns the resolved
// config path, so a caller that needs to write back to it (the TUI's
// calendar selector) does not have to re-derive it.
func loadEnvironment(configPath string) (*config.Config, *store.Store, string, error) {
	if configPath == "" {
		p, err := config.DefaultConfigPath()
		if err != nil {
			return nil, nil, "", fmt.Errorf("locating the config file: %w", err)
		}
		configPath = p
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		return nil, nil, "", err
	}
	cacheDir, err := config.CacheDir()
	if err != nil {
		return nil, nil, "", fmt.Errorf("locating the cache directory: %w", err)
	}
	return cfg, store.New(cacheDir), configPath, nil
}

// runSync discovers each account's calendars, syncs each one, then rebuilds
// the derived occurrence index.
//
// A failure on one account is reported but does not abort the others: a broken
// work server should not cost you your personal calendar.
func runSync(cfg *config.Config, s *store.Store, now time.Time, stdout io.Writer) error {
	ctx := context.Background()

	meta := &store.Meta{}
	var failures []error

	// A failed account must not erase its calendars from the derived index:
	// runSync always starts from an empty *store.Meta and only appends
	// accounts that finish the loop body below, so if we did nothing else, a
	// single transient failure (VPN not up yet, a flaky password_cmd, one DNS
	// hiccup) would silently drop that account's events from occurrences.json
	// and meta.json -- even though its cached .ics objects are still on disk
	// untouched. Carrying the previous run's AccountMeta forward on failure
	// keeps the TUI and Waybar showing last-known-good data instead of an
	// empty calendar. Do not "simplify" this away.
	prevAccounts := map[string]store.AccountMeta{}
	if prev, err := s.LoadMeta(); err == nil {
		for _, am := range prev.Accounts {
			prevAccounts[am.Name] = am
		}
	}

	type discoveryResult struct {
		client    *caldav.Client
		calendars []caldav.Resource
		err       error
	}
	type calendarResult struct {
		result calendarsync.Result
		err    error
	}
	type feedResult struct {
		result webcal.Result
		err    error
	}

	// Account discovery and subscription downloads are independent. They share
	// one bounded pool, but separate wait groups let CalDAV collection syncs
	// start as soon as discovery finishes instead of waiting for a slow feed.
	discoveries := make([]discoveryResult, len(cfg.Accounts))
	feedResults := make([]feedResult, len(cfg.WebCals))
	sem := make(chan struct{}, maxConcurrentSyncRequests)
	var discoveryWG, feedWG stdsync.WaitGroup
	for i := range cfg.WebCals {
		feedWG.Add(1)
		go func(i int) {
			defer feedWG.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			result, err := webcal.Sync(ctx, nil, s, cfg.WebCals[i])
			feedResults[i] = feedResult{result: result, err: err}
		}(i)
	}
	for i := range cfg.Accounts {
		discoveryWG.Add(1)
		go func(i int) {
			defer discoveryWG.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			acct := cfg.Accounts[i]
			password, err := acct.Password()
			if err != nil {
				discoveries[i].err = err
				return
			}
			client, err := caldav.NewClient(acct.URL, acct.Username, password, acct.InsecureSkipVerify)
			if err != nil {
				discoveries[i].err = fmt.Errorf("account %q: %w", acct.Name, err)
				return
			}
			calendars, err := client.Discover(ctx)
			if err != nil {
				discoveries[i].err = fmt.Errorf("account %q: %w", acct.Name, err)
				return
			}
			discoveries[i].client = client
			discoveries[i].calendars = calendars
		}(i)
	}
	discoveryWG.Wait()

	// Calendar collections have disjoint cache directories, so their network
	// and disk work can safely overlap. Results stay index-addressed and are
	// reported below in deterministic config/discovery order.
	calendarResults := make([][]calendarResult, len(cfg.Accounts))
	var calendarWG stdsync.WaitGroup
	for i, discovery := range discoveries {
		if discovery.err != nil {
			continue
		}
		calendarResults[i] = make([]calendarResult, len(discovery.calendars))
		for j, cal := range discovery.calendars {
			calendarWG.Add(1)
			go func(i, j int, cal caldav.Resource) {
				defer calendarWG.Done()
				sem <- struct{}{}
				defer func() { <-sem }()
				id := calendarID(cal.Href)
				res, err := calendarsync.Calendar(ctx, discoveries[i].client, s, cfg.Accounts[i].Name, id, cal.Href)
				calendarResults[i][j] = calendarResult{result: res, err: err}
			}(i, j, cal)
		}
	}
	calendarWG.Wait()
	feedWG.Wait()

	for i, acct := range cfg.Accounts {
		discovery := discoveries[i]
		if discovery.err != nil {
			failures = append(failures, discovery.err)
			if old, ok := prevAccounts[acct.Name]; ok {
				meta.Accounts = append(meta.Accounts, old)
				fmt.Fprintf(stdout, "%s: %v -- keeping %d previously synced calendar(s)\n", acct.Name, discovery.err, len(old.Calendars))
			}
			continue
		}

		am := store.AccountMeta{Name: acct.Name}
		calendarFailed := false
		for j, cal := range discovery.calendars {
			id := calendarID(cal.Href)
			// Every discovered calendar is synced. Hiding is a display choice
			// applied when the index is read, so a hidden calendar can be shown
			// again instantly without re-fetching it.
			am.Calendars = append(am.Calendars, store.CalendarMeta{
				ID: id, Path: cal.Href, Name: cal.DisplayName, Color: cal.Color,
			})
			outcome := calendarResults[i][j]
			if outcome.err != nil {
				failures = append(failures, fmt.Errorf("account %q: %w", acct.Name, outcome.err))
				calendarFailed = true
				continue
			}
			if outcome.result.Unchanged {
				fmt.Fprintf(stdout, "%s/%s: unchanged\n", acct.Name, id)
			} else {
				fmt.Fprintf(stdout, "%s/%s: %d fetched, %d deleted\n", acct.Name, id, outcome.result.Fetched, outcome.result.Deleted)
			}
		}
		// Freshness is EARNED by a successful fetch, never by the rebuild that
		// follows it. Only an account that finished discovery AND synced every
		// one of its calendars without error may stamp `now`; anything else
		// keeps the previous run's LastSync, so a cache that stopped updating
		// three days ago still reports as three days old. Stamping `now` here
		// regardless -- or, equivalently, stamping the index with `now` down
		// below -- is exactly the bug this guards: it would make waybar's
		// staleness warning, the only signal a user ever gets that syncing has
		// stopped, permanently unable to fire for the one failure mode that
		// matters (the timer running AND failing).
		if calendarFailed {
			am.LastSync = prevAccounts[acct.Name].LastSync
		} else {
			am.LastSync = now
		}
		meta.Accounts = append(meta.Accounts, am)
	}

	if len(cfg.WebCals) > 0 {
		oldAccount, hadOldAccount := prevAccounts[webcal.AccountName]
		oldCalendars := make(map[string]store.CalendarMeta, len(oldAccount.Calendars))
		for _, cal := range oldAccount.Calendars {
			oldCalendars[cal.ID] = cal
		}

		am := store.AccountMeta{Name: webcal.AccountName, LastSync: now}
		feedFailed := false
		for i, feed := range cfg.WebCals {
			outcome := feedResults[i]
			displayName := outcome.result.Name
			if displayName == "" {
				displayName = oldCalendars[feed.Name].Name
			}
			if displayName == "" {
				displayName = feed.Name
			}
			am.Calendars = append(am.Calendars, store.CalendarMeta{
				ID: feed.Name, Path: feed.URL, Name: displayName, Color: feed.Color,
			})

			if outcome.err != nil {
				feedFailed = true
				wrapped := fmt.Errorf("webcal %q: %w", feed.Name, outcome.err)
				failures = append(failures, wrapped)
				if _, ok := oldCalendars[feed.Name]; ok {
					fmt.Fprintf(stdout, "webcal/%s: %v -- keeping previous data\n", feed.Name, outcome.err)
				} else {
					fmt.Fprintf(stdout, "webcal/%s: %v\n", feed.Name, outcome.err)
				}
				continue
			}
			if outcome.result.Unchanged {
				fmt.Fprintf(stdout, "webcal/%s: unchanged\n", feed.Name)
			} else {
				fmt.Fprintf(stdout, "webcal/%s: %d fetched\n", feed.Name, outcome.result.Events)
			}
		}
		if feedFailed {
			if hadOldAccount {
				am.LastSync = oldAccount.LastSync
			} else {
				am.LastSync = time.Time{}
			}
		}
		meta.Accounts = append(meta.Accounts, am)
	}

	if len(meta.Accounts) == 0 && len(failures) > 0 {
		return fmt.Errorf("every account failed: %w", failures[0])
	}

	// meta.LastSync marks when the run happened, not when data was last
	// fetched: it is a whole-run marker and must never drive freshness.
	meta.LastSync = now
	if err := s.SaveMeta(meta); err != nil {
		return fmt.Errorf("saving metadata: %w", err)
	}

	idx, err := index.Build(s, meta, now, time.Local)
	if err != nil {
		return fmt.Errorf("rebuilding the occurrence index: %w", err)
	}
	if err := s.SaveOccurrences(idx); err != nil {
		return fmt.Errorf("saving the occurrence index: %w", err)
	}
	fmt.Fprintf(stdout, "%d occurrences indexed\n", len(idx.Occurrences))

	if len(failures) > 0 {
		return fmt.Errorf("%d operation(s) failed, first: %w", len(failures), failures[0])
	}
	return nil
}

// runCalendarSync refreshes only the collection whose invitation changed,
// then rebuilds the local occurrence index. It deliberately skips discovery
// and every unrelated account/calendar, which keeps an RSVP response fast.
func runCalendarSync(ctx context.Context, cfg *config.Config, s *store.Store, accountID, calendarID string, now time.Time) error {
	var acct *config.Account
	for i := range cfg.Accounts {
		if cfg.Accounts[i].Name == accountID {
			acct = &cfg.Accounts[i]
			break
		}
	}
	if acct == nil {
		return fmt.Errorf("calendar account %q is not configured", accountID)
	}
	meta, err := s.LoadMeta()
	if err != nil {
		return fmt.Errorf("loading calendar metadata: %w", err)
	}
	calendarPath := ""
	for _, am := range meta.Accounts {
		if am.Name != accountID {
			continue
		}
		for _, cal := range am.Calendars {
			if cal.ID == calendarID {
				calendarPath = cal.Path
				break
			}
		}
	}
	if calendarPath == "" {
		return fmt.Errorf("calendar %q/%q is missing from metadata; run a full sync", accountID, calendarID)
	}

	password, err := acct.Password()
	if err != nil {
		return err
	}
	client, err := caldav.NewClient(acct.URL, acct.Username, password, acct.InsecureSkipVerify)
	if err != nil {
		return fmt.Errorf("account %q: %w", acct.Name, err)
	}
	if _, err := calendarsync.Calendar(ctx, client, s, accountID, calendarID, calendarPath); err != nil {
		return err
	}
	idx, err := index.Build(s, meta, now, time.Local)
	if err != nil {
		return fmt.Errorf("rebuilding the occurrence index: %w", err)
	}
	if err := s.SaveOccurrences(idx); err != nil {
		return fmt.Errorf("saving the occurrence index: %w", err)
	}
	return nil
}

// calendarID derives a stable, filesystem-safe identifier from a collection
// href: the last non-empty path segment.
func calendarID(href string) string {
	trimmed := href
	for len(trimmed) > 0 && trimmed[len(trimmed)-1] == '/' {
		trimmed = trimmed[:len(trimmed)-1]
	}
	for i := len(trimmed) - 1; i >= 0; i-- {
		if trimmed[i] == '/' {
			return trimmed[i+1:]
		}
	}
	if trimmed == "" {
		return "default"
	}
	return trimmed
}
