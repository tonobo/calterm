// Package theme defines calterm's colour layer: a Palette carries only
// colours, while internal/tui owns every structural attribute (bold,
// padding, margins). Themes are TOML, embedded via go:embed, and
// overridable from a user's config directory.
package theme

import "fmt"

// Color is one palette entry. A theme may give a single value, used for both
// light and dark mode, or a light/dark pair.
//
//	fg = "#ffffff"
//	fg = { light = "#111111", dark = "#eeeeee" }
type Color struct {
	Light string
	Dark  string
}

// UnmarshalTOML implements toml.Unmarshaler so Color accepts both a bare
// string and a {light=, dark=} table.
func (c *Color) UnmarshalTOML(data any) error {
	switch v := data.(type) {
	case string:
		c.Light = v
		c.Dark = v
	case map[string]any:
		light, lok := v["light"].(string)
		dark, dok := v["dark"].(string)
		if !lok || !dok {
			return fmt.Errorf("theme: color table must set both light and dark, got %v", v)
		}
		c.Light = light
		c.Dark = dark
	default:
		return fmt.Errorf("theme: invalid color value %v (%T); want a string or {light=,dark=} table", data, data)
	}
	return nil
}

// Resolve returns the hex value for the requested mode.
func (c Color) Resolve(isDark bool) string {
	if isDark {
		return c.Dark
	}
	return c.Light
}

// isZero reports whether neither mode of this Color was ever set.
func (c Color) isZero() bool {
	return c.Light == "" && c.Dark == ""
}

// Palette is a theme's colours, decoded from one TOML file. Structural
// attributes (bold, padding, ...) never live here -- see BuildStyles.
type Palette struct {
	Name string `toml:"name"`
	Mode string `toml:"mode"` // "dark", "light", or "" for adaptive

	Fg     Color `toml:"fg"`
	Dim    Color `toml:"dim"`
	Accent Color `toml:"accent"`
	Warn   Color `toml:"warn"`
	// Success and Pending are optional semantic colours used for invitation
	// participation states. Older user themes derive them from their calendar
	// palette, so adding these fields does not invalidate existing themes.
	Success Color `toml:"success"`
	Pending Color `toml:"pending"`

	GridLine Color `toml:"grid_line"`

	SelectedFg Color `toml:"selected_fg"`
	SelectedBg Color `toml:"selected_bg"` // the inverted focused cell

	StatusFg Color `toml:"status_fg"`
	StatusBg Color `toml:"status_bg"`

	// StatusKeyFg, StatusKeyBg, and StatusDimFg colour the segmented status
	// bar's view badge and dim cluster (count, sync age, help hint). They
	// are deliberately OPTIONAL -- see effectiveStatusKeyFg and friends --
	// so a theme file written before these fields existed keeps loading
	// unchanged; validate() must never require them.
	StatusKeyFg Color `toml:"status_key_fg"`
	StatusKeyBg Color `toml:"status_key_bg"`
	StatusDimFg Color `toml:"status_dim_fg"`

	// CalendarPalette gives fallback colours for calendars the server did
	// not itself supply a colour for.
	CalendarPalette []Color `toml:"calendar_palette"`
}

// effectiveStatusKeyFg returns StatusKeyFg, deriving it from StatusBg when
// the theme file left it unset.
func (p Palette) effectiveStatusKeyFg() Color {
	if p.StatusKeyFg.isZero() {
		return p.StatusBg
	}
	return p.StatusKeyFg
}

// effectiveStatusKeyBg returns StatusKeyBg, deriving it from Accent when the
// theme file left it unset.
func (p Palette) effectiveStatusKeyBg() Color {
	if p.StatusKeyBg.isZero() {
		return p.Accent
	}
	return p.StatusKeyBg
}

// effectiveStatusDimFg returns StatusDimFg, deriving it from Dim when the
// theme file left it unset.
func (p Palette) effectiveStatusDimFg() Color {
	if p.StatusDimFg.isZero() {
		return p.Dim
	}
	return p.StatusDimFg
}

func (p Palette) effectiveSuccess() Color {
	if !p.Success.isZero() {
		return p.Success
	}
	if len(p.CalendarPalette) > 1 {
		return p.CalendarPalette[1]
	}
	return p.Accent
}

func (p Palette) effectivePending() Color {
	if !p.Pending.isZero() {
		return p.Pending
	}
	if len(p.CalendarPalette) > 2 {
		return p.CalendarPalette[2]
	}
	return p.Accent
}

// validate reports the first missing or incomplete field. A theme file
// that fails validation is skipped by the loader rather than treated as a
// fatal error -- a malformed theme must never stop the TUI starting.
func (p Palette) validate() error {
	fields := map[string]Color{
		"fg":          p.Fg,
		"dim":         p.Dim,
		"accent":      p.Accent,
		"warn":        p.Warn,
		"grid_line":   p.GridLine,
		"selected_fg": p.SelectedFg,
		"selected_bg": p.SelectedBg,
		"status_fg":   p.StatusFg,
		"status_bg":   p.StatusBg,
	}
	for name, c := range fields {
		if c.isZero() {
			return fmt.Errorf("theme: missing or empty field %q", name)
		}
	}
	if len(p.CalendarPalette) == 0 {
		return fmt.Errorf("theme: calendar_palette is empty")
	}
	for i, c := range p.CalendarPalette {
		if c.isZero() {
			return fmt.Errorf("theme: calendar_palette[%d] is empty", i)
		}
	}
	return nil
}
