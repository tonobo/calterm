package theme

import (
	"testing"

	"charm.land/lipgloss/v2"
)

// TestDefaultPaletteMatchesLegacy pins the hex values NewStyles(isDark) used
// to hard-code before the theme package existed, field for field. This is
// what proves the default look does not change: the fields covered here are
// exactly Fg, Dim, Accent, Warn, GridLine, and the six calendar slots — the
// only fields that had a pre-existing correspondent. SelectedBg and StatusBg
// are new fields with nothing to pin against; they are exercised only by the
// "no zero Color field" test below.
func TestDefaultPaletteMatchesLegacy(t *testing.T) {
	p := DefaultPalette()

	cases := []struct {
		name        string
		c           Color
		light, dark string
	}{
		{"Fg", p.Fg, "#1e1e2e", "#cdd6f4"},
		{"Dim", p.Dim, "#6c7086", "#7f849c"},
		{"Accent", p.Accent, "#1e66f5", "#89b4fa"},
		{"Warn", p.Warn, "#d20f39", "#f38ba8"},
		{"Success", p.Success, "#40a02b", "#a6e3a1"},
		{"Pending", p.Pending, "#df8e1d", "#f9e2af"},
		{"GridLine", p.GridLine, "#6c7086", "#7f849c"},
	}
	for _, tc := range cases {
		if tc.c.Light != tc.light || tc.c.Dark != tc.dark {
			t.Errorf("%s = {Light:%q Dark:%q}, want {Light:%q Dark:%q}", tc.name, tc.c.Light, tc.c.Dark, tc.light, tc.dark)
		}
	}

	wantCal := [][2]string{
		{"#1e66f5", "#89b4fa"},
		{"#40a02b", "#a6e3a1"},
		{"#df8e1d", "#f9e2af"},
		{"#8839ef", "#cba6f7"},
		{"#179299", "#94e2d5"},
		{"#e64553", "#eba0ac"},
	}
	if len(p.CalendarPalette) != len(wantCal) {
		t.Fatalf("CalendarPalette has %d entries, want %d", len(p.CalendarPalette), len(wantCal))
	}
	for i, w := range wantCal {
		got := p.CalendarPalette[i]
		if got.Light != w[0] || got.Dark != w[1] {
			t.Errorf("CalendarPalette[%d] = {Light:%q Dark:%q}, want {Light:%q Dark:%q}", i, got.Light, got.Dark, w[0], w[1])
		}
	}
}

// TestBuildStylesResolvesColors pins that BuildStyles actually threads the
// palette through into the returned Styles, for both light and dark mode.
func TestBuildStylesResolvesColors(t *testing.T) {
	p := DefaultPalette()

	dark := BuildStyles(p, true)
	if got := dark.Base.GetForeground(); got == nil {
		t.Fatalf("dark Base has no foreground set")
	}

	light := BuildStyles(p, false)
	if got := light.Base.GetForeground(); got == nil {
		t.Fatalf("light Base has no foreground set")
	}

	// The two modes must actually differ -- otherwise isDark was ignored.
	if dark.Base.GetForeground() == light.Base.GetForeground() {
		t.Errorf("Base foreground identical between light and dark mode")
	}
}

// TestBuildStylesRespectsPinnedMode pins that Palette.Mode -- decoded from
// every embedded theme's "mode" key, documented in the design spec -- is
// actually read: a theme pinned to "dark" or "light" must render in that
// mode regardless of what isDark (the terminal's own reported background)
// says, exactly the shape a theme author writing light/dark colour tables
// AND a mode pin would expect. Before this, Mode was decoded and never
// consulted by BuildStyles at all -- harmless only because every shipped
// theme's light/dark colours happen to coincide with its own pinned mode.
func TestBuildStylesRespectsPinnedMode(t *testing.T) {
	base := DefaultPalette()

	darkPinned := base
	darkPinned.Mode = "dark"
	// isDark=false (a light terminal) must still resolve the DARK colour,
	// because the theme is pinned.
	got := BuildStyles(darkPinned, false)
	want := lipgloss.Color(base.Fg.Dark)
	if got.Base.GetForeground() != want {
		t.Errorf("Mode=dark, isDark=false: Base foreground = %v, want the pinned dark colour %v", got.Base.GetForeground(), want)
	}

	lightPinned := base
	lightPinned.Mode = "light"
	// isDark=true (a dark terminal) must still resolve the LIGHT colour.
	got = BuildStyles(lightPinned, true)
	want = lipgloss.Color(base.Fg.Light)
	if got.Base.GetForeground() != want {
		t.Errorf("Mode=light, isDark=true: Base foreground = %v, want the pinned light colour %v", got.Base.GetForeground(), want)
	}

	// Mode="" (adaptive, what every current theme file actually pins to
	// besides catppuccin/solarized) leaves isDark in control, unchanged
	// from before this test existed.
	adaptive := base
	adaptive.Mode = ""
	got = BuildStyles(adaptive, true)
	want = lipgloss.Color(base.Fg.Dark)
	if got.Base.GetForeground() != want {
		t.Errorf("Mode=\"\", isDark=true: Base foreground = %v, want %v (isDark should still decide)", got.Base.GetForeground(), want)
	}
}

// TestBuildStylesSelectedIsForegroundOnly is a regression guard: Selected is
// shared by agenda.go's cursor row, calendars.go's list cursor, and
// whichkey's Key style, none of which pad their text to the row's full
// width. Giving Selected a background would fill only the text, not the
// row -- a ragged, "accidentally highlighted word" look. The full-cell
// inversion the focused day needs lives in SelectedCell instead (below),
// which only renderMonthCell and renderWeekHeader use.
func TestBuildStylesSelectedIsForegroundOnly(t *testing.T) {
	p := DefaultPalette()
	for _, isDark := range []bool{true, false} {
		st := BuildStyles(p, isDark)
		if _, ok := st.Selected.GetBackground().(lipgloss.NoColor); !ok {
			t.Errorf("isDark=%v: Selected has a background set (%v); it must stay foreground-only", isDark, st.Selected.GetBackground())
		}
		if _, ok := st.Selected.GetForeground().(lipgloss.NoColor); ok {
			t.Errorf("isDark=%v: Selected has no foreground set", isDark)
		}
	}
}

// TestBuildStylesSelectedCellHasForegroundAndBackground pins that
// SelectedCell -- the style backing the month/week focused-cell inversion --
// carries both a foreground and a background, in both modes.
func TestBuildStylesSelectedCellHasForegroundAndBackground(t *testing.T) {
	p := DefaultPalette()
	for _, isDark := range []bool{true, false} {
		st := BuildStyles(p, isDark)
		if _, ok := st.SelectedCell.GetBackground().(lipgloss.NoColor); ok {
			t.Errorf("isDark=%v: SelectedCell has no background set", isDark)
		}
		if _, ok := st.SelectedCell.GetForeground().(lipgloss.NoColor); ok {
			t.Errorf("isDark=%v: SelectedCell has no foreground set", isDark)
		}
	}
}

// TestBuildStylesStatusBarHasForegroundAndBackground pins that StatusBar --
// left foreground-only until Task 6 -- now carries StatusBg too, so the
// status line renders as a real bar rather than plain dim text on the
// terminal's own background.
func TestBuildStylesStatusBarHasForegroundAndBackground(t *testing.T) {
	p := DefaultPalette()
	for _, isDark := range []bool{true, false} {
		st := BuildStyles(p, isDark)
		if _, ok := st.StatusBar.GetBackground().(lipgloss.NoColor); ok {
			t.Errorf("isDark=%v: StatusBar has no background set", isDark)
		}
		if _, ok := st.StatusBar.GetForeground().(lipgloss.NoColor); ok {
			t.Errorf("isDark=%v: StatusBar has no foreground set", isDark)
		}
	}
}

// TestBuildStylesCalendarPalette pins that the calendar palette carries
// through and produces distinct, deterministic colours per ID, matching the
// legacy Calendar() behaviour (server colour wins; otherwise a hash-derived
// palette slot).
func TestBuildStylesCalendarPalette(t *testing.T) {
	st := BuildStyles(DefaultPalette(), true)

	a := st.Calendar("cal-a")
	b := st.Calendar("cal-a")
	if a.GetForeground() != b.GetForeground() {
		t.Errorf("Calendar(id) is not deterministic across calls")
	}

	st.SetCalendarColors(map[string]string{"cal-a": "#123456ff"})
	server := st.Calendar("cal-a")
	if server.GetForeground() == a.GetForeground() {
		// Not a strict requirement that they differ, but with this hex they
		// should -- guards against SetCalendarColors being a no-op.
		t.Errorf("SetCalendarColors had no visible effect on Calendar()")
	}
}

// TestCalendarOnZeroValueStylesDoesNotPanic guards theme.go's Calendar
// against dividing by len(s.palette) when the palette is empty. Production
// code can't reach this today -- Palette.validate rejects an empty
// calendar_palette -- but a zero-value Styles (the project's own
// plainStyles() test helper builds one, and the theme picker builds Styles
// from arbitrary user-supplied themes) hits it immediately otherwise.
func TestCalendarOnZeroValueStylesDoesNotPanic(t *testing.T) {
	var st Styles
	got := st.Calendar("some-id")
	if _, ok := got.GetForeground().(lipgloss.NoColor); !ok {
		t.Errorf("Calendar on a zero-value Styles should return a plain, unstyled result; got foreground %v", got.GetForeground())
	}
}

// TestPaletteValidateDoesNotRequireStatusFields is the regression that
// matters: a theme file written before status_key_fg/status_key_bg/
// status_dim_fg existed -- every pre-existing theme -- must still validate.
func TestPaletteValidateDoesNotRequireStatusFields(t *testing.T) {
	p := DefaultPalette()
	p.StatusKeyFg = Color{}
	p.StatusKeyBg = Color{}
	p.StatusDimFg = Color{}
	p.Success = Color{}
	p.Pending = Color{}
	if err := p.validate(); err != nil {
		t.Fatalf("validate() with optional semantic/status fields absent: %v, want nil", err)
	}
}

// TestBuildStylesDerivesStatusColorsWhenAbsent pins the fallback formula: a
// palette that never set the three new fields still gets non-empty badge
// colours, derived from fields that already existed.
func TestBuildStylesDerivesStatusColorsWhenAbsent(t *testing.T) {
	p := DefaultPalette()
	p.StatusKeyFg = Color{}
	p.StatusKeyBg = Color{}
	p.StatusDimFg = Color{}
	p.Success = Color{}
	p.Pending = Color{}

	for _, isDark := range []bool{true, false} {
		st := BuildStyles(p, isDark)
		if _, ok := st.StatusKey.GetForeground().(lipgloss.NoColor); ok {
			t.Errorf("isDark=%v: StatusKey has no derived foreground", isDark)
		}
		if _, ok := st.StatusKey.GetBackground().(lipgloss.NoColor); ok {
			t.Errorf("isDark=%v: StatusKey has no derived background", isDark)
		}
		if _, ok := st.StatusDim.GetForeground().(lipgloss.NoColor); ok {
			t.Errorf("isDark=%v: StatusDim has no derived foreground", isDark)
		}
		if _, ok := st.Success.GetForeground().(lipgloss.NoColor); ok {
			t.Errorf("isDark=%v: Success has no derived foreground", isDark)
		}
		if _, ok := st.Pending.GetForeground().(lipgloss.NoColor); ok {
			t.Errorf("isDark=%v: Pending has no derived foreground", isDark)
		}

		wantKeyFg := lipgloss.Color(p.StatusBg.Resolve(isDark))
		wantKeyBg := lipgloss.Color(p.Accent.Resolve(isDark))
		wantDimFg := lipgloss.Color(p.Dim.Resolve(isDark))
		wantSuccess := lipgloss.Color(p.CalendarPalette[1].Resolve(isDark))
		wantPending := lipgloss.Color(p.CalendarPalette[2].Resolve(isDark))
		if got := st.StatusKey.GetForeground(); got != wantKeyFg {
			t.Errorf("isDark=%v: StatusKey foreground = %v, want derived from StatusBg %v", isDark, got, wantKeyFg)
		}
		if got := st.StatusKey.GetBackground(); got != wantKeyBg {
			t.Errorf("isDark=%v: StatusKey background = %v, want derived from Accent %v", isDark, got, wantKeyBg)
		}
		if got := st.StatusDim.GetForeground(); got != wantDimFg {
			t.Errorf("isDark=%v: StatusDim foreground = %v, want derived from Dim %v", isDark, got, wantDimFg)
		}
		if got := st.Success.GetForeground(); got != wantSuccess {
			t.Errorf("isDark=%v: Success foreground = %v, want %v", isDark, got, wantSuccess)
		}
		if got := st.Pending.GetForeground(); got != wantPending {
			t.Errorf("isDark=%v: Pending foreground = %v, want %v", isDark, got, wantPending)
		}
	}
}

// TestBuildStylesUsesExplicitStatusColorsWhenPresent pins that an explicit
// status_key_fg/status_key_bg/status_dim_fg is honoured rather than always
// re-derived.
func TestBuildStylesUsesExplicitStatusColorsWhenPresent(t *testing.T) {
	p := DefaultPalette()
	p.StatusKeyFg = Color{Light: "#111111", Dark: "#222222"}
	p.StatusKeyBg = Color{Light: "#333333", Dark: "#444444"}
	p.StatusDimFg = Color{Light: "#555555", Dark: "#666666"}

	st := BuildStyles(p, false)
	if got, want := st.StatusKey.GetForeground(), lipgloss.Color("#111111"); got != want {
		t.Errorf("StatusKey foreground = %v, want explicit %v", got, want)
	}
	if got, want := st.StatusKey.GetBackground(), lipgloss.Color("#333333"); got != want {
		t.Errorf("StatusKey background = %v, want explicit %v", got, want)
	}
	if got, want := st.StatusDim.GetForeground(), lipgloss.Color("#555555"); got != want {
		t.Errorf("StatusDim foreground = %v, want explicit %v", got, want)
	}

	st = BuildStyles(p, true)
	if got, want := st.StatusKey.GetForeground(), lipgloss.Color("#222222"); got != want {
		t.Errorf("StatusKey foreground (dark) = %v, want explicit %v", got, want)
	}
}
