package desktop

import (
	"testing"
	"time"
)

func TestNotificationHintsRequestConfiguredSound(t *testing.T) {
	hints := notificationHints("message-new-instant")
	sound, ok := hints["sound-name"]
	if !ok || sound.Value() != "message-new-instant" {
		t.Fatalf("sound-name hint = %#v", sound.Value())
	}
	if _, ok := hints["suppress-sound"]; ok {
		t.Fatal("configured sound was also suppressed")
	}
}

func TestNotificationHintsSuppressSoundWhenUnset(t *testing.T) {
	hints := notificationHints("")
	suppress, ok := hints["suppress-sound"]
	if !ok || suppress.Value() != true {
		t.Fatalf("suppress-sound hint = %#v", suppress.Value())
	}
	if _, ok := hints["sound-name"]; ok {
		t.Fatal("silent notification carries a sound-name hint")
	}
}

func TestNotificationHintsUseSoundFileForAbsolutePath(t *testing.T) {
	hints := notificationHints("/usr/share/sounds/freedesktop/stereo/complete.oga")
	sound, ok := hints["sound-file"]
	if !ok || sound.Value() != "/usr/share/sounds/freedesktop/stereo/complete.oga" {
		t.Fatalf("sound-file hint = %#v", sound.Value())
	}
	if _, ok := hints["sound-name"]; ok {
		t.Fatal("absolute sound file also carries a sound-name hint")
	}
	if _, ok := hints["suppress-sound"]; ok {
		t.Fatal("absolute sound file was also suppressed")
	}
}

func TestNotificationExpireTimeout(t *testing.T) {
	tests := []struct {
		name     string
		duration time.Duration
		want     int32
	}{
		{name: "persistent", duration: 0, want: 0},
		{name: "sub-millisecond", duration: time.Microsecond, want: 1},
		{name: "default", duration: time.Second, want: 1000},
		{name: "fractional seconds", duration: 1500 * time.Millisecond, want: 1500},
		{name: "overflow", duration: 60 * 24 * time.Hour, want: 1<<31 - 1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := notificationExpireTimeout(test.duration); got != test.want {
				t.Errorf("notificationExpireTimeout(%s) = %d, want %d", test.duration, got, test.want)
			}
		})
	}
}
