package theme

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
)

// paletteFields returns every Color field of a Palette, named, for the
// "no zero Color field" sweep. CalendarPalette entries are included with an
// index in the name.
func paletteFields(p Palette) map[string]Color {
	fields := map[string]Color{
		"Fg":          p.Fg,
		"Dim":         p.Dim,
		"Accent":      p.Accent,
		"Warn":        p.Warn,
		"Success":     p.Success,
		"Pending":     p.Pending,
		"GridLine":    p.GridLine,
		"SelectedFg":  p.SelectedFg,
		"SelectedBg":  p.SelectedBg,
		"StatusFg":    p.StatusFg,
		"StatusBg":    p.StatusBg,
		"StatusKeyFg": p.StatusKeyFg,
		"StatusKeyBg": p.StatusKeyBg,
		"StatusDimFg": p.StatusDimFg,
	}
	for i, c := range p.CalendarPalette {
		fields["CalendarPalette"+string(rune('0'+i))] = c
	}
	return fields
}

func isZeroColor(c Color) bool {
	return c.Light == "" && c.Dark == ""
}

// TestEmbeddedThemesLoadComplete is table-driven over Names() so a newly
// added theme file is covered automatically, per the plan.
func TestEmbeddedThemesLoadComplete(t *testing.T) {
	reg, err := LoadRegistry("")
	if err != nil {
		t.Fatalf("LoadRegistry: %v", err)
	}
	names := reg.Names()
	if len(names) == 0 {
		t.Fatalf("LoadRegistry(\"\") registered no themes")
	}

	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			p, ok := reg.Get(name)
			if !ok {
				t.Fatalf("Get(%q) not found, but it was in Names()", name)
			}
			if len(p.CalendarPalette) == 0 {
				t.Errorf("%s: CalendarPalette is empty", name)
			}
			for field, c := range paletteFields(p) {
				if isZeroColor(c) {
					t.Errorf("%s: field %s is a zero Color", name, field)
				}
			}
		})
	}
}

func TestEmbeddedThemesRequireDefault(t *testing.T) {
	reg, err := LoadRegistry("")
	if err != nil {
		t.Fatalf("LoadRegistry: %v", err)
	}
	want := []string{
		"system:default",
		"system:catppuccin-latte",
		"system:catppuccin-frappe",
		"system:catppuccin-macchiato",
		"system:catppuccin-mocha",
		"system:solarized-light",
		"system:solarized-dark",
	}
	for _, w := range want {
		if _, ok := reg.Get(w); !ok {
			t.Errorf("Get(%q) not found among embedded themes", w)
		}
	}
}

func TestNamesSortedAndStable(t *testing.T) {
	reg, err := LoadRegistry("")
	if err != nil {
		t.Fatalf("LoadRegistry: %v", err)
	}
	got := reg.Names()
	want := append([]string(nil), got...)
	sort.Strings(want)
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("Names() not sorted: got %v, want %v", got, want)
		}
	}

	got2 := reg.Names()
	if len(got) != len(got2) {
		t.Fatalf("Names() unstable across calls: %v vs %v", got, got2)
	}
	for i := range got {
		if got[i] != got2[i] {
			t.Fatalf("Names() unstable across calls: %v vs %v", got, got2)
		}
	}
}

func writeTheme(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

const validUserTheme = `
name = "My Theme"
mode = ""

fg = "#111111"
dim = "#222222"
accent = "#333333"
warn = "#444444"
grid_line = "#555555"
selected_fg = "#666666"
selected_bg = "#777777"
status_fg = "#888888"
status_bg = "#999999"
calendar_palette = ["#aaaaaa", "#bbbbbb"]
`

func TestUserThemeRegistersWithPrefix(t *testing.T) {
	dir := t.TempDir()
	writeTheme(t, dir, "mine.toml", validUserTheme)

	reg, err := LoadRegistry(dir)
	if err != nil {
		t.Fatalf("LoadRegistry: %v", err)
	}
	p, ok := reg.Get("user:mine")
	if !ok {
		t.Fatalf("Get(\"user:mine\") not found; Names() = %v", reg.Names())
	}
	if p.Fg.Light != "#111111" {
		t.Errorf("Fg.Light = %q, want #111111", p.Fg.Light)
	}
}

// TestUserThemeWithoutStatusFieldsStillLoads is the regression that matters
// for status_key_fg/status_key_bg/status_dim_fg: validUserTheme predates
// these fields entirely, matching every theme file a user has already
// written. It must still load, and BuildStyles must still produce non-empty
// derived badge colours from it.
func TestUserThemeWithoutStatusFieldsStillLoads(t *testing.T) {
	dir := t.TempDir()
	writeTheme(t, dir, "mine.toml", validUserTheme)

	reg, err := LoadRegistry(dir)
	if err != nil {
		t.Fatalf("LoadRegistry: %v", err)
	}
	p, ok := reg.Get("user:mine")
	if !ok {
		t.Fatalf("Get(\"user:mine\") not found; Names() = %v", reg.Names())
	}
	if !p.StatusKeyFg.isZero() || !p.StatusKeyBg.isZero() || !p.StatusDimFg.isZero() ||
		!p.Success.isZero() || !p.Pending.isZero() {
		t.Fatalf("validUserTheme fixture unexpectedly sets a newer optional colour field")
	}

	st := BuildStyles(p, true)
	if _, ok := st.StatusKey.GetForeground().(lipgloss.NoColor); ok {
		t.Errorf("StatusKey has no derived foreground for a theme predating status_key_fg")
	}
	if _, ok := st.StatusKey.GetBackground().(lipgloss.NoColor); ok {
		t.Errorf("StatusKey has no derived background for a theme predating status_key_bg")
	}
	if _, ok := st.StatusDim.GetForeground().(lipgloss.NoColor); ok {
		t.Errorf("StatusDim has no derived foreground for a theme predating status_dim_fg")
	}
	if _, ok := st.Success.GetForeground().(lipgloss.NoColor); ok {
		t.Errorf("Success has no derived foreground for a theme predating success")
	}
	if _, ok := st.Pending.GetForeground().(lipgloss.NoColor); ok {
		t.Errorf("Pending has no derived foreground for a theme predating pending")
	}
}

func TestUserThemeShadowsBuiltinBareName(t *testing.T) {
	dir := t.TempDir()
	writeTheme(t, dir, "default.toml", validUserTheme)

	reg, err := LoadRegistry(dir)
	if err != nil {
		t.Fatalf("LoadRegistry: %v", err)
	}

	// Both canonical entries must stay registered and listed: shadowing is
	// about which one bare-name resolution prefers, not about deleting the
	// built-in. The spec's "falls back to system:default" guarantee depends
	// on system:default staying reachable even when a user default.toml
	// exists (and might later go bad).
	names := reg.Names()
	if !containsAll(names, "system:default", "user:default") {
		t.Fatalf("Names() = %v, want both system:default and user:default", names)
	}

	sysP, ok := reg.Get("system:default")
	if !ok {
		t.Fatalf("Get(\"system:default\") not found; a user theme must shadow, not delete, the built-in")
	}
	if sysP.Fg.Light != "#1e1e2e" {
		t.Errorf("system:default: Fg.Light = %q, want #1e1e2e (untouched built-in value)", sysP.Fg.Light)
	}

	userP, ok := reg.Get("user:default")
	if !ok {
		t.Fatalf("Get(\"user:default\") not found")
	}
	if userP.Fg.Light != "#111111" {
		t.Errorf("user:default: Fg.Light = %q, want #111111 (user value)", userP.Fg.Light)
	}
}

func containsAll(names []string, want ...string) bool {
	set := map[string]bool{}
	for _, n := range names {
		set[n] = true
	}
	for _, w := range want {
		if !set[w] {
			return false
		}
	}
	return true
}

// TestResolvePrefersUserOverBuiltin pins bare-name resolution: given both a
// user and a built-in theme of the same bare name, the user's wins.
func TestResolvePrefersUserOverBuiltin(t *testing.T) {
	dir := t.TempDir()
	writeTheme(t, dir, "default.toml", validUserTheme)

	reg, err := LoadRegistry(dir)
	if err != nil {
		t.Fatalf("LoadRegistry: %v", err)
	}

	p, name, ok := reg.Resolve("default")
	if !ok {
		t.Fatalf("Resolve(\"default\") not found")
	}
	if name != "user:default" {
		t.Errorf("Resolve(\"default\") resolved to %q, want user:default", name)
	}
	if p.Fg.Light != "#111111" {
		t.Errorf("Resolve(\"default\"): Fg.Light = %q, want #111111 (user value)", p.Fg.Light)
	}
}

// TestResolveFallsBackToBuiltin pins that a bare name with no user override
// still resolves, to the built-in.
func TestResolveFallsBackToBuiltin(t *testing.T) {
	reg, err := LoadRegistry("")
	if err != nil {
		t.Fatalf("LoadRegistry: %v", err)
	}
	p, name, ok := reg.Resolve("default")
	if !ok {
		t.Fatalf("Resolve(\"default\") not found")
	}
	if name != "system:default" {
		t.Errorf("Resolve(\"default\") resolved to %q, want system:default", name)
	}
	if p.Fg.Light != "#1e1e2e" {
		t.Errorf("Resolve(\"default\"): Fg.Light = %q, want #1e1e2e", p.Fg.Light)
	}
}

// TestResolvePassesThroughPrefixedNames pins that a caller who already has a
// full "system:"/"user:" name can pass it straight to Resolve.
func TestResolvePassesThroughPrefixedNames(t *testing.T) {
	reg, err := LoadRegistry("")
	if err != nil {
		t.Fatalf("LoadRegistry: %v", err)
	}
	p, name, ok := reg.Resolve("system:default")
	if !ok {
		t.Fatalf("Resolve(\"system:default\") not found")
	}
	if name != "system:default" {
		t.Errorf("Resolve(\"system:default\") resolved to %q, want system:default", name)
	}
	if p.Fg.Light != "#1e1e2e" {
		t.Errorf("Resolve(\"system:default\"): Fg.Light = %q, want #1e1e2e", p.Fg.Light)
	}
}

// TestResolveUnknownNotFound pins that an unknown bare name is not found,
// leaving the system:default fallback decision to the caller.
func TestResolveUnknownNotFound(t *testing.T) {
	reg, err := LoadRegistry("")
	if err != nil {
		t.Fatalf("LoadRegistry: %v", err)
	}
	if _, _, ok := reg.Resolve("does-not-exist"); ok {
		t.Errorf("Resolve(\"does-not-exist\") reported found")
	}
}

func TestMissingUserDirIsNotError(t *testing.T) {
	reg, err := LoadRegistry(filepath.Join(t.TempDir(), "does-not-exist"))
	if err != nil {
		t.Fatalf("LoadRegistry with missing user dir: %v", err)
	}
	if _, ok := reg.Get("system:default"); !ok {
		t.Errorf("embedded themes missing when user dir does not exist")
	}
}

func TestMalformedUserThemeIsSkipped(t *testing.T) {
	dir := t.TempDir()
	writeTheme(t, dir, "broken.toml", "this is not [ valid toml")
	writeTheme(t, dir, "good.toml", validUserTheme)

	reg, err := LoadRegistry(dir)
	if err != nil {
		t.Fatalf("LoadRegistry must not fail because one file is malformed: %v", err)
	}
	if _, ok := reg.Get("user:broken"); ok {
		t.Errorf("malformed theme was registered")
	}
	if _, ok := reg.Get("user:good"); !ok {
		t.Errorf("well-formed sibling theme was not loaded")
	}

	warnings := reg.Warnings()
	if len(warnings) == 0 {
		t.Fatalf("Warnings() is empty; want a warning naming broken.toml")
	}
	found := false
	for _, w := range warnings {
		if strings.Contains(w, "broken.toml") {
			found = true
		}
	}
	if !found {
		t.Errorf("Warnings() = %v, want an entry naming broken.toml", warnings)
	}
}

func TestIncompleteUserThemeIsSkipped(t *testing.T) {
	dir := t.TempDir()
	// Parses fine as TOML but is missing most required fields.
	writeTheme(t, dir, "incomplete.toml", `fg = "#111111"`)

	reg, err := LoadRegistry(dir)
	if err != nil {
		t.Fatalf("LoadRegistry: %v", err)
	}
	if _, ok := reg.Get("user:incomplete"); ok {
		t.Errorf("theme with zero-value colour fields was registered")
	}
}

func TestGetUnknownNotFound(t *testing.T) {
	reg, err := LoadRegistry("")
	if err != nil {
		t.Fatalf("LoadRegistry: %v", err)
	}
	if _, ok := reg.Get("system:does-not-exist"); ok {
		t.Errorf("Get on an unknown name reported found")
	}
}
