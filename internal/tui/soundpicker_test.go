package tui

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/tonobo/calterm/internal/config"
)

func TestNotificationSoundPickerLabelsSourcesAndFiles(t *testing.T) {
	m := testModel(t)
	options := []notificationSoundOption{
		{Sound: "message-new-instant", Configured: true, Installed: true},
		{Sound: "/opt/example/sounds/reminder.oga", Configured: true},
		{Sound: "alarm-clock-elapsed", Installed: true},
	}
	got := stripANSI(RenderNotificationSounds(options, 0, 80, 12, m.styles))
	for _, want := range []string{
		"Notification sounds",
		"message-new-instant  configured",
		"/opt/example/sounds/reminder.oga  file · configured",
		"alarm-clock-elapsed",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("sound picker is missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "select") || strings.Contains(got, "enter test") {
		t.Errorf("sound picker contains redundant usage text:\n%s", got)
	}
	for _, redundant := range []string{"sound theme", "installed"} {
		if strings.Contains(got, redundant) {
			t.Errorf("sound picker contains redundant label %q:\n%s", redundant, got)
		}
	}
}

func TestNotificationSoundPickerTestsOnlySelectedRow(t *testing.T) {
	old := sendTestNotification
	t.Cleanup(func() { sendTestNotification = old })
	var sounds []string
	sendTestNotification = func(_ context.Context, _, _, sound string, _ time.Duration) error {
		sounds = append(sounds, sound)
		return nil
	}

	m := testModel(t)
	m.installedSounds = []string{"alarm-clock-elapsed", "complete"}
	m = openNotificationSounds(t, m)
	m = press(t, m, "j")
	next, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = next.(Model)
	if cmd == nil {
		t.Fatal("enter did not start the selected sound test")
	}
	next, _ = m.Update(cmd())
	m = next.(Model)

	if len(sounds) != 1 || sounds[0] != "complete" {
		t.Fatalf("tested sounds = %v, want only complete", sounds)
	}
	if m.view != viewNotificationSounds {
		t.Fatalf("view = %v after test, want sound picker to stay open", m.view)
	}
}

func TestNotificationSoundPickerQAndEscReturnWithoutQuitting(t *testing.T) {
	for _, keyMsg := range []tea.KeyPressMsg{
		{Code: 'q', Text: "q"},
		{Code: tea.KeyEscape},
	} {
		m := openNotificationSounds(t, testModel(t))
		next, cmd := m.Update(keyMsg)
		m = next.(Model)
		if cmd != nil {
			t.Errorf("%s returned a command; it should only go back", keyMsg.String())
		}
		if m.view != viewAgenda {
			t.Errorf("%s left view = %v, want agenda", keyMsg.String(), m.view)
		}
	}
}

func TestNotificationSoundPickerFallsBackToSilentTest(t *testing.T) {
	options := notificationSoundOptions(config.NotificationsConfig{}, nil)
	if len(options) != 1 || options[0].Sound != "" {
		t.Fatalf("notificationSoundOptions = %+v, want one silent test", options)
	}
}
