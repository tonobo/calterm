// Package desktop integrates calterm with desktop-session services.
package desktop

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"github.com/godbus/dbus/v5"
)

const (
	notificationService = "org.freedesktop.Notifications"
	notificationPath    = dbus.ObjectPath("/org/freedesktop/Notifications")
	notificationMethod  = "org.freedesktop.Notifications.Notify"
)

// Notify sends one desktop notification over the standard session-bus
// interface. A sound-theme ID is passed as sound-name, while an absolute path
// is passed as sound-file, leaving lookup, volume, and playback to the desktop
// session. Empty explicitly requests a silent notification.
func Notify(ctx context.Context, summary, body, sound string) error {
	return NotifyWithDuration(ctx, summary, body, sound, time.Second)
}

// NotifyWithDuration sends a notification with an explicit popup lifetime.
// A zero duration requests a persistent notification. Positive values below a
// millisecond are clamped up instead of accidentally becoming persistent.
func NotifyWithDuration(ctx context.Context, summary, body, sound string, duration time.Duration) error {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return fmt.Errorf("connecting to the desktop notification bus: %w", err)
	}
	defer conn.Close()

	hints := notificationHints(sound)
	var id uint32
	call := conn.Object(notificationService, notificationPath).CallWithContext(
		ctx,
		notificationMethod,
		0,
		"calterm",
		uint32(0),
		"x-office-calendar",
		summary,
		body,
		[]string{},
		hints,
		notificationExpireTimeout(duration),
	)
	if err := call.Store(&id); err != nil {
		return fmt.Errorf("sending desktop notification: %w", err)
	}
	return nil
}

func notificationExpireTimeout(duration time.Duration) int32 {
	if duration <= 0 {
		return 0
	}
	milliseconds := duration.Milliseconds()
	if milliseconds == 0 {
		return 1
	}
	const maxInt32 = int64(1<<31 - 1)
	if milliseconds > maxInt32 {
		return int32(maxInt32)
	}
	return int32(milliseconds)
}

func notificationHints(sound string) map[string]dbus.Variant {
	hints := map[string]dbus.Variant{
		"transient": dbus.MakeVariant(true),
		"urgency":   dbus.MakeVariant(byte(1)),
	}
	if sound == "" {
		hints["suppress-sound"] = dbus.MakeVariant(true)
	} else if filepath.IsAbs(sound) {
		hints["sound-file"] = dbus.MakeVariant(sound)
	} else {
		hints["sound-name"] = dbus.MakeVariant(sound)
	}
	return hints
}
