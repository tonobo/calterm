package tui

import (
	"path/filepath"

	"github.com/tonobo/calterm/internal/config"
)

type notificationSoundOption struct {
	Sound      string
	Configured bool
	Installed  bool
}

// notificationSoundOptions combines explicit configuration with sound-theme
// event IDs discovered through the XDG data paths. Configured entries stay at
// the top in their original order; the discovered input is already sorted.
func notificationSoundOptions(cfg config.NotificationsConfig, installed []string) []notificationSoundOption {
	indices := make(map[string]int)
	var options []notificationSoundOption
	for _, calendar := range cfg.Calendars {
		for _, reminder := range calendar.Reminders {
			if reminder.Sound == "" {
				continue
			}
			if index, ok := indices[reminder.Sound]; ok {
				options[index].Configured = true
				continue
			}
			indices[reminder.Sound] = len(options)
			options = append(options, notificationSoundOption{Sound: reminder.Sound, Configured: true})
		}
	}
	for _, sound := range installed {
		if sound == "" {
			continue
		}
		if index, ok := indices[sound]; ok {
			options[index].Installed = true
			continue
		}
		indices[sound] = len(options)
		options = append(options, notificationSoundOption{Sound: sound, Installed: true})
	}
	if len(options) == 0 {
		options = append(options, notificationSoundOption{})
	}
	return options
}

// RenderNotificationSounds draws a picker whose Enter key sends exactly one
// test notification. Keeping tests one-at-a-time makes a broken sound name or
// file distinguishable from the rest of the configured reminders.
func RenderNotificationSounds(options []notificationSoundOption, cursor, width, height int, st Styles) string {
	if width <= 0 || height <= 0 {
		return ""
	}

	header := st.DayHeader.Render("Notification sounds")
	var body []string
	cursorLine := 0
	for i, option := range options {
		if i == cursor {
			cursorLine = len(body)
		}
		body = append(body, renderNotificationSoundRow(option, i == cursor, width, st))
	}

	budget := height - 1 // header
	if budget < 1 {
		budget = 1
	}
	lines := append([]string{header}, scrollWindow(body, cursorLine, budget)...)
	return clampBlock(lines, width, height)
}

func renderNotificationSoundRow(option notificationSoundOption, selected bool, width int, st Styles) string {
	marker := "  "
	style := st.Summary
	if selected {
		marker = cursorMarker + " "
		style = st.Selected
	}

	name := option.Sound
	kind := ""
	if name == "" {
		name = "No sound"
		kind = "notification only"
	} else if filepath.IsAbs(name) {
		kind = "file"
		if option.Configured {
			kind += " · configured"
		}
	} else if option.Configured {
		kind = "configured"
		if !option.Installed {
			kind += " · unavailable"
		}
	}
	if kind != "" {
		kind = "  " + kind
	}
	return truncate(marker+style.Render(name)+st.Dim.Render(kind), width)
}
