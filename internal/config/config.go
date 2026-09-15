package config

import (
	"fmt"
	"net/mail"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

type Config struct {
	Accounts  []Account       `toml:"account"`
	WebCals   []WebCal        `toml:"webcal"`
	Waybar    WaybarConfig    `toml:"waybar"`
	UI        UIConfig        `toml:"ui"`
	Calendars CalendarsConfig `toml:"calendars"`
}

var webcalColorPattern = regexp.MustCompile(`^#[0-9A-Fa-f]{6}([0-9A-Fa-f]{2})?$`)

// WebCal is a read-only iCalendar subscription fetched over HTTPS. Name is a
// stable local ID; the feed's X-WR-CALNAME is used as its display name when
// available.
type WebCal struct {
	Name               string `toml:"name"`
	URL                string `toml:"url"`
	Color              string `toml:"color"`
	Username           string `toml:"username"`
	PasswordCmd        string `toml:"password_cmd"`
	InsecureSkipVerify bool   `toml:"insecure_skip_verify"`
}

type Account struct {
	Name               string `toml:"name"`
	URL                string `toml:"url"`
	Username           string `toml:"username"`
	PasswordCmd        string `toml:"password_cmd"`
	InsecureSkipVerify bool   `toml:"insecure_skip_verify"`
	// Email identifies this user among an event's ATTENDEE properties and is
	// required for direct CalDAV invitation responses.
	Email string `toml:"email"`
}

type WaybarConfig struct {
	LeadTime       time.Duration `toml:"-"`
	NowDuration    time.Duration `toml:"-"`
	StaleAfter     time.Duration `toml:"-"`
	LeadTimeRaw    string        `toml:"lead_time"`
	NowDurationRaw string        `toml:"now_duration"`
	StaleAfterRaw  string        `toml:"stale_after"`
	TextFormat     string        `toml:"text_format"`
	TooltipFormat  string        `toml:"tooltip_format"`

	// TooltipDays is how many days ahead the tooltip's event list looks.
	TooltipDays int `toml:"tooltip_days"`
	// TooltipMax hard-caps the number of events listed in the tooltip.
	TooltipMax int `toml:"tooltip_max"`
	// TooltipHeading is the tooltip's first line. Empty means no heading at
	// all -- not the default -- so it must survive Load() as an explicit "".
	TooltipHeading string `toml:"tooltip_heading"`
	// TooltipDateFmt is the Go time layout used for each day's header line.
	TooltipDateFmt string `toml:"tooltip_date_fmt"`
	// AllDayLabel replaces the hardcoded "all day" text for date-valued events.
	AllDayLabel string `toml:"all_day_label"`
	// TooltipFooter is an optional last line of the tooltip. Empty omits it.
	TooltipFooter string `toml:"tooltip_footer"`
}

type UIConfig struct {
	DefaultView string `toml:"default_view"`
	WeekStart   string `toml:"week_start"`
	// Theme names the active colour theme, e.g. "system:catppuccin-mocha" or
	// "user:my-theme". Empty means no theme configured -- use the default --
	// and is a valid state, not an error.
	Theme string `toml:"theme"`
}

type CalendarsConfig struct {
	Hidden []string `toml:"hidden"`
}

const (
	defaultLeadTime      = 15 * time.Minute
	defaultNowDuration   = 5 * time.Minute
	defaultStaleAfter    = 2 * time.Hour
	defaultTextFormat    = "{start} {summary}"
	defaultTooltipFormat = "{start}–{end} · {summary}"

	defaultTooltipDays    = 7
	defaultTooltipMax     = 10
	defaultTooltipHeading = "Upcoming"
	defaultTooltipDateFmt = "Mon 2 Jan"
	defaultAllDayLabel    = "all day"
)

var validViews = map[string]bool{"agenda": true, "month": true, "week": true, "day": true}

// Load reads and validates the TOML configuration at path.
func Load(path string) (*Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config: %w", err)
	}
	var cfg Config
	// Pre-populate defaults that must be distinguishable from "the user
	// wrote an explicit zero value" (tooltip_days = 0, tooltip_max = 0 must
	// still reach validate() as zero; tooltip_heading = "" must still reach
	// Render() as empty). toml.Unmarshal only touches keys present in the
	// file, so an omitted key keeps the pre-set default and an explicit
	// value -- including a zero one -- overwrites it.
	cfg.Waybar.TooltipDays = defaultTooltipDays
	cfg.Waybar.TooltipMax = defaultTooltipMax
	cfg.Waybar.TooltipHeading = defaultTooltipHeading
	cfg.Waybar.TooltipDateFmt = defaultTooltipDateFmt
	cfg.Waybar.AllDayLabel = defaultAllDayLabel
	if err := toml.Unmarshal(b, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config %s: %w", path, err)
	}
	if err := cfg.applyDefaults(); err != nil {
		return nil, err
	}
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func (c *Config) applyDefaults() error {
	var err error
	if c.Waybar.LeadTime, err = parseDuration(c.Waybar.LeadTimeRaw, defaultLeadTime, "lead_time"); err != nil {
		return err
	}
	if c.Waybar.NowDuration, err = parseDuration(c.Waybar.NowDurationRaw, defaultNowDuration, "now_duration"); err != nil {
		return err
	}
	if c.Waybar.StaleAfter, err = parseDuration(c.Waybar.StaleAfterRaw, defaultStaleAfter, "stale_after"); err != nil {
		return err
	}
	if c.Waybar.TextFormat == "" {
		c.Waybar.TextFormat = defaultTextFormat
	}
	if c.Waybar.TooltipFormat == "" {
		c.Waybar.TooltipFormat = defaultTooltipFormat
	}
	if c.UI.DefaultView == "" {
		c.UI.DefaultView = "agenda"
	}
	if c.UI.WeekStart == "" {
		c.UI.WeekStart = "monday"
	}
	return nil
}

func parseDuration(raw string, def time.Duration, field string) (time.Duration, error) {
	if raw == "" {
		return def, nil
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("waybar.%s: %w", field, err)
	}
	return d, nil
}

func (c *Config) validate() error {
	if len(c.Accounts) == 0 && len(c.WebCals) == 0 {
		return fmt.Errorf("config defines neither [[account]] nor [[webcal]] blocks")
	}
	seen := map[string]bool{}
	for i, a := range c.Accounts {
		switch {
		case a.Name == "":
			return fmt.Errorf("account %d: name is required", i)
		case a.URL == "":
			return fmt.Errorf("account %q: url is required", a.Name)
		case a.Username == "":
			return fmt.Errorf("account %q: username is required", a.Name)
		case a.PasswordCmd == "":
			return fmt.Errorf("account %q: password_cmd is required", a.Name)
		case a.Email != "":
			parsed, err := mail.ParseAddress(a.Email)
			if err != nil || !strings.EqualFold(parsed.Address, a.Email) {
				return fmt.Errorf("account %q: email must be one bare mail address", a.Name)
			}
		}
		if seen[a.Name] {
			return fmt.Errorf("duplicate account name %q", a.Name)
		}
		seen[a.Name] = true
	}
	webcalNames := map[string]bool{}
	for i, feed := range c.WebCals {
		switch {
		case feed.Name == "":
			return fmt.Errorf("webcal %d: name is required", i)
		case feed.URL == "":
			return fmt.Errorf("webcal %q: url is required", feed.Name)
		case webcalNames[feed.Name]:
			return fmt.Errorf("duplicate webcal name %q", feed.Name)
		case (feed.Username == "") != (feed.PasswordCmd == ""):
			return fmt.Errorf("webcal %q: username and password_cmd must be configured together", feed.Name)
		}
		u, err := url.Parse(feed.URL)
		scheme := ""
		if u != nil {
			scheme = strings.ToLower(u.Scheme)
		}
		if err != nil || u == nil || u.Host == "" || (scheme != "https" && scheme != "http" && scheme != "webcal") {
			return fmt.Errorf("webcal %q: url must use https, http, or webcal", feed.Name)
		}
		if feed.Color != "" && !webcalColorPattern.MatchString(feed.Color) {
			return fmt.Errorf("webcal %q: color must be #RRGGBB or #RRGGBBAA", feed.Name)
		}
		webcalNames[feed.Name] = true
	}
	if len(c.WebCals) > 0 && seen["webcal"] {
		return fmt.Errorf("account name %q is reserved when [[webcal]] feeds are configured", "webcal")
	}
	if !validViews[c.UI.DefaultView] {
		return fmt.Errorf("ui.default_view %q: want agenda, month, week, or day", c.UI.DefaultView)
	}
	if c.UI.WeekStart != "monday" && c.UI.WeekStart != "sunday" {
		return fmt.Errorf("ui.week_start %q: want monday or sunday", c.UI.WeekStart)
	}
	if c.Waybar.NowDuration < 0 {
		return fmt.Errorf("waybar.now_duration must be >= 0, got %s", c.Waybar.NowDuration)
	}
	if c.Waybar.TooltipDays < 1 {
		return fmt.Errorf("waybar.tooltip_days must be >= 1, got %d", c.Waybar.TooltipDays)
	}
	if c.Waybar.TooltipMax < 1 {
		return fmt.Errorf("waybar.tooltip_max must be >= 1, got %d", c.Waybar.TooltipMax)
	}
	return nil
}
