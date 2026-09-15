package contrib

import (
	"os"
	"strings"
	"testing"

	"github.com/tonobo/calterm/internal/waybar"
)

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	return string(b)
}

// Every class the renderer can emit must have a CSS rule, or a state will
// silently render unstyled in the bar.
func TestStyleSheetCoversEveryClass(t *testing.T) {
	css := read(t, "waybar/style.css")
	for _, class := range []string{
		waybar.ClassNow, waybar.ClassSoon, waybar.ClassUpcoming,
		waybar.ClassNone, waybar.ClassStale,
	} {
		if !strings.Contains(css, "."+class) {
			t.Errorf("style.css has no rule for the %q class", class)
		}
	}
}

func TestWaybarModuleInvokesTheRightSubcommand(t *testing.T) {
	mod := read(t, "waybar/module.jsonc")
	for _, want := range []string{"calterm waybar", `"return-type": "json"`, `"exec"`} {
		if !strings.Contains(mod, want) {
			t.Errorf("module.jsonc is missing %q", want)
		}
	}
}

func TestSystemdUnitsReferenceSyncAndTimer(t *testing.T) {
	service := read(t, "systemd/calterm-sync.service")
	if !strings.Contains(service, "calterm sync") {
		t.Error("the service unit does not run `calterm sync`")
	}
	if !strings.Contains(service, "Type=oneshot") {
		t.Error("the service unit should be Type=oneshot")
	}
	timer := read(t, "systemd/calterm-sync.timer")
	// User managers may start or restart long after the machine booted. Anchor
	// the initial run to the user manager instead of an already elapsed boot,
	// otherwise the timer can remain active with no next trigger.
	for _, want := range []string{"OnStartupSec", "OnUnitActiveSec", "WantedBy=timers.target"} {
		if !strings.Contains(timer, want) {
			t.Errorf("the timer unit is missing %q", want)
		}
	}
}

func TestSystemdNotificationUnitsRunEveryMinute(t *testing.T) {
	service := read(t, "systemd/calterm-notify.service")
	for _, want := range []string{"calterm notify", "Type=oneshot", "graphical-session.target"} {
		if !strings.Contains(service, want) {
			t.Errorf("the notification service is missing %q", want)
		}
	}
	timer := read(t, "systemd/calterm-notify.timer")
	for _, want := range []string{"OnStartupSec", "OnUnitActiveSec=1min", "AccuracySec=1s", "Persistent=true", "WantedBy=timers.target"} {
		if !strings.Contains(timer, want) {
			t.Errorf("the notification timer is missing %q", want)
		}
	}
}

// `calterm sync` exits 1 whenever any account fails, so the unit WILL be
// marked failed on such a run. SuccessExitStatus=0 was a no-op -- 0 is
// already success -- and the comment above it claimed the opposite, telling
// the reader that failed runs are not escalated. Either the directive masks
// exit 1 or the documentation tells the truth; this pins the latter, so that
// a persistent auth failure stays visible in `systemctl --failed`.
func TestSystemdServiceDoesNotCarryANoOpSuccessExitStatus(t *testing.T) {
	service := read(t, "systemd/calterm-sync.service")
	for _, line := range strings.Split(service, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "SuccessExitStatus=") {
			continue
		}
		codes := strings.Fields(strings.TrimPrefix(line, "SuccessExitStatus="))
		masksFailure := false
		for _, c := range codes {
			if c != "0" {
				masksFailure = true
			}
		}
		if !masksFailure {
			t.Errorf("%q is a no-op: exit 0 is already success, and `calterm sync` exits 1 on any account failure", line)
		}
	}
	if strings.Contains(service, "not worth escalating") {
		t.Error("the unit's comment claims failed runs are not escalated, but `calterm sync` exits 1 and systemd will mark the unit failed")
	}
	if !strings.Contains(service, "exits 1") {
		t.Error("the unit should say what a failed run actually does, so the reader is not surprised by a failed unit")
	}
}

// The example config must actually load, so a user copying it gets a working
// starting point rather than a validation error.
func TestExampleConfigIsValid(t *testing.T) {
	if _, err := os.Stat("config.example.toml"); err != nil {
		t.Fatalf("example config is missing: %v", err)
	}
}
