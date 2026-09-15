package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/tonobo/calterm/internal/config"
	"github.com/tonobo/calterm/internal/model"
	"github.com/tonobo/calterm/internal/store"
)

func testNotificationConfig() *config.Config {
	return &config.Config{
		Notifications: config.NotificationsConfig{
			StaleAfter: 2 * time.Hour,
			Duration:   time.Second,
			Calendars: []config.NotificationCalendarConfig{{
				Calendar: "personal/calendar",
				Reminders: []config.NotificationReminderConfig{{
					Before: 5 * time.Minute, Sound: "message-new-instant",
				}},
			}},
		},
	}
}

func stubDesktopNotifications(t *testing.T, fn func(context.Context, string, string, string, time.Duration) error) {
	t.Helper()
	old := sendDesktopNotification
	sendDesktopNotification = fn
	t.Cleanup(func() { sendDesktopNotification = old })
}

func TestRunNotificationPassDeliversOnceAndPersistsOpaqueState(t *testing.T) {
	now := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	storage := store.New(t.TempDir())
	due := model.Occurrence{
		UID: "private-event-uid", Start: now.Add(5 * time.Minute), End: now.Add(35 * time.Minute),
		Summary: "Planning session", Location: "Example room", AccountID: "personal", CalendarID: "calendar",
	}
	later := due
	later.UID = "later"
	later.Start = now.Add(10 * time.Minute)
	later.End = later.Start.Add(30 * time.Minute)
	if err := storage.SaveOccurrences(&store.OccurrenceIndex{
		GeneratedAt: now, Occurrences: []model.Occurrence{due, later},
	}); err != nil {
		t.Fatal(err)
	}

	var summaries, sounds []string
	var durations []time.Duration
	stubDesktopNotifications(t, func(_ context.Context, summary, _ string, sound string, duration time.Duration) error {
		summaries = append(summaries, summary)
		sounds = append(sounds, sound)
		durations = append(durations, duration)
		return nil
	})

	sent, err := runNotificationPass(context.Background(), testNotificationConfig(), storage, now, time.UTC)
	if err != nil || sent != 1 {
		t.Fatalf("first pass sent=%d err=%v, want 1 nil", sent, err)
	}
	if len(summaries) != 1 || summaries[0] != "Planning session" || sounds[0] != "message-new-instant" {
		t.Fatalf("notification calls: summaries=%v sounds=%v", summaries, sounds)
	}
	if len(durations) != 1 || durations[0] != time.Second {
		t.Fatalf("notification durations = %v, want [1s]", durations)
	}
	sent, err = runNotificationPass(context.Background(), testNotificationConfig(), storage, now.Add(time.Minute), time.UTC)
	if err != nil || sent != 0 || len(summaries) != 1 {
		t.Fatalf("second pass sent=%d calls=%d err=%v, want deduplicated", sent, len(summaries), err)
	}

	stateData, err := os.ReadFile(filepath.Join(storage.Root(), "notifications.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(stateData), due.UID) || strings.Contains(string(stateData), due.Summary) {
		t.Errorf("notification state contains readable event data: %s", stateData)
	}
}

func TestRunNotificationPassDeliversConfiguredThresholdsSeparately(t *testing.T) {
	start := time.Date(2026, 6, 10, 13, 0, 0, 0, time.UTC)
	storage := store.New(t.TempDir())
	event := model.Occurrence{
		UID: "event", Start: start, End: start.Add(30 * time.Minute), Summary: "Example event",
		AccountID: "personal", CalendarID: "calendar",
	}
	if err := storage.SaveOccurrences(&store.OccurrenceIndex{
		GeneratedAt: start.Add(-31 * time.Minute), Occurrences: []model.Occurrence{event},
	}); err != nil {
		t.Fatal(err)
	}
	cfg := testNotificationConfig()
	cfg.Notifications.Calendars[0].Reminders = []config.NotificationReminderConfig{
		{Before: 30 * time.Minute},
		{Before: 5 * time.Minute, Sound: "message-new-instant"},
		{Before: time.Minute, Sound: "message-new-instant"},
	}
	var sounds []string
	stubDesktopNotifications(t, func(_ context.Context, _, _ string, sound string, _ time.Duration) error {
		sounds = append(sounds, sound)
		return nil
	})

	for _, now := range []time.Time{start.Add(-30 * time.Minute), start.Add(-5 * time.Minute), start.Add(-time.Minute)} {
		if sent, err := runNotificationPass(context.Background(), cfg, storage, now, time.UTC); sent != 1 || err != nil {
			t.Fatalf("pass at %v sent=%d err=%v, want 1 nil", now, sent, err)
		}
	}
	if len(sounds) != 3 || sounds[0] != "" || sounds[1] != "message-new-instant" || sounds[2] != "message-new-instant" {
		t.Fatalf("notification sounds = %v", sounds)
	}
}

func TestRunNotificationPassRejectsStaleCache(t *testing.T) {
	now := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	storage := store.New(t.TempDir())
	if err := storage.SaveOccurrences(&store.OccurrenceIndex{
		GeneratedAt: now.Add(-3 * time.Hour),
		Occurrences: []model.Occurrence{{
			UID: "event", Start: now.Add(time.Minute), End: now.Add(time.Hour), Summary: "Example event",
		}},
	}); err != nil {
		t.Fatal(err)
	}
	called := false
	stubDesktopNotifications(t, func(context.Context, string, string, string, time.Duration) error {
		called = true
		return nil
	})

	if _, err := runNotificationPass(context.Background(), testNotificationConfig(), storage, now, time.UTC); err == nil || !strings.Contains(err.Error(), "stale") {
		t.Fatalf("runNotificationPass error = %v, want stale-cache error", err)
	}
	if called {
		t.Fatal("stale cache produced a desktop notification")
	}
}

func TestRunNotificationPassSkipsConfiguredIdentityAfterDecline(t *testing.T) {
	now := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	storage := store.New(t.TempDir())
	event := model.Occurrence{
		UID: "declined", Start: now.Add(5 * time.Minute), End: now.Add(time.Hour), Summary: "Example event", AccountID: "personal", CalendarID: "calendar",
		Attendees: []model.Participant{{Email: "user@example.com", Status: "DECLINED"}},
	}
	if err := storage.SaveOccurrences(&store.OccurrenceIndex{GeneratedAt: now, Occurrences: []model.Occurrence{event}}); err != nil {
		t.Fatal(err)
	}
	cfg := testNotificationConfig()
	cfg.Accounts = []config.Account{{Name: "personal", Email: "user@example.com"}}
	called := false
	stubDesktopNotifications(t, func(context.Context, string, string, string, time.Duration) error {
		called = true
		return nil
	})

	sent, err := runNotificationPass(context.Background(), cfg, storage, now, time.UTC)
	if err != nil || sent != 0 || called {
		t.Fatalf("declined event sent=%d called=%v err=%v", sent, called, err)
	}
}

func TestRunNotificationPassDoesNotWriteIdleState(t *testing.T) {
	now := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	storage := store.New(t.TempDir())
	if err := storage.SaveOccurrences(&store.OccurrenceIndex{GeneratedAt: now}); err != nil {
		t.Fatal(err)
	}
	stubDesktopNotifications(t, func(context.Context, string, string, string, time.Duration) error {
		t.Fatal("empty cache attempted a notification")
		return nil
	})

	if sent, err := runNotificationPass(context.Background(), testNotificationConfig(), storage, now, time.UTC); sent != 0 || err != nil {
		t.Fatalf("empty pass sent=%d err=%v", sent, err)
	}
	if _, err := os.Stat(filepath.Join(storage.Root(), "notifications.json")); !os.IsNotExist(err) {
		t.Fatalf("idle pass wrote notification state: %v", err)
	}
}

func TestRunNotificationPassRetriesFailedDelivery(t *testing.T) {
	now := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	storage := store.New(t.TempDir())
	event := model.Occurrence{
		UID: "event", Start: now.Add(5 * time.Minute), End: now.Add(time.Hour), Summary: "Example event",
		AccountID: "personal", CalendarID: "calendar",
	}
	if err := storage.SaveOccurrences(&store.OccurrenceIndex{GeneratedAt: now, Occurrences: []model.Occurrence{event}}); err != nil {
		t.Fatal(err)
	}
	calls := 0
	stubDesktopNotifications(t, func(context.Context, string, string, string, time.Duration) error {
		calls++
		if calls == 1 {
			return errors.New("desktop unavailable")
		}
		return nil
	})

	if sent, err := runNotificationPass(context.Background(), testNotificationConfig(), storage, now, time.UTC); sent != 0 || err == nil {
		t.Fatalf("failed pass sent=%d err=%v, want 0 and error", sent, err)
	}
	if sent, err := runNotificationPass(context.Background(), testNotificationConfig(), storage, now, time.UTC); sent != 1 || err != nil {
		t.Fatalf("retry sent=%d err=%v, want 1 nil", sent, err)
	}
}

func TestNotificationContent(t *testing.T) {
	loc := time.FixedZone("test", 2*60*60)
	summary, body := notificationContent(model.Occurrence{
		Start: time.Date(2026, 6, 10, 12, 30, 0, 0, time.UTC), Location: "Example room",
	}, loc)
	if summary != "Calendar event" || body != "Wed 10 Jun · 14:30\nExample room" {
		t.Errorf("notification = %q / %q", summary, body)
	}
}

func TestNotificationContentCapsEachFieldAtOneHundredCharacters(t *testing.T) {
	summary, body := notificationContent(model.Occurrence{
		Summary:  strings.Repeat("Calendar ", 20) + "📅",
		Start:    time.Date(2026, 6, 10, 12, 30, 0, 0, time.UTC),
		Location: strings.Repeat("Example room ", 20) + "📍",
	}, time.UTC)

	for name, text := range map[string]string{"summary": summary, "body": body} {
		if !utf8.ValidString(text) {
			t.Errorf("%s is not valid UTF-8: %q", name, text)
		}
		if got := utf8.RuneCountInString(text); got != notificationTextLimit {
			t.Errorf("%s length = %d, want %d", name, got, notificationTextLimit)
		}
		if !strings.HasSuffix(text, "…") {
			t.Errorf("%s = %q, want ellipsis suffix", name, text)
		}
	}
}
