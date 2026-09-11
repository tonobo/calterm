package theme

import (
	"hash/fnv"
	"image/color"

	"charm.land/lipgloss/v2"
)

// Styles holds every style the TUI uses, resolved once against a palette and
// a light/dark mode. It lives here rather than in internal/tui because
// BuildStyles must not create an import cycle: internal/tui imports theme,
// so theme cannot import internal/tui back. internal/tui.Styles is a type
// alias for this type, so every existing call site keeps compiling
// unchanged.
type Styles struct {
	Base         lipgloss.Style
	DayHeader    lipgloss.Style
	TodayHeader  lipgloss.Style
	Time         lipgloss.Style
	Summary      lipgloss.Style
	Selected     lipgloss.Style
	SelectedCell lipgloss.Style
	Dim          lipgloss.Style
	StatusBar    lipgloss.Style
	Error        lipgloss.Style
	Success      lipgloss.Style
	Pending      lipgloss.Style
	GridLine     lipgloss.Style
	// StatusKey is the segmented status bar's view badge: foreground and
	// background both set, so it reads as a pill rather than plain text.
	StatusKey lipgloss.Style
	// StatusDim is the segmented status bar's dim cluster -- the count and
	// the right-hand sync age / help hint. Foreground only; callers add
	// their own background so it keeps reading as part of the bar.
	StatusDim lipgloss.Style

	palette []color.Color
	// calendarColors holds server-supplied colours, keyed by calendar ID.
	calendarColors map[string]string
}

// BuildStyles resolves a Palette against a light/dark mode into a Styles.
// The palette supplies colour only; every structural attribute (bold, in
// this case) is decided here, not by the theme file.
func BuildStyles(p Palette, isDark bool) Styles {
	// A theme pinned to a mode overrides the terminal's own reported
	// background: p.Mode == "" (adaptive, what a theme with no "mode" key,
	// or an explicit empty one, decodes to) leaves isDark -- the caller's
	// own signal -- in charge.
	switch p.Mode {
	case "dark":
		isDark = true
	case "light":
		isDark = false
	}

	fg := lipgloss.Color(p.Fg.Resolve(isDark))
	dim := lipgloss.Color(p.Dim.Resolve(isDark))
	accent := lipgloss.Color(p.Accent.Resolve(isDark))
	warn := lipgloss.Color(p.Warn.Resolve(isDark))
	success := lipgloss.Color(p.effectiveSuccess().Resolve(isDark))
	pending := lipgloss.Color(p.effectivePending().Resolve(isDark))
	gridLine := lipgloss.Color(p.GridLine.Resolve(isDark))
	selectedFg := lipgloss.Color(p.SelectedFg.Resolve(isDark))
	selectedBg := lipgloss.Color(p.SelectedBg.Resolve(isDark))
	statusFg := lipgloss.Color(p.StatusFg.Resolve(isDark))
	statusBg := lipgloss.Color(p.StatusBg.Resolve(isDark))
	statusKeyFg := lipgloss.Color(p.effectiveStatusKeyFg().Resolve(isDark))
	statusKeyBg := lipgloss.Color(p.effectiveStatusKeyBg().Resolve(isDark))
	statusDimFg := lipgloss.Color(p.effectiveStatusDimFg().Resolve(isDark))

	pal := make([]color.Color, len(p.CalendarPalette))
	for i, c := range p.CalendarPalette {
		pal[i] = lipgloss.Color(c.Resolve(isDark))
	}

	return Styles{
		Base:         lipgloss.NewStyle().Foreground(fg),
		DayHeader:    lipgloss.NewStyle().Foreground(fg).Bold(true),
		TodayHeader:  lipgloss.NewStyle().Foreground(accent).Bold(true),
		Time:         lipgloss.NewStyle().Foreground(dim),
		Summary:      lipgloss.NewStyle().Foreground(fg),
		Selected:     lipgloss.NewStyle().Foreground(selectedFg).Bold(true),
		SelectedCell: lipgloss.NewStyle().Foreground(selectedFg).Background(selectedBg).Bold(true),
		Dim:          lipgloss.NewStyle().Foreground(dim),
		StatusBar:    lipgloss.NewStyle().Foreground(statusFg).Background(statusBg),
		Error:        lipgloss.NewStyle().Foreground(warn).Bold(true),
		Success:      lipgloss.NewStyle().Foreground(success),
		Pending:      lipgloss.NewStyle().Foreground(pending),
		GridLine:     lipgloss.NewStyle().Foreground(gridLine),
		StatusKey:    lipgloss.NewStyle().Foreground(statusKeyFg).Background(statusKeyBg),
		StatusDim:    lipgloss.NewStyle().Foreground(statusDimFg),
		palette:      pal,

		calendarColors: map[string]string{},
	}
}

// SetCalendarColors records the colours discovery read from the server.
func (s *Styles) SetCalendarColors(colors map[string]string) {
	s.calendarColors = colors
}

// Calendar returns the style for one calendar: the server's own colour when
// it supplied one, otherwise a deterministic palette slot derived from the
// ID, so colours stay stable across runs.
func (s Styles) Calendar(id string) lipgloss.Style {
	if hex, ok := s.calendarColors[id]; ok && len(hex) >= 7 {
		// Servers commonly send #RRGGBBAA; lipgloss wants #RRGGBB.
		return lipgloss.NewStyle().Foreground(lipgloss.Color(hex[:7]))
	}
	// A zero-value Styles (as plainStyles() test helpers build, or a Styles
	// never routed through BuildStyles) has an empty palette. len(s.palette)
	// as a modulus would divide by zero and panic; an unstyled result is the
	// right degraded behaviour instead, matching "a theme problem never
	// stops calterm from rendering."
	if len(s.palette) == 0 {
		return lipgloss.NewStyle()
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(id))
	return lipgloss.NewStyle().Foreground(s.palette[int(h.Sum32())%len(s.palette)])
}

// DefaultPalette reproduces, field for field, the palette NewStyles used to
// hard-code before themes existed. SelectedBg and StatusBg have no
// pre-existing correspondent -- they are new fields -- so their values here
// are simply sensible choices, not a pin of prior behaviour.
func DefaultPalette() Palette {
	return Palette{
		Name: "Default",
		Mode: "",

		Fg:      Color{Light: "#1e1e2e", Dark: "#cdd6f4"},
		Dim:     Color{Light: "#6c7086", Dark: "#7f849c"},
		Accent:  Color{Light: "#1e66f5", Dark: "#89b4fa"},
		Warn:    Color{Light: "#d20f39", Dark: "#f38ba8"},
		Success: Color{Light: "#40a02b", Dark: "#a6e3a1"},
		Pending: Color{Light: "#df8e1d", Dark: "#f9e2af"},

		GridLine: Color{Light: "#6c7086", Dark: "#7f849c"},

		SelectedFg: Color{Light: "#1e66f5", Dark: "#89b4fa"},
		SelectedBg: Color{Light: "#ccd0da", Dark: "#313244"},

		StatusFg: Color{Light: "#6c7086", Dark: "#7f849c"},
		StatusBg: Color{Light: "#e6e9ef", Dark: "#181825"},

		StatusKeyFg: Color{Light: "#e6e9ef", Dark: "#181825"},
		StatusKeyBg: Color{Light: "#1e66f5", Dark: "#89b4fa"},
		StatusDimFg: Color{Light: "#6c7086", Dark: "#7f849c"},

		CalendarPalette: []Color{
			{Light: "#1e66f5", Dark: "#89b4fa"},
			{Light: "#40a02b", Dark: "#a6e3a1"},
			{Light: "#df8e1d", Dark: "#f9e2af"},
			{Light: "#8839ef", Dark: "#cba6f7"},
			{Light: "#179299", Dark: "#94e2d5"},
			{Light: "#e64553", Dark: "#eba0ac"},
		},
	}
}
