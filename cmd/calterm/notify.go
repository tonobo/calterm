package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/tonobo/calterm/internal/config"
	"github.com/tonobo/calterm/internal/desktop"
	"github.com/tonobo/calterm/internal/model"
	"github.com/tonobo/calterm/internal/reminder"
	"github.com/tonobo/calterm/internal/store"
)

var sendDesktopNotification = desktop.NotifyWithDuration

const notificationTextLimit = 100

func notifyCommand(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("notify", flag.ContinueOnError)
	fs.SetOutput(stderr)
	configPath := fs.String("config", "", "configuration file")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 0 {
		fmt.Fprintln(stderr, "calterm: notify does not accept positional arguments")
		return 2
	}

	cfg, storage, _, err := loadEnvironment(*configPath)
	if err != nil {
		fmt.Fprintf(stderr, "calterm: %v\n", err)
		return 1
	}
	if len(cfg.Notifications.Calendars) == 0 {
		return 0
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	sent, err := runNotificationPass(ctx, cfg, storage, time.Now(), time.Local)
	if err != nil {
		fmt.Fprintf(stderr, "calterm: %v\n", err)
		return 1
	}
	if sent > 0 {
		noun := "notifications"
		if sent == 1 {
			noun = "notification"
		}
		fmt.Fprintf(stdout, "sent %d %s\n", sent, noun)
	}
	return 0
}

func runNotificationPass(ctx context.Context, cfg *config.Config, storage *store.Store, now time.Time, loc *time.Location) (int, error) {
	idx, err := storage.LoadOccurrences()
	if err != nil {
		return 0, fmt.Errorf("loading occurrence cache: %w", err)
	}
	if idx.GeneratedAt.IsZero() {
		return 0, errors.New("calendar cache has never been synced")
	}
	if cfg.Notifications.StaleAfter > 0 && idx.Age(now) > cfg.Notifications.StaleAfter {
		return 0, fmt.Errorf("calendar cache is stale (%s old)", idx.Age(now).Round(time.Second))
	}
	markNotificationIdentities(idx, cfg)
	state, err := reminder.LoadState(storage)
	if err != nil {
		return 0, err
	}
	stateChanged := state.Prune(now.Add(-24 * time.Hour))
	due := reminder.Due(
		idx.Occurrences,
		state,
		notificationRules(cfg.Notifications),
		now,
	)
	if loc == nil {
		loc = time.Local
	}

	sent := 0
	var deliveryErrors []error
	for _, delivery := range due {
		summary, body := notificationContent(delivery.Occurrence, loc)
		if err := sendDesktopNotification(ctx, summary, body, delivery.Sound, cfg.Notifications.Duration); err != nil {
			deliveryErrors = append(deliveryErrors, fmt.Errorf("notifying %q: %w", summary, err))
			continue
		}
		state.Mark(delivery.Occurrence, delivery.Before)
		stateChanged = true
		sent++
	}
	if stateChanged {
		if err := state.Save(storage); err != nil {
			return sent, err
		}
	}
	if len(deliveryErrors) > 0 {
		return sent, errors.Join(deliveryErrors...)
	}
	return sent, nil
}

func notificationRules(cfg config.NotificationsConfig) []reminder.Rule {
	var rules []reminder.Rule
	for _, calendar := range cfg.Calendars {
		for _, configured := range calendar.Reminders {
			rules = append(rules, reminder.Rule{
				Calendar: calendar.Calendar,
				Before:   configured.Before,
				Sound:    configured.Sound,
			})
		}
	}
	return rules
}

func markNotificationIdentities(idx *store.OccurrenceIndex, cfg *config.Config) {
	emails := make(map[string]string, len(cfg.Accounts))
	for _, account := range cfg.Accounts {
		emails[account.Name] = account.Email
	}
	for i := range idx.Occurrences {
		idx.Occurrences[i].MarkIdentity(emails[idx.Occurrences[i].AccountID])
	}
}

func notificationContent(occurrence model.Occurrence, loc *time.Location) (string, string) {
	summary := strings.TrimSpace(occurrence.Summary)
	if summary == "" {
		summary = "Calendar event"
	}
	body := occurrence.Start.In(loc).Format("Mon 2 Jan · 15:04")
	if location := strings.TrimSpace(occurrence.Location); location != "" {
		body += "\n" + location
	}
	return truncateNotificationText(summary), truncateNotificationText(body)
}

func truncateNotificationText(text string) string {
	runes := []rune(text)
	if len(runes) <= notificationTextLimit {
		return text
	}
	return string(runes[:notificationTextLimit-1]) + "…"
}
