package tui

import (
	"context"
	"fmt"
	"image/color"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/tonobo/calterm/internal/config"
	"github.com/tonobo/calterm/internal/model"
	"github.com/tonobo/calterm/internal/rsvp"
	"github.com/tonobo/calterm/internal/store"
)

func testModel(t *testing.T) Model {
	t.Helper()
	cfg := &config.Config{UI: config.UIConfig{DefaultView: "agenda", WeekStart: "monday"}}
	idx := &store.OccurrenceIndex{
		GeneratedAt: time.Date(2026, 6, 10, 7, 0, 0, 0, time.UTC),
		Occurrences: agendaFixture(),
	}
	meta := &store.Meta{Accounts: []store.AccountMeta{{
		Name: "personal", Calendars: []store.CalendarMeta{{ID: "work", Name: "Work"}},
	}}}
	m := New(cfg, nil, idx, meta, time.Date(2026, 6, 10, 8, 0, 0, 0, time.UTC))
	// "" means no user theme directory: the registry gets only the embedded
	// built-ins, deterministically, regardless of what a real user's
	// $XDG_CONFIG_HOME/calterm/themes happens to contain. This is what makes
	// every test built through testModel (the theme picker's tests included)
	// hermetic against the real filesystem.
	m = m.loadThemes("")
	m.loc = time.UTC
	next, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	return next.(Model)
}

func press(t *testing.T, m Model, keys ...string) Model {
	t.Helper()
	for _, k := range keys {
		next, _ := m.Update(tea.KeyPressMsg{Code: rune(k[0]), Text: k})
		m = next.(Model)
	}
	return m
}

func TestModelRendersAgendaByDefault(t *testing.T) {
	m := testModel(t)
	got := m.View().Content
	if !strings.Contains(got, "Standup") {
		t.Errorf("default view should be the agenda:\n%s", got)
	}
}

func TestModelOmitsRedundantTopTitle(t *testing.T) {
	m := testModel(t)
	lines := strings.Split(stripANSI(m.View().Content), "\n")
	if strings.Contains(lines[0], "calterm ·") {
		t.Errorf("first body row still contains the redundant title: %q", lines[0])
	}
	if !strings.Contains(lines[len(lines)-1], "Agenda") {
		t.Errorf("status bar lost the active view: %q", lines[len(lines)-1])
	}
}

func openNotificationSounds(t *testing.T, m Model) Model {
	t.Helper()
	m = press(t, m, " ")
	next, actionCmd := m.Update(tea.KeyPressMsg{Code: 'n', Text: "n"})
	m = next.(Model)
	if actionCmd == nil {
		t.Fatal("<space>n produced no action command")
	}
	next, _ = m.Update(actionCmd())
	return next.(Model)
}

func TestNotificationSoundPickerTestsSelectedConfiguredSound(t *testing.T) {
	old := sendTestNotification
	t.Cleanup(func() { sendTestNotification = old })
	var gotSummary, gotBody, gotSound string
	var gotDuration time.Duration
	sendTestNotification = func(_ context.Context, summary, body, sound string, duration time.Duration) error {
		gotSummary, gotBody, gotSound = summary, body, sound
		gotDuration = duration
		return nil
	}

	m := testModel(t)
	m.cfg.Notifications.Duration = 3 * time.Second
	m.cfg.Notifications.Calendars = []config.NotificationCalendarConfig{{
		Calendar: "personal/work",
		Reminders: []config.NotificationReminderConfig{{
			Before: 5 * time.Minute, Sound: "message-new-instant",
		}},
	}}
	m.installedSounds = []string{"alarm-clock-elapsed", "message-new-instant"}
	m = openNotificationSounds(t, m)
	if m.view != viewNotificationSounds {
		t.Fatalf("view = %v, want notification sound picker", m.view)
	}
	if got := stripANSI(m.View().Content); !strings.Contains(got, "message-new-instant") || !strings.Contains(got, "alarm-clock-elapsed") {
		t.Errorf("sound picker is missing configured or installed sounds:\n%s", got)
	}
	next, notificationCmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = next.(Model)
	if notificationCmd == nil || !m.testingNotification {
		t.Fatal("notification action did not start an asynchronous test")
	}
	next, _ = m.Update(notificationCmd())
	m = next.(Model)

	if gotSummary != "Calterm notification test" || gotBody == "" || gotSound != "message-new-instant" {
		t.Errorf("test notification = %q / %q / %q", gotSummary, gotBody, gotSound)
	}
	if gotDuration != 3*time.Second {
		t.Errorf("test notification duration = %s, want 3s", gotDuration)
	}
	if m.testingNotification || m.statusIsErr || !strings.Contains(m.status, "message-new-instant") {
		t.Errorf("completed test state: testing=%v error=%v status=%q", m.testingNotification, m.statusIsErr, m.status)
	}
}

func TestNotificationSoundOptionsDeduplicateAndMarkSources(t *testing.T) {
	cfg := config.NotificationsConfig{Calendars: []config.NotificationCalendarConfig{
		{Calendar: "personal/one", Reminders: []config.NotificationReminderConfig{
			{Before: 30 * time.Minute},
			{Before: 5 * time.Minute, Sound: "message-new-instant"},
		}},
		{Calendar: "personal/two", Reminders: []config.NotificationReminderConfig{
			{Before: time.Minute, Sound: "message-new-instant"},
			{Before: 0, Sound: "complete"},
		}},
	}}
	got := notificationSoundOptions(cfg, []string{"alarm-clock-elapsed", "message-new-instant"})
	if len(got) != 3 || got[0].Sound != "message-new-instant" || !got[0].Configured || !got[0].Installed ||
		got[1].Sound != "complete" || !got[1].Configured || got[1].Installed ||
		got[2].Sound != "alarm-clock-elapsed" || got[2].Configured || !got[2].Installed {
		t.Fatalf("notificationSoundOptions = %+v", got)
	}
}

func TestNotificationSoundOptionsKeepConfiguredAbsolutePath(t *testing.T) {
	cfg := config.NotificationsConfig{Calendars: []config.NotificationCalendarConfig{{
		Calendar: "personal/one",
		Reminders: []config.NotificationReminderConfig{{
			Before: time.Minute, Sound: "/opt/example/sounds/reminder.oga",
		}},
	}}}
	got := notificationSoundOptions(cfg, nil)
	if len(got) != 1 || got[0].Sound != "/opt/example/sounds/reminder.oga" || !got[0].Configured {
		t.Fatalf("notificationSoundOptions = %+v", got)
	}
}

func TestNotificationTestChordReportsFailure(t *testing.T) {
	old := sendTestNotification
	t.Cleanup(func() { sendTestNotification = old })
	sendTestNotification = func(context.Context, string, string, string, time.Duration) error {
		return fmt.Errorf("desktop unavailable")
	}

	m := testModel(t)
	m = openNotificationSounds(t, m)
	next, notificationCmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = next.(Model)
	next, _ = m.Update(notificationCmd())
	m = next.(Model)
	if !m.statusIsErr || !strings.Contains(m.status, "desktop unavailable") {
		t.Errorf("failed test status: error=%v status=%q", m.statusIsErr, m.status)
	}
}

func TestNewFocusedOpensOccurrenceFromHiddenCalendar(t *testing.T) {
	start := time.Date(2026, 9, 15, 11, 0, 0, 0, time.UTC)
	hidden := model.Occurrence{
		UID: "invite", Start: start, End: start.Add(45 * time.Minute),
		Summary: "Hidden status sync", AccountID: "work", CalendarID: "team",
	}
	visible := model.Occurrence{
		UID: "visible", Start: start.Add(-time.Hour), End: start,
		Summary: "Visible event", AccountID: "work", CalendarID: "personal",
	}
	cfg := &config.Config{
		Accounts:  []config.Account{{Name: "work", Email: "user@example.com"}},
		UI:        config.UIConfig{DefaultView: "agenda", WeekStart: "monday"},
		Calendars: config.CalendarsConfig{Hidden: []string{"work/team"}},
	}
	idx := &store.OccurrenceIndex{GeneratedAt: start, Occurrences: []model.Occurrence{visible, hidden}}
	m := NewFocused(cfg, nil, idx, &store.Meta{}, start, FocusOptions{Occurrence: hidden})

	if m.view != viewDetail || !m.focusDay.Equal(start) {
		t.Fatalf("view=%v focusDay=%v, want detail at %v", m.view, m.focusDay, start)
	}
	if len(m.visible()) != 1 || m.visible()[0].UID != "visible" {
		t.Fatalf("visible occurrences = %+v; hidden calendar was mutated", m.visible())
	}
	selected, ok := m.selected()
	if !ok || selected.UID != "invite" {
		t.Fatalf("selected = %+v, %v", selected, ok)
	}
	if m.cursor != 1 || m.idx.Occurrences[m.cursor].UID != "invite" {
		t.Fatalf("cursor = %d, want raw hidden occurrence index 1", m.cursor)
	}
	next, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	if got := stripANSI(next.(Model).View().Content); !strings.Contains(got, "Hidden status sync") {
		t.Errorf("focused hidden event was not rendered:\n%s", got)
	}
}

func TestNewFocusedReadOnlyOccurrenceDisablesRSVP(t *testing.T) {
	start := time.Date(2026, 9, 15, 11, 0, 0, 0, time.UTC)
	external := model.Occurrence{
		UID: "external", Start: start, End: start.Add(time.Hour), Summary: "External invite",
		AccountID: "accidental", CalendarID: "accidental", AttendeeStatus: "NEEDS-ACTION",
		Attendees: []model.Participant{{Email: "user@example.com", Self: true}},
	}
	cfg := &config.Config{UI: config.UIConfig{DefaultView: "agenda", WeekStart: "monday"}}
	m := NewFocused(cfg, nil, &store.OccurrenceIndex{}, &store.Meta{}, start, FocusOptions{
		Occurrence: external,
		ReadOnly:   true,
		Notice:     "not present in synced calendar",
	})

	selected, ok := m.selected()
	if !ok || selected.AccountID != "" || selected.CalendarID != "" || selected.AttendeeStatus != "" {
		t.Fatalf("read-only selected occurrence retained cache identity: %+v", selected)
	}
	menu := press(t, m, " ")
	if got := stripANSI(menu.View().Content); strings.Contains(got, "+respond") {
		t.Errorf("read-only detail still advertises RSVP actions:\n%s", got)
	}
	m, cmd := m.startRSVP(rsvp.Accepted)
	if cmd != nil || !strings.Contains(m.status, "RSVP unavailable") {
		t.Fatalf("read-only RSVP returned cmd=%v status=%q", cmd != nil, m.status)
	}
	next, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	if got := stripANSI(next.(Model).View().Content); !strings.Contains(got, "not present in synced calendar") {
		t.Errorf("missing read-only notice:\n%s", got)
	}
}

func TestModelQuitReturnsQuitCmd(t *testing.T) {
	m := testModel(t)
	_, cmd := m.Update(tea.KeyPressMsg{Code: 'q', Text: "q"})
	if cmd == nil {
		t.Fatal("q produced no command, want tea.Quit")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Errorf("q produced %T, want tea.QuitMsg", cmd())
	}
}

func TestQBacksOutOfSubviewsBeforeQuitting(t *testing.T) {
	t.Run("detail", func(t *testing.T) {
		m := testModel(t)
		next, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
		m = next.(Model)
		if m.view != viewDetail {
			t.Fatalf("test setup: view = %v, want detail", m.view)
		}
		next, cmd := m.Update(tea.KeyPressMsg{Code: 'q', Text: "q"})
		m = next.(Model)
		if cmd != nil || m.view != viewAgenda {
			t.Fatalf("q from detail returned cmd=%v view=%v, want agenda without quitting", cmd != nil, m.view)
		}
	})

	t.Run("calendar selector nested over detail", func(t *testing.T) {
		m := testModel(t)
		next, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
		m = next.(Model)
		m = press(t, m, "c")
		if m.view != viewCalendars {
			t.Fatalf("test setup: view = %v, want calendars", m.view)
		}
		next, cmd := m.Update(tea.KeyPressMsg{Code: 'q', Text: "q"})
		m = next.(Model)
		if cmd != nil || m.view != viewDetail {
			t.Fatalf("first q returned cmd=%v view=%v, want detail", cmd != nil, m.view)
		}
		next, cmd = m.Update(tea.KeyPressMsg{Code: 'q', Text: "q"})
		m = next.(Model)
		if cmd != nil || m.view != viewAgenda {
			t.Fatalf("second q returned cmd=%v view=%v, want agenda", cmd != nil, m.view)
		}
	})

	t.Run("theme picker", func(t *testing.T) {
		m := testModel(t)
		next, _ := m.runAction("theme")
		m = next.(Model)
		next, cmd := m.Update(tea.KeyPressMsg{Code: 'q', Text: "q"})
		m = next.(Model)
		if cmd != nil || m.view != viewAgenda {
			t.Fatalf("q returned cmd=%v view=%v, want agenda", cmd != nil, m.view)
		}
	})

	for _, tc := range []struct {
		name   string
		key    string
		parent viewKind
	}{
		{name: "day drilled from month", key: "m", parent: viewMonth},
		{name: "day drilled from week", key: "w", parent: viewWeek},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := press(t, testModel(t), tc.key)
			next, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
			m = next.(Model)
			if m.view != viewDay || !m.dayDrilldown {
				t.Fatalf("test setup: view=%v drilldown=%v, want drilled-down day", m.view, m.dayDrilldown)
			}
			next, cmd := m.Update(tea.KeyPressMsg{Code: 'q', Text: "q"})
			m = next.(Model)
			if cmd != nil || m.view != tc.parent || m.dayDrilldown {
				t.Fatalf("q returned cmd=%v view=%v drilldown=%v, want parent %v", cmd != nil, m.view, m.dayDrilldown, tc.parent)
			}
		})
	}
}

func TestEscBacksOutOfDrilledDownDay(t *testing.T) {
	m := press(t, testModel(t), "m")
	next, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = next.(Model)
	next, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = next.(Model)
	if cmd != nil || m.view != viewMonth || m.dayDrilldown {
		t.Fatalf("esc returned cmd=%v view=%v drilldown=%v, want month", cmd != nil, m.view, m.dayDrilldown)
	}
}

func TestQPreservesDayReturnAcrossCalendarSelector(t *testing.T) {
	m := press(t, testModel(t), "w")
	next, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = next.(Model)
	m = press(t, m, "c")
	if m.view != viewCalendars {
		t.Fatalf("test setup: view=%v, want calendars", m.view)
	}
	next, cmd := m.Update(tea.KeyPressMsg{Code: 'q', Text: "q"})
	m = next.(Model)
	if cmd != nil || m.view != viewDay || !m.dayDrilldown {
		t.Fatalf("first q returned cmd=%v view=%v drilldown=%v, want drilled-down day", cmd != nil, m.view, m.dayDrilldown)
	}
	next, cmd = m.Update(tea.KeyPressMsg{Code: 'q', Text: "q"})
	m = next.(Model)
	if cmd != nil || m.view != viewWeek || m.dayDrilldown {
		t.Fatalf("second q returned cmd=%v view=%v drilldown=%v, want week", cmd != nil, m.view, m.dayDrilldown)
	}
}

func TestDirectDayRemainsAMainViewForQ(t *testing.T) {
	m := press(t, testModel(t), "d")
	if m.dayDrilldown {
		t.Fatal("direct d unexpectedly created a drill-down return path")
	}
	_, cmd := m.Update(tea.KeyPressMsg{Code: 'q', Text: "q"})
	if cmd == nil {
		t.Fatal("q on the directly selected day main view should quit")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Errorf("q produced %T, want tea.QuitMsg", cmd())
	}
}

func TestQClearsAppliedFilterBeforeQuitting(t *testing.T) {
	m := testModel(t)
	m.filterQuery = "standup"
	m.filter.SetValue("standup")

	next, cmd := m.Update(tea.KeyPressMsg{Code: 'q', Text: "q"})
	m = next.(Model)
	if cmd != nil || m.filterQuery != "" {
		t.Fatalf("first q returned cmd=%v filter=%q, want cleared filter without quitting", cmd != nil, m.filterQuery)
	}
	next, cmd = m.Update(tea.KeyPressMsg{Code: 'q', Text: "q"})
	if cmd == nil {
		t.Fatal("second q on the main screen should quit")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Errorf("second q produced %T, want tea.QuitMsg", cmd())
	}
}

func TestCtrlCQuitsImmediatelyFromDetail(t *testing.T) {
	m := testModel(t)
	m.view = viewDetail
	next, cmd := m.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	if got := next.(Model).view; got != viewDetail {
		t.Errorf("Ctrl+C navigated to %v before quitting", got)
	}
	if cmd == nil {
		t.Fatal("Ctrl+C produced no quit command")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Errorf("Ctrl+C produced %T, want tea.QuitMsg", cmd())
	}
}

func TestModelCursorMovement(t *testing.T) {
	m := testModel(t)
	if m.cursor != 0 {
		t.Fatalf("initial cursor = %d, want 0", m.cursor)
	}
	m = press(t, m, "j")
	if m.cursor != 1 {
		t.Errorf("after j, cursor = %d, want 1", m.cursor)
	}
	m = press(t, m, "k", "k")
	if m.cursor != 0 {
		t.Errorf("cursor = %d, want it clamped at 0", m.cursor)
	}
}

func TestModelCursorClampsAtEnd(t *testing.T) {
	m := testModel(t)
	for i := 0; i < 20; i++ {
		m = press(t, m, "j")
	}
	if want := len(agendaFixture()) - 1; m.cursor != want {
		t.Errorf("cursor = %d, want it clamped at %d", m.cursor, want)
	}
}

func TestModelSwitchesViews(t *testing.T) {
	m := testModel(t)
	m = press(t, m, "m")
	if m.view != viewMonth {
		t.Errorf("after m, view = %v, want month", m.view)
	}
	m = press(t, m, "a")
	if m.view != viewAgenda {
		t.Errorf("after a, view = %v, want agenda", m.view)
	}
}

func TestMonthNavigationMovesFocusedDate(t *testing.T) {
	m := testModel(t)
	m = press(t, m, "m")
	start := m.focusDay

	m = press(t, m, "l")
	if got := m.focusDay.Sub(start); got != 24*time.Hour {
		t.Errorf("l moved the focus by %v, want one day", got)
	}
	m = press(t, m, "h")
	if !m.focusDay.Equal(start) {
		t.Errorf("h did not undo l: %v vs %v", m.focusDay, start)
	}
	m = press(t, m, "j")
	if got := m.focusDay.Sub(start); got != 7*24*time.Hour {
		t.Errorf("j moved the focus by %v, want one week", got)
	}
}

func TestDayNavigationMovesOneDayAtATime(t *testing.T) {
	m := testModel(t)
	m = press(t, m, "d")
	start := m.focusDay
	m = press(t, m, "j")
	if got := m.focusDay.Sub(start); got != 24*time.Hour {
		t.Errorf("j in the day view moved %v, want one day", got)
	}
}

func TestTodayReturnsToNow(t *testing.T) {
	m := testModel(t)
	m = press(t, m, "m", "l", "l", "j")
	m = press(t, m, "t")
	if !m.focusDay.Equal(m.now) {
		t.Errorf("t left the focus at %v, want %v", m.focusDay, m.now)
	}
}

// Navigation keys must not disturb the agenda cursor, and vice versa.
func TestAgendaNavigationMovesTheCursorNotTheDate(t *testing.T) {
	m := testModel(t)
	start := m.focusDay
	m = press(t, m, "j", "j")
	if m.cursor != 2 {
		t.Errorf("cursor = %d, want 2", m.cursor)
	}
	if !m.focusDay.Equal(start) {
		t.Error("agenda navigation should not move the focused date")
	}
}

func TestEnterFromMonthOpensTheDayView(t *testing.T) {
	m := testModel(t)
	m = press(t, m, "m")
	next, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = next.(Model)
	if m.view != viewDay {
		t.Errorf("Enter in the month grid opened %v, want the day view", m.view)
	}
}

func TestModelViewIsNeverEmpty(t *testing.T) {
	m := testModel(t)
	for _, keys := range [][]string{{}, {"m"}, {"w"}, {"d"}, {"?"}} {
		mm := press(t, m, keys...)
		if strings.TrimSpace(mm.View().Content) == "" {
			t.Errorf("keys %v produced an empty view", keys)
		}
	}
}

// A zero-size terminal arrives before the first WindowSizeMsg. Rendering must
// not panic or divide by zero.
func TestModelRendersBeforeWindowSize(t *testing.T) {
	cfg := &config.Config{UI: config.UIConfig{DefaultView: "agenda", WeekStart: "monday"}}
	m := New(cfg, nil, &store.OccurrenceIndex{}, &store.Meta{}, time.Now())
	if v := m.View(); v.Content == "" {
		t.Error("view before the first WindowSizeMsg is empty")
	}
}

// The status line is the one thing on screen for a user who never presses ?,
// so it should advertise the headline feature rather than bury it behind a
// help key. MINOR 7 in the final review.
func TestStatusLineAdvertisesTheLeader(t *testing.T) {
	m := testModel(t)
	got := m.View().Content
	if !strings.Contains(got, "space keys") {
		t.Errorf("default status hint does not mention the leader:\n%s", got)
	}
}

func TestModelShowsStaleWarning(t *testing.T) {
	cfg := &config.Config{UI: config.UIConfig{DefaultView: "agenda", WeekStart: "monday"}}
	idx := &store.OccurrenceIndex{} // never generated
	m := New(cfg, nil, idx, &store.Meta{}, time.Date(2026, 6, 10, 8, 0, 0, 0, time.UTC))
	next, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	got := next.(Model).View().Content
	if !strings.Contains(strings.ToLower(got), "never synced") {
		t.Errorf("an ungenerated index should be called out:\n%s", got)
	}
}

// pastAndFutureFixture spans both sides of the test clock (2026-06-10 08:00).
func pastAndFutureFixture() []model.Occurrence {
	d := func(day, hour int) time.Time {
		return time.Date(2026, 6, day, hour, 0, 0, 0, time.UTC)
	}
	return []model.Occurrence{
		occ("p1", "Last month retro", d(1, 10), time.Hour),
		occ("p2", "Yesterday 1:1", d(9, 15), 30*time.Minute),
		occ("f1", "Standup", d(10, 9), 30*time.Minute),
		occ("f2", "Design review", d(12, 14), time.Hour),
	}
}

func pastAndFutureModel(t *testing.T) Model {
	t.Helper()
	cfg := &config.Config{UI: config.UIConfig{DefaultView: "agenda", WeekStart: "monday"}}
	idx := &store.OccurrenceIndex{
		GeneratedAt: time.Date(2026, 6, 10, 7, 0, 0, 0, time.UTC),
		Occurrences: pastAndFutureFixture(),
	}
	meta := &store.Meta{Accounts: []store.AccountMeta{{
		Name: "personal", Calendars: []store.CalendarMeta{{ID: "work", Name: "Work"}},
	}}}
	m := New(cfg, nil, idx, meta, time.Date(2026, 6, 10, 8, 0, 0, 0, time.UTC))
	m.loc = time.UTC
	next, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	return next.(Model)
}

// index.DefaultWindow spans now-1month to now+12months, so visible()[0] is the
// OLDEST occurrence and RenderAgenda scrolls to the cursor: a cursor of 0
// opens calterm on last month.
func TestAgendaOpensOnTheFirstNonPastOccurrence(t *testing.T) {
	m := pastAndFutureModel(t)
	if m.cursor != 2 {
		t.Errorf("initial cursor = %d, want 2 (the first occurrence not in the past)", m.cursor)
	}
	if o, ok := m.selected(); !ok || o.Summary != "Standup" {
		t.Errorf("selected = %+v, want Standup", o)
	}
	if !strings.Contains(m.View().Content, "Standup") {
		t.Errorf("the opening view does not show the next event:\n%s", m.View().Content)
	}
}

// "t" is defined as "jump to today". In the agenda it used to reset the cursor
// to 0, jumping FURTHER into the past than the view already opened.
func TestTodayReturnsTheAgendaCursorToTheFirstNonPastOccurrence(t *testing.T) {
	m := pastAndFutureModel(t)
	m = press(t, m, "k", "k") // walk back into last month
	if m.cursor != 0 {
		t.Fatalf("cursor = %d, want 0 after moving up twice", m.cursor)
	}
	m = press(t, m, "t")
	if m.cursor != 2 {
		t.Errorf("after t, cursor = %d, want 2 (today's first event), not a jump further back", m.cursor)
	}
}

// Every occurrence in the past: the cursor lands on the last one rather than
// running off the end.
func TestAgendaWithOnlyPastOccurrencesSelectsTheLast(t *testing.T) {
	cfg := &config.Config{UI: config.UIConfig{DefaultView: "agenda", WeekStart: "monday"}}
	occs := pastAndFutureFixture()[:2]
	idx := &store.OccurrenceIndex{GeneratedAt: time.Date(2026, 6, 10, 7, 0, 0, 0, time.UTC), Occurrences: occs}
	m := New(cfg, nil, idx, &store.Meta{}, time.Date(2026, 6, 10, 8, 0, 0, 0, time.UTC))
	if m.cursor != 1 {
		t.Errorf("cursor = %d, want 1 (the last occurrence) when everything is in the past", m.cursor)
	}
}

// The agenda's cursor must use the same carried-over ordering as its renderer;
// otherwise the vacation is drawn on one row while Enter opens another occurrence
// from the index's original Start order.
func TestAgendaSelectsOngoingMultiDayEventCarriedIntoToday(t *testing.T) {
	loc := time.FixedZone("CEST", 2*60*60)
	now := time.Date(2026, 9, 1, 8, 0, 0, 0, loc)
	occs := []model.Occurrence{
		{UID: "vacation", Summary: "Extended vacation", AllDay: true,
			Start: time.Date(2026, 8, 7, 0, 0, 0, 0, time.UTC),
			End:   time.Date(2026, 9, 3, 0, 0, 0, 0, time.UTC)},
		{UID: "party", Summary: "Summer horse party", AllDay: true,
			Start: time.Date(2026, 8, 29, 0, 0, 0, 0, time.UTC),
			End:   time.Date(2026, 8, 30, 0, 0, 0, 0, time.UTC)},
		occ("casual", "Team coffee", time.Date(2026, 9, 1, 9, 0, 0, 0, loc), 2*time.Hour),
	}
	cfg := &config.Config{UI: config.UIConfig{DefaultView: "agenda", WeekStart: "monday"}}
	m := New(cfg, nil, &store.OccurrenceIndex{GeneratedAt: now, Occurrences: occs}, &store.Meta{}, now)
	m.loc = loc
	// New used time.Local while constructing the initial cursor. Re-run the
	// same action users have available after the location is fixed for this
	// hermetic test.
	next, _ := m.runAction("goto.today")
	m = next.(Model)

	if o, ok := m.selected(); !ok || o.Summary != "Extended vacation" {
		t.Fatalf("selected = %+v, want the ongoing Extended vacation occurrence", o)
	}
	if !strings.Contains(m.render(), "Extended vacation") {
		t.Errorf("opening agenda does not show the ongoing absence:\n%s", m.render())
	}

	m = m.move(0, 1)
	if o, ok := m.selected(); !ok || o.Summary != "Team coffee" {
		t.Errorf("moving down selected %+v, want the next visible agenda row", o)
	}
}

// Week and day navigation moves focusDay and never touches the agenda cursor,
// so Enter used to open viewDetail on visible()[m.cursor] -- typically an
// event a month away from the day the user is looking at.
func TestEnterFromWeekDoesNotOpenAnUnrelatedEvent(t *testing.T) {
	m := pastAndFutureModel(t)
	m = press(t, m, "w")
	m = press(t, m, "l", "l") // move the focused day; the cursor stays put
	next, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = next.(Model)

	if m.view == viewDetail {
		o, _ := m.selected()
		t.Fatalf("Enter from the week view opened the detail of %q (%s), which has nothing to do with the focused day %s",
			o.Summary, o.Start.Format("2006-01-02"), m.focusDay.Format("2006-01-02"))
	}
	if m.view != viewDay {
		t.Errorf("view = %v, want the day view for the focused day", m.view)
	}
}

func TestEnterFromDayViewIsANoOp(t *testing.T) {
	m := pastAndFutureModel(t)
	m = press(t, m, "d")
	next, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = next.(Model)
	if m.view != viewDay {
		t.Errorf("view = %v, want it to stay on the day view", m.view)
	}
}

// The status line advertises "esc to clear", so esc must actually clear an
// applied filter -- Back only ever handled the detail and help views.
func TestEscapeClearsAnAppliedFilter(t *testing.T) {
	m := testModel(t)
	m = press(t, m, "/", "S", "t", "a")
	next, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = next.(Model)
	if m.filterQuery != "Sta" {
		t.Fatalf("filterQuery = %q, want Sta", m.filterQuery)
	}
	if !strings.Contains(m.View().Content, "esc to clear") {
		t.Fatalf("the status line does not advertise esc:\n%s", m.View().Content)
	}

	next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = next.(Model)
	if m.filterQuery != "" {
		t.Errorf("filterQuery = %q after esc, want it cleared", m.filterQuery)
	}
	if len(m.visible()) != len(agendaFixture()) {
		t.Errorf("visible() = %d occurrences, want all %d back", len(m.visible()), len(agendaFixture()))
	}
}

// esc in the detail view must still mean "go back", even with a filter
// applied: leaving the detail comes first.
func TestEscapeInDetailLeavesTheFilterAlone(t *testing.T) {
	m := testModel(t)
	m = press(t, m, "/", "S", "t", "a")
	next, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = next.(Model)
	next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter}) // open the detail
	m = next.(Model)
	if m.view != viewDetail {
		t.Fatalf("view = %v, want the detail view", m.view)
	}
	next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = next.(Model)
	if m.view == viewDetail {
		t.Error("esc did not leave the detail view")
	}
	if m.filterQuery != "Sta" {
		t.Errorf("filterQuery = %q, want the filter still applied after leaving the detail", m.filterQuery)
	}
}

func TestPageKeysMoveTheCursorInTheAgenda(t *testing.T) {
	m := testModel(t)
	m.view = viewAgenda
	m.cursor = 0
	start := m.focusDay

	next, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyPgDown})
	m = next.(Model)
	if m.cursor == 0 {
		t.Error("page down should move the agenda cursor")
	}
	if !m.focusDay.Equal(start) {
		t.Error("paging the agenda must not move the focused date")
	}

	next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyPgUp})
	m = next.(Model)
	if m.cursor != 0 {
		t.Errorf("page up should return to the top, cursor = %d", m.cursor)
	}
}

func TestPageKeysMoveTheDateInCalendarViews(t *testing.T) {
	cases := []struct {
		view viewKind
		want time.Duration
	}{
		{viewMonth, 0}, // months vary in length; checked separately below
		{viewWeek, 7 * 24 * time.Hour},
		{viewDay, 24 * time.Hour},
	}
	for _, tc := range cases {
		m := testModel(t)
		m.view = tc.view
		start := m.focusDay
		startCursor := m.cursor

		next, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyPgDown})
		m = next.(Model)

		if m.cursor != startCursor {
			t.Errorf("view %v: paging must not move the agenda cursor", tc.view)
		}
		if tc.view == viewMonth {
			if m.focusDay.Month() == start.Month() {
				t.Errorf("view month: page down should advance a month, got %v", m.focusDay)
			}
			continue
		}
		if got := m.focusDay.Sub(start); got != tc.want {
			t.Errorf("view %v: paged %v, want %v", tc.view, got, tc.want)
		}
	}
}

func TestPageUpAndDownAreSymmetric(t *testing.T) {
	for _, v := range []viewKind{viewMonth, viewWeek, viewDay} {
		m := testModel(t)
		m.view = v
		start := m.focusDay
		next, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyPgDown})
		m = next.(Model)
		next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyPgUp})
		m = next.(Model)
		if !m.focusDay.Equal(start) {
			t.Errorf("view %v: page down then up left the date at %v, want %v", v, m.focusDay, start)
		}
	}
}

func TestMonthPageDoesNotSkipMonthsOnMonthEndDates(t *testing.T) {
	cases := []struct {
		start    time.Time
		wantNext time.Time // the month after one page down
	}{
		{time.Date(2026, 1, 31, 12, 0, 0, 0, time.UTC), time.Date(2026, 2, 1, 12, 0, 0, 0, time.UTC)},
		{time.Date(2026, 3, 31, 12, 0, 0, 0, time.UTC), time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC)},
		{time.Date(2026, 5, 31, 12, 0, 0, 0, time.UTC), time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)},
	}
	for _, tc := range cases {
		cfg := &config.Config{UI: config.UIConfig{DefaultView: "month", WeekStart: "monday"}}
		idx := &store.OccurrenceIndex{GeneratedAt: time.Now(), Occurrences: []model.Occurrence{}}
		m := New(cfg, nil, idx, &store.Meta{}, time.Now())
		m.view = viewMonth
		m.focusDay = tc.start
		next, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
		m = next.(Model)

		// Page down once
		next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyPgDown})
		m = next.(Model)

		// Must land in the next month (within 3 days for short-month clamping)
		if m.focusDay.Month() != tc.wantNext.Month() {
			t.Errorf("start %v: after page down, month = %v, want %v (skipped a month)",
				tc.start, m.focusDay, tc.wantNext)
		}

		// Page down again — must advance another full month, not skip
		next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyPgDown})
		m = next.(Model)
		expectedMonth := tc.wantNext.Month() + 1
		if int(expectedMonth) > 12 {
			expectedMonth = 1
		}
		if m.focusDay.Month() != expectedMonth {
			t.Errorf("start %v: after two page downs, month = %v, want %v (second advance is wrong)",
				tc.start, m.focusDay.Month(), expectedMonth)
		}
	}
}

func TestVisibleDropsHiddenCalendars(t *testing.T) {
	m := testModel(t)
	all := len(m.visible())
	if all == 0 {
		t.Fatal("fixture has no occurrences")
	}

	// agendaFixture puts every occurrence on calendar "work" with no account,
	// so hiding it should empty the view.
	m.hidden = model.HiddenSet([]string{"work"})
	if got := len(m.visible()); got != 0 {
		t.Errorf("hiding work left %d occurrences, want 0", got)
	}

	m.hidden = model.HiddenSet([]string{"something-else"})
	if got := len(m.visible()); got != all {
		t.Errorf("hiding an unrelated calendar left %d occurrences, want %d", got, all)
	}
}

// Hiding and the / filter must compose rather than one overriding the other.
func TestVisibleCombinesTheFilterAndHiding(t *testing.T) {
	m := testModel(t)
	m.filterQuery = "standup"
	withFilter := len(m.visible())
	if withFilter == 0 {
		t.Fatal("expected the filter to match something")
	}
	m.hidden = model.HiddenSet([]string{"work"})
	if got := len(m.visible()); got != 0 {
		t.Errorf("filter plus hiding left %d occurrences, want 0", got)
	}
}

// dayPaneDateLine is the day pane's unambiguous fingerprint: its date line
// ("Mon 2 January") is a format neither the month grid (bare day numbers)
// nor the week header (short "Mon 2") ever produces. testModel's now is
// 2026-06-10, a Wednesday.
const dayPaneDateLine = "Wed 10 June"

func TestMonthAndWeekUseFullBodyWithoutDuplicateDayPane(t *testing.T) {
	for _, key := range []string{"m", "w"} {
		m := press(t, testModel(t), key)
		got := m.View().Content
		if strings.Contains(got, dayPaneDateLine) {
			t.Errorf("%s view should not repeat the focused day below its grid:\n%s", key, got)
		}
		if lines := strings.Split(got, "\n"); len(lines) != m.height {
			t.Errorf("%s view rendered %d terminal rows, want %d", key, len(lines), m.height)
		}
	}
}

// The day view and the agenda are unchanged: no pane, enter still drills in
// from month/week the same way it always has.
//
// The focused day is moved to one with no fixture events (agendaFixture has
// none on June 20, a Saturday): the agenda only ever emits a day header for
// days that actually have events, so "Sat 20 June" appearing here can only
// come from a pane -- unlike the focused day used elsewhere in this file,
// whose date the agenda legitimately prints as its own (unrelated) day
// header.
func TestDayAndAgendaViewsHaveNoPane(t *testing.T) {
	focus := time.Date(2026, 6, 20, 0, 0, 0, 0, time.UTC)
	for _, view := range []viewKind{viewDay, viewAgenda} {
		m := testModel(t)
		m.view = view
		m.focusDay = focus
		got := m.View().Content
		// The day grid owns its single-day header, so "Sat 20 June" should
		// not appear anywhere when no focused-day pane exists.
		if strings.Contains(got, "Sat 20 June") {
			t.Errorf("view %v should not show a focused-day pane:\n%s", view, got)
		}
	}
}

// A terminal exactly at NarrowWidth used to show the full seven-day grid,
// back when RenderWeek received the raw terminal width. The left-padding
// sweep (bodyLeftPad) narrows what RenderWeek actually receives by one
// column, so without straightening out NarrowWidth to compensate, a
// 90-column terminal would collapse to the single-day view where it never
// used to -- purely a side effect of adding padding, not a real
// readability problem. NarrowWidth's own doc comment still promises "the
// terminal width", so that promise has to hold through the app, not just
// against RenderWeek called directly.
func TestWeekAtNarrowWidthBoundaryShowsFullGridThroughTheApp(t *testing.T) {
	// Deliberately the literal historical boundary (90), not the NarrowWidth
	// constant itself: NarrowWidth is now defined as 90 - bodyLeftPad, so
	// referencing the constant here would make this test track its own fix
	// instead of pinning the concrete terminal width this behaviour is
	// actually about.
	const terminalWidth = 90
	m := testModel(t)
	next, _ := m.Update(tea.WindowSizeMsg{Width: terminalWidth, Height: 24})
	m = next.(Model)
	m = press(t, m, "w")
	got := m.View().Content
	if !strings.Contains(got, "Sun") {
		t.Errorf("a %d-column terminal should still show the full week grid, not collapse to one day:\n%s", terminalWidth, got)
	}
}

// At a range of terminal sizes, every calendar-ish view must never exceed
// the width or the total height it was given through the shared body padding
// and clipping composition.
func TestCalendarViewsRespectWidthAndHeight(t *testing.T) {
	sizes := []struct{ w, h int }{
		{40, 12}, {80, 9}, {80, 24}, {90, 30}, {120, 40}, {200, 60},
	}
	for _, view := range []string{"m", "w", "d", "a"} {
		for _, size := range sizes {
			m := testModel(t)
			next, _ := m.Update(tea.WindowSizeMsg{Width: size.w, Height: size.h})
			m = next.(Model)
			m = press(t, m, view)
			got := m.View().Content
			lines := strings.Split(got, "\n")
			if len(lines) > size.h {
				t.Errorf("%s %dx%d: rendered %d lines, want at most %d", view, size.w, size.h, len(lines), size.h)
			}
			for i, line := range lines {
				if w := lipglossWidth(line); w > size.w {
					t.Errorf("%s %dx%d: line %d is %d cells: %q", view, size.w, size.h, i, w, line)
				}
			}
		}
	}
}

// The status bar must span exactly the terminal width, no more (it must not
// overflow) and no less (its background has to reach the right edge, or the
// "bar" look falls apart) -- in both the normal state AND while filtering.
// Filtering is its own render path (renderStatus's m.filtering branch, not
// the shared style/pad path the normal and error states go through), so it
// needs its own coverage: nothing about the normal-state assertions would
// have caught it being left unstyled.
func TestStatusBarSpansExactlyTheTerminalWidth(t *testing.T) {
	for _, width := range []int{1, 4, 8, 12, 20, 40, 80, 120} {
		for _, filtering := range []bool{false, true} {
			m := testModel(t)
			next, _ := m.Update(tea.WindowSizeMsg{Width: width, Height: 24})
			m = next.(Model)
			if filtering {
				m = press(t, m, "/")
			}
			got := m.View().Content
			lines := strings.Split(got, "\n")
			status := lines[len(lines)-1]
			if w := lipgloss.Width(status); w != width {
				t.Errorf("width %d filtering=%v: status bar is %d cells, want exactly %d: %q", width, filtering, w, width, status)
			}
			if !strings.Contains(status, "48;2;") {
				t.Errorf("width %d filtering=%v: status bar has no background SGR code: %q", width, filtering, status)
			}
		}
	}
}

func TestStatusBarIsPinnedToTerminalBottom(t *testing.T) {
	m := testModel(t)
	m.view = viewDetail
	m.cursor = 0
	next, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 31})
	got := next.(Model).View().Content
	lines := strings.Split(got, "\n")
	if len(lines) != 31 {
		t.Fatalf("rendered %d rows, want terminal height 31", len(lines))
	}
	if !strings.Contains(stripANSI(lines[30]), "Event") {
		t.Errorf("last row is not the status bar: %q", lines[30])
	}
}

func startResponseChord(t *testing.T, m Model, decisionKey string) (Model, tea.Cmd) {
	t.Helper()
	m = press(t, m, " ", "r")
	next, actionCmd := m.Update(tea.KeyPressMsg{Code: rune(decisionKey[0]), Text: decisionKey})
	m = next.(Model)
	if actionCmd == nil {
		t.Fatalf("space r %s did not complete to an action", decisionKey)
	}
	next, responseCmd := m.Update(actionCmd())
	return next.(Model), responseCmd
}

func TestAcceptFromDetailUpdatesInvitationThroughMenu(t *testing.T) {
	m := testModel(t)
	m.view = viewDetail
	m.idx.Occurrences[m.cursor].AttendeeStatus = "NEEDS-ACTION"
	m.cfg.Accounts = []config.Account{{Name: "personal", Email: "user@example.com"}}

	m, cmd := startResponseChord(t, m, "a")
	if cmd == nil {
		t.Fatal("space r a in the detail view produced no response command")
	}
	if !strings.Contains(m.status, "updating invitation as accepted") {
		t.Errorf("status = %q", m.status)
	}
	next, _ := m.Update(cmd())
	m = next.(Model)
	if !m.statusIsErr || !strings.Contains(m.status, "cache is unavailable") {
		t.Errorf("CalDAV update error was not surfaced: status=%q error=%v", m.status, m.statusIsErr)
	}
}

func TestDirectInvitationResponseKeysDoNothing(t *testing.T) {
	for _, direct := range []string{"A", "D"} {
		m := testModel(t)
		m.view = viewDetail
		m.idx.Occurrences[m.cursor].AttendeeStatus = "NEEDS-ACTION"
		m.cfg.Accounts = []config.Account{{Name: "personal", Email: "user@example.com"}}

		next, cmd := m.Update(tea.KeyPressMsg{Code: rune(direct[0]), Text: direct})
		m = next.(Model)
		if cmd != nil || m.syncing {
			t.Errorf("direct %s started an invitation response", direct)
		}
	}
}

func TestInvitationResponseDoesNotOverlapActiveSync(t *testing.T) {
	m := testModel(t)
	m.view = viewDetail
	m.syncing = true
	m.idx.Occurrences[m.cursor].AttendeeStatus = "NEEDS-ACTION"
	m.cfg.Accounts = []config.Account{{Name: "personal", Email: "user@example.com"}}

	m, cmd := startResponseChord(t, m, "a")
	if cmd != nil {
		t.Fatal("response menu started a response while another calendar operation was active")
	}
}

func TestSuccessfulInvitationResponseTriggersSync(t *testing.T) {
	oldRespond, oldSync, oldCalendarSync := respondRSVP, SyncFunc, SyncCalendarFunc
	defer func() { respondRSVP, SyncFunc, SyncCalendarFunc = oldRespond, oldSync, oldCalendarSync }()

	responded, synced := false, false
	respondRSVP = func(context.Context, config.Account, *store.Store, model.Occurrence, rsvp.Decision) error {
		responded = true
		return nil
	}
	SyncFunc = func(context.Context, *config.Config, *store.Store, time.Time) error {
		t.Fatal("RSVP used the global sync instead of the affected calendar")
		return nil
	}
	SyncCalendarFunc = func(_ context.Context, _ *config.Config, s *store.Store, accountID, calendarID string, _ time.Time) error {
		synced = true
		if accountID != "personal" || calendarID != "work" {
			t.Errorf("targeted sync = %s/%s, want personal/work", accountID, calendarID)
		}
		idx := &store.OccurrenceIndex{Occurrences: agendaFixture()}
		idx.Occurrences[0].Attendees = []model.Participant{{Email: "user@example.com", Status: "ACCEPTED"}}
		if err := s.SaveOccurrences(idx); err != nil {
			return err
		}
		return s.SaveMeta(&store.Meta{})
	}

	m := testModel(t)
	m.store = store.New(t.TempDir())
	m.view = viewDetail
	m.idx.Occurrences[m.cursor].AttendeeStatus = "NEEDS-ACTION"
	m.cfg.Accounts = []config.Account{{Name: "personal", Email: "user@example.com"}}
	m, cmd := startResponseChord(t, m, "a")
	if cmd == nil || !m.syncing {
		t.Fatal("accept should start a background update")
	}
	next, _ := m.Update(cmd())
	m = next.(Model)
	if !responded || !synced {
		t.Fatalf("responded=%v synced=%v, want both", responded, synced)
	}
	if m.syncing || m.statusIsErr || !strings.Contains(m.status, "accepted — synced") {
		t.Errorf("status=%q syncing=%v error=%v", m.status, m.syncing, m.statusIsErr)
	}
}

// The status bar's background must actually be set -- Task 6 wires
// StatusBg into the StatusBar style, which BuildStyles used to leave
// foreground-only.
func TestStatusBarHasABackground(t *testing.T) {
	m := testModel(t)
	got := m.renderStatus(80)
	// A background SGR parameter (48;2;r;g;b, true colour) appears
	// somewhere in the escape sequence -- foreground and background are
	// combined into one \x1b[...m run, so it is not necessarily the very
	// first parameter.
	if !strings.Contains(got, "48;2;") {
		t.Errorf("status bar has no background SGR code: %q", got)
	}
}

// The width invariant must hold for every view, at very narrow widths, and
// for the error and filter-query states -- not just the default agenda
// view's happy path TestStatusBarSpansExactlyTheTerminalWidth already
// covers.
func TestStatusBarSpansExactlyTheTerminalWidthAcrossViewsAndStates(t *testing.T) {
	widths := []int{1, 3, 6, 10, 15, 20, 30, 50, 80, 120}
	// Every badge statusBadgeNames knows about, set directly rather than
	// through key presses: viewCalendars ("Calendars") is the widest badge
	// and the one most likely to hit a degrade boundary the shorter view
	// names never reach, and viewDetail/viewThemePicker aren't reachable
	// by a single keypress from the default agenda view at all.
	views := []viewKind{
		viewAgenda, viewMonth, viewWeek, viewDay,
		viewDetail, viewCalendars, viewThemePicker, viewNotificationSounds,
	}
	// overfillStatus is long enough to fill (and exceed) the middle segment
	// at every width in the sweep, including 120 -- it exercises the
	// reserved gap before the sync glyph (renderStatus's gapW) at exactly
	// the boundary where the middle has no slack to spare.
	overfillStatus := "synced -- a status message deliberately long enough to fill the entire middle segment and then some, at any width this sweep tries"
	for _, width := range widths {
		for _, view := range views {
			for _, errState := range []bool{false, true} {
				for _, filterQuery := range []bool{false, true} {
					for _, overfill := range []bool{false, true} {
						m := testModel(t)
						next, _ := m.Update(tea.WindowSizeMsg{Width: width, Height: 24})
						m = next.(Model)
						m.view = view
						m.statusIsErr = errState
						switch {
						case errState:
							m.status = "sync failed: boom"
						case overfill:
							m.status = overfillStatus
						}
						if filterQuery {
							m.filterQuery = "standup"
						}
						got := m.renderStatus(width)
						if w := lipgloss.Width(got); w != width {
							t.Errorf("view=%v width=%d err=%v filterQuery=%v overfill=%v: status bar is %d cells, want exactly %d: %q",
								view, width, errState, filterQuery, overfill, w, width, got)
						}
					}
				}
			}
		}
	}
}

// The view badge names the active view and changes as the view changes,
// including the three badges (Themes, Calendars, Detail) the width sweeps
// above only ever touch incidentally. All Contains checks run against the
// ANSI-stripped text -- SGR parameters are digit-heavy (e.g. "48;2;24") and
// a check against the raw, styled string can false-positive on them, as
// TestStatusBarDegradationOrder's count check originally did.
func TestStatusBarBadgeShowsTheActiveView(t *testing.T) {
	cases := []struct {
		view viewKind
		want string
	}{
		{viewAgenda, "Agenda"},
		{viewMonth, "Month"},
		{viewWeek, "Week"},
		{viewDay, "Day"},
		{viewThemePicker, "Themes"},
		{viewNotificationSounds, "Sounds"},
		{viewCalendars, "Calendars"},
		{viewDetail, "Event"},
	}
	for _, tc := range cases {
		m := testModel(t)
		m.view = tc.view
		got := ansiRE.ReplaceAllString(m.renderStatus(80), "")
		if !strings.Contains(got, tc.want) {
			t.Errorf("view %v: status bar = %q, want it to contain badge %q", tc.view, got, tc.want)
		}
	}
}

func TestStatusBarDescribesContextualQ(t *testing.T) {
	m := testModel(t)
	if got := stripANSI(m.renderStatus(80)); !strings.Contains(got, "q quit") {
		t.Errorf("main status = %q, want q quit", got)
	}
	for _, view := range []viewKind{viewDetail, viewCalendars, viewThemePicker, viewNotificationSounds} {
		m.view = view
		if got := stripANSI(m.renderStatus(80)); !strings.Contains(got, "q back") {
			t.Errorf("view %v status = %q, want q back", view, got)
		}
	}
	m.view = viewAgenda
	m.filterQuery = "meeting"
	if got := stripANSI(m.renderStatus(80)); !strings.Contains(got, "q back") {
		t.Errorf("filtered status = %q, want q back", got)
	}
	m.filterQuery = ""
	m.view = viewDay
	m.dayDrilldown = true
	if got := stripANSI(m.renderStatus(80)); !strings.Contains(got, "q back") {
		t.Errorf("drilled-down day status = %q, want q back", got)
	}
	m.dayDrilldown = false
	m = press(t, m, " ")
	if got := stripANSI(m.renderStatus(80)); !strings.Contains(got, "q back") {
		t.Errorf("popup status = %q, want q back", got)
	}
}

// The count segment mirrors len(m.visible()), including when a filter
// narrows it.
func TestStatusBarCountMatchesVisible(t *testing.T) {
	m := testModel(t)
	want := fmt.Sprintf("%d", len(m.visible()))
	got := ansiRE.ReplaceAllString(m.renderStatus(80), "")
	if !strings.Contains(got, want) {
		t.Errorf("status bar = %q, want it to contain the visible count %q", got, want)
	}

	m.filterQuery = "Standup"
	wantFiltered := fmt.Sprintf("%d", len(m.visible()))
	if wantFiltered == want {
		t.Fatalf("test fixture problem: filtering \"Standup\" did not change the visible count (%s)", want)
	}
	gotFiltered := ansiRE.ReplaceAllString(m.renderStatus(80), "")
	if !strings.Contains(gotFiltered, wantFiltered) {
		t.Errorf("filtered status bar = %q, want it to contain the filtered count %q", gotFiltered, wantFiltered)
	}
}

// oldestAccountLastSync picks the oldest of several accounts' LastSync
// values, not the newest -- an account that never synced (a zero LastSync)
// is the most honest, most extreme case of that and must win outright.
func TestOldestAccountLastSync(t *testing.T) {
	older := time.Date(2026, 6, 9, 8, 0, 0, 0, time.UTC)
	newer := time.Date(2026, 6, 10, 7, 55, 0, 0, time.UTC)
	meta := &store.Meta{Accounts: []store.AccountMeta{
		{Name: "a", LastSync: newer},
		{Name: "b", LastSync: older},
	}}
	got := oldestAccountLastSync(meta)
	if !got.Equal(older) {
		t.Errorf("oldestAccountLastSync = %v, want the older account's %v", got, older)
	}
}

func TestOldestAccountLastSyncNeverSynced(t *testing.T) {
	meta := &store.Meta{}
	got := oldestAccountLastSync(meta)
	if !got.IsZero() {
		t.Errorf("oldestAccountLastSync with no accounts = %v, want zero", got)
	}

	metaOneNeverSynced := &store.Meta{Accounts: []store.AccountMeta{
		{Name: "a", LastSync: time.Date(2026, 6, 9, 8, 0, 0, 0, time.UTC)},
		{Name: "b"}, // never synced
	}}
	got = oldestAccountLastSync(metaOneNeverSynced)
	if !got.IsZero() {
		t.Errorf("oldestAccountLastSync with one never-synced account = %v, want zero (the honest, most-stale answer)", got)
	}
}

// formatSyncAge renders a known duration to a known, compact string.
func TestFormatSyncAge(t *testing.T) {
	cases := []struct {
		d    time.Duration
		want string
	}{
		{30 * time.Second, "<1m"},
		{3 * time.Minute, "3m"},
		{90 * time.Minute, "1h"},
		{25 * time.Hour, "1d"},
	}
	for _, tc := range cases {
		if got := formatSyncAge(tc.d); got != tc.want {
			t.Errorf("formatSyncAge(%v) = %q, want %q", tc.d, got, tc.want)
		}
	}
}

// The sync-age segment renders a known LastSync against a known m.now as a
// known string, colours it with the error foreground once stale, and falls
// back to "never synced" wording when nothing has synced at all -- never
// "↺ 0s". Checked against the ANSI-stripped text; see
// TestStatusBarBadgeShowsTheActiveView's comment for why.
func TestStatusBarSyncAge(t *testing.T) {
	now := time.Date(2026, 6, 10, 8, 3, 0, 0, time.UTC)

	m := testModel(t)
	m.now = now
	m.cfg.Waybar.StaleAfter = time.Hour
	m.meta = &store.Meta{Accounts: []store.AccountMeta{
		{Name: "a", LastSync: now.Add(-3 * time.Minute)},
	}}
	got := ansiRE.ReplaceAllString(m.renderStatus(80), "")
	if !strings.Contains(got, "3m") {
		t.Errorf("fresh sync: status bar = %q, want it to contain the age %q", got, "3m")
	}

	// The oldest account, not the newest, decides staleness.
	m.meta = &store.Meta{Accounts: []store.AccountMeta{
		{Name: "fresh", LastSync: now.Add(-1 * time.Minute)},
		{Name: "stale", LastSync: now.Add(-2 * time.Hour)},
	}}
	got = ansiRE.ReplaceAllString(m.renderStatus(80), "")
	if !strings.Contains(got, "2h") {
		t.Errorf("mixed accounts: status bar = %q, want it to show the oldest account's age (2h)", got)
	}

	// Never synced at all: the model's existing wording, not "↺ 0s".
	m.meta = &store.Meta{}
	got = ansiRE.ReplaceAllString(m.renderStatus(80), "")
	if !strings.Contains(got, "never synced") {
		t.Errorf("never synced: status bar = %q, want it to contain %q", got, "never synced")
	}
	if strings.Contains(got, "↺ 0s") {
		t.Errorf("never synced: status bar = %q must not render as a zero-age duration", got)
	}
}

// A stale sync age is coloured with the error foreground -- staleness is the
// only signal a user gets that their sync timer has died, so the bar must
// not be the place that hides it. This check needs the raw (unstripped)
// output -- it is asserting on the SGR escape sequence itself, not on
// rendered text.
func TestStatusBarSyncAgeColoursErrorWhenStale(t *testing.T) {
	now := time.Date(2026, 6, 10, 8, 0, 0, 0, time.UTC)
	m := testModel(t)
	m.now = now
	m.cfg.Waybar.StaleAfter = time.Hour
	m.meta = &store.Meta{Accounts: []store.AccountMeta{
		{Name: "a", LastSync: now.Add(-2 * time.Hour)},
	}}
	got := m.renderStatus(80)

	errFg, ok := m.styles.Error.GetForeground().(color.RGBA)
	if !ok {
		t.Fatalf("Error style foreground is not a color.RGBA: %T", m.styles.Error.GetForeground())
	}
	wantSGR := fmt.Sprintf("38;2;%d;%d;%d", errFg.R, errFg.G, errFg.B)
	if !strings.Contains(got, wantSGR) {
		t.Errorf("stale sync age: status bar does not carry the error foreground SGR %q:\n%q", wantSGR, got)
	}
}

// At narrow widths, segments degrade in a fixed order -- right cluster
// first, then the count, then the badge -- rather than ever overflowing.
// Every content check here runs against the ANSI-stripped text; see
// TestStatusBarBadgeShowsTheActiveView's comment for why.
func TestStatusBarDegradationOrder(t *testing.T) {
	m := testModel(t)
	m.status = "synced — 4 occurrences, a message long enough to fill the whole bar on its own"

	full := ansiRE.ReplaceAllString(m.renderStatus(80), "")
	if !strings.Contains(full, "space keys") {
		t.Fatalf("width 80: right cluster missing from a full-width bar: %q", full)
	}

	// Narrow enough that the right cluster must be dropped, but the badge
	// and count still fit.
	noRight := ansiRE.ReplaceAllString(m.renderStatus(18), "")
	if strings.Contains(noRight, "space keys") {
		t.Errorf("width 18: right cluster should have been dropped: %q", noRight)
	}
	if !strings.Contains(noRight, "Agenda") {
		t.Errorf("width 18: badge should still be present: %q", noRight)
	}

	// Narrower still: the count goes too, but the badge survives.
	noCount := ansiRE.ReplaceAllString(m.renderStatus(9), "")
	if !strings.Contains(noCount, "Agenda") {
		t.Errorf("width 9: badge should still be present: %q", noCount)
	}
	if strings.Contains(noCount, "4") {
		t.Errorf("width 9: count should have been dropped: %q", noCount)
	}

	// Narrowest: the badge itself goes.
	rawNoBadge := m.renderStatus(3)
	noBadge := ansiRE.ReplaceAllString(rawNoBadge, "")
	if strings.Contains(noBadge, "Agenda") {
		t.Errorf("width 3: badge should have been dropped: %q", noBadge)
	}
	if w := lipgloss.Width(rawNoBadge); w != 3 {
		t.Errorf("width 3: bar is %d cells, want exactly 3: %q", w, rawNoBadge)
	}
}

// A status message that fills the middle exactly must not run straight into
// the sync glyph -- renderStatus reserves one cell between them so
// "...on its own" and "↺ 3m" never collide into "...on its own↺ 3m".
// The width invariant still wins: at the exact boundary where there is no
// slack to spare, the gap is dropped rather than the bar overflowing (see
// TestStatusBarSpansExactlyTheTerminalWidthAcrossViewsAndStates's overfill
// case for that boundary, checked purely on width).
func TestStatusBarReservesAGapBeforeTheSyncSegment(t *testing.T) {
	m := testModel(t)
	m.now = time.Date(2026, 6, 10, 8, 0, 0, 0, time.UTC)
	m.cfg.Waybar.StaleAfter = time.Hour
	m.meta = &store.Meta{Accounts: []store.AccountMeta{
		{Name: "a", LastSync: m.now.Add(-3 * time.Minute)},
	}}
	// Long enough to fill the middle at width 80 with no slack left over.
	m.status = "synced -- a status message deliberately long enough to fill the entire middle segment and then some"

	got := ansiRE.ReplaceAllString(m.renderStatus(80), "")
	if strings.Contains(got, "↺") {
		if idx := strings.Index(got, "↺"); idx == 0 || got[idx-1] != ' ' {
			t.Errorf("sync glyph is not preceded by a separator space: %q", got)
		}
	} else {
		t.Fatalf("test fixture problem: sync glyph missing from a full-width bar: %q", got)
	}
}

// With the redundant top title removed, the status bar retains the active
// month/week context.
func TestStatusBarCarriesThePeriodAndWeekNumber(t *testing.T) {
	m := testModel(t) // now/focusDay = 2026-06-10, a Wednesday, week 24
	cases := []struct {
		key  string
		want string
	}{
		{"m", "June 2026"},
		{"w", "Wk 24"},
	}
	for _, tc := range cases {
		mm := press(t, m, tc.key)
		status := stripANSI(mm.renderStatus(80))
		if !strings.Contains(status, tc.want) {
			t.Errorf("%s view status missing %q: %q", tc.key, tc.want, status)
		}
	}
	// The day grid already renders its own date and Wk.
	dayStatus := stripANSI(press(t, m, "d").renderStatus(80))
	if strings.Contains(dayStatus, "Wk") {
		t.Errorf("day status should carry no duplicate period/Wk: %q", dayStatus)
	}
}

// Every view's body is inset by the same left padding. Only the status bar
// stays flush because its background fills the complete terminal width.
func TestBodyIsLeftPaddedConsistently(t *testing.T) {
	prefix := strings.Repeat(" ", bodyLeftPad)

	renderView := func(view viewKind) string {
		mm := testModel(t)
		mm.view = view
		if view == viewDetail {
			mm.cursor = 0
		}
		return mm.View().Content
	}

	views := []viewKind{viewAgenda, viewMonth, viewWeek, viewDay, viewDetail, viewCalendars, viewThemePicker, viewNotificationSounds}
	for _, v := range views {
		got := renderView(v)
		lines := strings.Split(got, "\n")
		if len(lines) < 3 {
			t.Fatalf("view %v rendered too few lines: %q", v, got)
		}
		// Every body line except the final status bar is inset.
		for i := 0; i < len(lines)-1; i++ {
			if !strings.HasPrefix(lines[i], prefix) {
				t.Errorf("view %v line %d not left-padded by %d: %q", v, i, bodyLeftPad, lines[i])
			}
		}
	}
}
