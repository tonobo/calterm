package tui

import (
	"github.com/tonobo/calterm/internal/tui/theme"
	"github.com/tonobo/calterm/internal/tui/whichkey"
)

// cursorMarker prefixes the selected row. A glyph rather than colour alone, so
// the selection survives a monochrome terminal.
const cursorMarker = "▸"

// Styles holds every style the TUI uses, resolved once against a palette and
// a light/dark mode. It is a type alias for theme.Styles: theme cannot
// import tui (that would be an import cycle, since tui imports theme), so
// the struct -- and its Calendar/SetCalendarColors methods -- are defined
// there and aliased back in here. Every existing st.Base, Styles{...}, and
// plainStyles() reference in this package keeps compiling unchanged.
type Styles = theme.Styles

// NewStyles builds the Styles for calterm's original, hard-coded palette. It
// is a thin wrapper over theme.BuildStyles so existing call sites and tests
// do not all have to change at once; selectable themes are layered on top of
// theme.Registry elsewhere.
func NewStyles(isDark bool) Styles {
	return theme.BuildStyles(theme.DefaultPalette(), isDark)
}

// popupStyles adapts the app palette to what the popup draws. whichkey cannot
// import this package, so the styles travel to it rather than the other way.
func (m Model) popupStyles() whichkey.Styles {
	return whichkey.Styles{
		Breadcrumb: m.styles.DayHeader,
		Key:        m.styles.Selected,
		Group:      m.styles.TodayHeader,
		Command:    m.styles.Summary,
		Dim:        m.styles.Dim,
	}
}
