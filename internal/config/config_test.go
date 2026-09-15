package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadFullConfig(t *testing.T) {
	path := writeConfig(t, `
[[account]]
name = "personal"
url = "https://cloud.example.com/remote.php/dav"
username = "calendar-user"
password_cmd = "pass show caldav/nextcloud"
email = "calendar-user@example.com"

[[webcal]]
name = "waste"
url = "webcal://calendar.example/waste.ics"
color = "#8aadf4"
username = "feed-user"
password_cmd = "pass show webcal/example"

[waybar]
lead_time = "20m"
now_duration = "8m"
stale_after = "3h"
text_format = "{start} {summary}"

[ui]
default_view = "month"
week_start = "sunday"

[calendars]
hidden = ["birthdays"]
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.Accounts) != 1 {
		t.Fatalf("got %d accounts, want 1", len(cfg.Accounts))
	}
	if cfg.Accounts[0].Username != "calendar-user" {
		t.Errorf("username = %q, want calendar-user", cfg.Accounts[0].Username)
	}
	if cfg.Accounts[0].Email != "calendar-user@example.com" {
		t.Errorf("mail identity = %q", cfg.Accounts[0].Email)
	}
	if len(cfg.WebCals) != 1 || cfg.WebCals[0].Name != "waste" || cfg.WebCals[0].Color != "#8aadf4" {
		t.Errorf("webcal feeds = %+v", cfg.WebCals)
	}
	if cfg.WebCals[0].Username != "feed-user" || cfg.WebCals[0].PasswordCmd != "pass show webcal/example" {
		t.Errorf("webcal credentials = %+v", cfg.WebCals[0])
	}
	if cfg.Waybar.LeadTime != 20*time.Minute {
		t.Errorf("lead_time = %v, want 20m", cfg.Waybar.LeadTime)
	}
	if cfg.Waybar.NowDuration != 8*time.Minute {
		t.Errorf("now_duration = %v, want 8m", cfg.Waybar.NowDuration)
	}
	if cfg.Waybar.StaleAfter != 3*time.Hour {
		t.Errorf("stale_after = %v, want 3h", cfg.Waybar.StaleAfter)
	}
	if cfg.UI.DefaultView != "month" {
		t.Errorf("default_view = %q, want month", cfg.UI.DefaultView)
	}
	if len(cfg.Calendars.Hidden) != 1 || cfg.Calendars.Hidden[0] != "birthdays" {
		t.Errorf("hidden = %v, want [birthdays]", cfg.Calendars.Hidden)
	}
}

func TestLoadWebCalOnlyConfig(t *testing.T) {
	path := writeConfig(t, `[[webcal]]
name = "holidays"
url = "HTTPS://calendar.example/holidays.ics"
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.Accounts) != 0 || len(cfg.WebCals) != 1 || cfg.WebCals[0].URL != "HTTPS://calendar.example/holidays.ics" {
		t.Fatalf("config = %+v", cfg)
	}
}

func TestLoadAppliesDefaults(t *testing.T) {
	path := writeConfig(t, `
[[account]]
name = "personal"
url = "https://example.com/dav"
username = "u"
password_cmd = "echo hunter2"
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Waybar.LeadTime != 15*time.Minute {
		t.Errorf("default lead_time = %v, want 15m", cfg.Waybar.LeadTime)
	}
	if cfg.Waybar.NowDuration != 5*time.Minute {
		t.Errorf("default now_duration = %v, want 5m", cfg.Waybar.NowDuration)
	}
	if cfg.Waybar.StaleAfter != 2*time.Hour {
		t.Errorf("default stale_after = %v, want 2h", cfg.Waybar.StaleAfter)
	}
	if cfg.Waybar.TextFormat != "{start} {summary}" {
		t.Errorf("default text_format = %q", cfg.Waybar.TextFormat)
	}
	if cfg.Waybar.TooltipFormat != "{start}–{end} · {summary}" {
		t.Errorf("default tooltip_format = %q, want \"{start}–{end} · {summary}\"", cfg.Waybar.TooltipFormat)
	}
	if cfg.UI.DefaultView != "agenda" {
		t.Errorf("default view = %q, want agenda", cfg.UI.DefaultView)
	}
	if cfg.UI.WeekStart != "monday" {
		t.Errorf("default week_start = %q, want monday", cfg.UI.WeekStart)
	}
	if cfg.Waybar.TooltipDays != 7 {
		t.Errorf("default tooltip_days = %d, want 7", cfg.Waybar.TooltipDays)
	}
	if cfg.Waybar.TooltipMax != 10 {
		t.Errorf("default tooltip_max = %d, want 10", cfg.Waybar.TooltipMax)
	}
	if cfg.Waybar.TooltipHeading != "Upcoming" {
		t.Errorf("default tooltip_heading = %q, want Upcoming", cfg.Waybar.TooltipHeading)
	}
	if cfg.Waybar.TooltipDateFmt != "Mon 2 Jan" {
		t.Errorf("default tooltip_date_fmt = %q, want \"Mon 2 Jan\"", cfg.Waybar.TooltipDateFmt)
	}
	if cfg.Waybar.AllDayLabel != "all day" {
		t.Errorf("default all_day_label = %q, want \"all day\"", cfg.Waybar.AllDayLabel)
	}
	if cfg.Waybar.TooltipFooter != "" {
		t.Errorf("default tooltip_footer = %q, want empty", cfg.Waybar.TooltipFooter)
	}
}

func TestLoadAllowsZeroNowDuration(t *testing.T) {
	path := writeConfig(t, `
[[account]]
name = "personal"
url = "https://example.com/dav"
username = "u"
password_cmd = "echo p"

[waybar]
now_duration = "0s"
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Waybar.NowDuration != 0 {
		t.Errorf("now_duration = %v, want 0", cfg.Waybar.NowDuration)
	}
}

// An explicit empty tooltip_heading means "no heading" and must survive
// loading rather than being clobbered back to the default -- the pattern
// used for tooltip_format (empty means "use the default") does not apply
// here, because empty is itself a meaningful, distinct value.
func TestLoadExplicitEmptyHeadingSurvives(t *testing.T) {
	path := writeConfig(t, `
[[account]]
name = "personal"
url = "https://example.com/dav"
username = "u"
password_cmd = "echo hunter2"

[waybar]
tooltip_heading = ""
tooltip_footer = ""
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Waybar.TooltipHeading != "" {
		t.Errorf("tooltip_heading = %q, want explicit empty to survive", cfg.Waybar.TooltipHeading)
	}
}

func TestLoadTooltipConfig(t *testing.T) {
	path := writeConfig(t, `
[[account]]
name = "personal"
url = "https://example.com/dav"
username = "u"
password_cmd = "echo hunter2"

[waybar]
tooltip_days = 3
tooltip_max = 5
tooltip_heading = "Upcoming events"
tooltip_date_fmt = "02.01."
all_day_label = "full day"
tooltip_footer = "Left click: calendar"
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Waybar.TooltipDays != 3 {
		t.Errorf("tooltip_days = %d, want 3", cfg.Waybar.TooltipDays)
	}
	if cfg.Waybar.TooltipMax != 5 {
		t.Errorf("tooltip_max = %d, want 5", cfg.Waybar.TooltipMax)
	}
	if cfg.Waybar.TooltipHeading != "Upcoming events" {
		t.Errorf("tooltip_heading = %q", cfg.Waybar.TooltipHeading)
	}
	if cfg.Waybar.TooltipDateFmt != "02.01." {
		t.Errorf("tooltip_date_fmt = %q", cfg.Waybar.TooltipDateFmt)
	}
	if cfg.Waybar.AllDayLabel != "full day" {
		t.Errorf("all_day_label = %q", cfg.Waybar.AllDayLabel)
	}
	if cfg.Waybar.TooltipFooter != "Left click: calendar" {
		t.Errorf("tooltip_footer = %q", cfg.Waybar.TooltipFooter)
	}
}

func TestLoadValidationErrors(t *testing.T) {
	tests := []struct{ name, body string }{
		{"no accounts", `[ui]
default_view = "agenda"`},
		{"missing url", `[[account]]
name = "x"
username = "u"
password_cmd = "echo p"`},
		{"missing name", `[[account]]
url = "https://example.com"
username = "u"
password_cmd = "echo p"`},
		{"duplicate names", `[[account]]
name = "a"
url = "https://example.com"
username = "u"
password_cmd = "echo p"

[[account]]
name = "a"
url = "https://example.org"
username = "u"
password_cmd = "echo p"`},
		{"bad view", `[[account]]
name = "a"
url = "https://example.com"
username = "u"
password_cmd = "echo p"

[ui]
default_view = "year"`},
		{"bad duration", `[[account]]
name = "a"
url = "https://example.com"
username = "u"
password_cmd = "echo p"

[waybar]
lead_time = "soon"`},
		{"negative now duration", `[[account]]
name = "a"
url = "https://example.com"
username = "u"
password_cmd = "echo p"

[waybar]
now_duration = "-1m"`},
		{"tooltip_days zero", `[[account]]
name = "a"
url = "https://example.com"
username = "u"
password_cmd = "echo p"

[waybar]
tooltip_days = 0`},
		{"tooltip_max zero", `[[account]]
name = "a"
url = "https://example.com"
username = "u"
password_cmd = "echo p"

[waybar]
tooltip_max = 0`},
		{"display name in email", `[[account]]
name = "a"
url = "https://example.com"
username = "u"
password_cmd = "echo p"
email = "Somebody <somebody@example.com>"`},
		{"webcal missing name", `[[webcal]]
url = "https://calendar.example/feed.ics"`},
		{"webcal missing url", `[[webcal]]
name = "feed"`},
		{"webcal bad scheme", `[[webcal]]
name = "feed"
url = "file:///tmp/feed.ics"`},
		{"webcal bad color", `[[webcal]]
name = "feed"
url = "https://calendar.example/feed.ics"
color = "blue"`},
		{"webcal username without password command", `[[webcal]]
name = "feed"
url = "https://calendar.example/feed.ics"
username = "feed-user"`},
		{"webcal password command without username", `[[webcal]]
name = "feed"
url = "https://calendar.example/feed.ics"
password_cmd = "echo secret"`},
		{"duplicate webcal names", `[[webcal]]
name = "feed"
url = "https://calendar.example/a.ics"

[[webcal]]
name = "feed"
url = "https://calendar.example/b.ics"`},
		{"reserved account name", `[[account]]
name = "webcal"
url = "https://example.com/dav"
username = "u"
password_cmd = "echo p"

[[webcal]]
name = "feed"
url = "https://calendar.example/feed.ics"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := Load(writeConfig(t, tt.body)); err == nil {
				t.Fatal("expected an error, got nil")
			}
		})
	}
}

func TestLoadMissingFile(t *testing.T) {
	_, err := Load(filepath.Join(t.TempDir(), "absent.toml"))
	if err == nil {
		t.Fatal("expected an error for a missing config file")
	}
}
