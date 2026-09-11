package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/tonobo/calterm/internal/config"
	"github.com/tonobo/calterm/internal/store"
)

// themeValidConfig is the minimum config.SetTheme's post-write validation
// needs to accept the file -- mirrors internal/config/write_test.go's
// validAccount.
const themeValidConfig = `[[account]]
name = "personal"
url = "https://example.com/dav"
username = "u"
password_cmd = "echo p"

[ui]
theme = "system:default"
`

// themeTestModel is testModel plus a real, on-disk config file wired up as
// configPath, so enter's persistence can be asserted against it.
func themeTestModel(t *testing.T) (Model, string) {
	t.Helper()
	m := testModel(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte(themeValidConfig), 0o600); err != nil {
		t.Fatal(err)
	}
	m.configPath = path
	return m, path
}

// openThemePicker sends the "space t" chord and, unlike press (which only
// covers direct keys), also pumps the resulting actionMsg command through a
// second Update -- the leader chord's Run returns a tea.Cmd rather than
// acting synchronously, and nothing drives Bubble Tea's command loop in
// these tests.
func openThemePicker(t *testing.T, m Model) Model {
	t.Helper()
	next, _ := m.Update(tea.KeyPressMsg{Code: ' ', Text: " "})
	m = next.(Model)
	next, cmd := m.Update(tea.KeyPressMsg{Code: 't', Text: "t"})
	m = next.(Model)
	if cmd == nil {
		t.Fatal("space t produced no command")
	}
	next, _ = m.Update(cmd())
	m = next.(Model)
	return m
}

// An unknown or unresolvable configured theme must fall back to
// system:default and still render -- a malformed or missing theme choice
// must never stop the TUI starting.
func TestUnknownConfiguredThemeFallsBackToDefault(t *testing.T) {
	cfg := &config.Config{UI: config.UIConfig{DefaultView: "agenda", WeekStart: "monday", Theme: "user:does-not-exist"}}
	idx := &store.OccurrenceIndex{
		GeneratedAt: time.Date(2026, 6, 10, 7, 0, 0, 0, time.UTC),
		Occurrences: agendaFixture(),
	}
	meta := &store.Meta{Accounts: []store.AccountMeta{{
		Name: "personal", Calendars: []store.CalendarMeta{{ID: "work", Name: "Work"}},
	}}}
	m := New(cfg, nil, idx, meta, time.Date(2026, 6, 10, 8, 0, 0, 0, time.UTC))
	// loadThemes is what actually performs the Resolve(cfg.UI.Theme) this
	// test is exercising -- New alone always parks themeName at
	// "system:default" regardless of cfg, so skipping this call would make
	// the assertion below pass vacuously.
	m = m.loadThemes("")
	m.loc = time.UTC

	if m.themeName != "system:default" {
		t.Errorf("themeName = %q, want %q (fallback)", m.themeName, "system:default")
	}
	next, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = next.(Model)
	if got := m.View().Content; got == "" {
		t.Error("an unresolvable configured theme should still render, not produce an empty view")
	}
}

func TestThemePickerListsEveryRegisteredTheme(t *testing.T) {
	m := testModel(t)
	m = openThemePicker(t, m)
	if m.view != viewThemePicker {
		t.Fatalf("space t did not open the theme picker, view = %v", m.view)
	}
	got := m.View().Content
	names := m.themes.Names()
	if len(names) == 0 {
		t.Fatal("registry has no themes; fixture is broken")
	}
	for _, name := range names {
		if !strings.Contains(got, name) {
			t.Errorf("theme picker is missing %q:\n%s", name, got)
		}
	}
}

// Moving the cursor must swap the model's active styles immediately -- the
// preview is live, not deferred to enter.
func TestThemePickerCursorPreviewsLive(t *testing.T) {
	m := testModel(t)
	m = openThemePicker(t, m)
	names := m.themes.Names()
	if len(names) < 2 {
		// The registry here is testModel's, seeded from loadThemes("") --
		// the seven embedded built-ins only, compiled in via go:embed. Fewer
		// than two is not a reason to skip; it means the built-in registry
		// itself is broken, which is exactly the state this test (and its
		// siblings below) exist to catch.
		t.Fatalf("registry has only %d theme(s); the embedded built-ins are broken", len(names))
	}
	startCursor := m.themeCursor
	before := m.styles
	m = press(t, m, "j")
	if m.themeCursor != startCursor+1 {
		t.Fatalf("themeCursor = %d, want %d", m.themeCursor, startCursor+1)
	}
	if m.styles.Base.GetForeground() == before.Base.GetForeground() {
		t.Errorf("moving the cursor did not change the previewed styles")
	}
	// The whole model repaints, not just the list: rendering the month view
	// underneath (well, the picker itself, but through the same m.styles)
	// must reflect the new colours too.
	got := m.View().Content
	if got == "" {
		t.Fatal("empty render after preview")
	}
}

func TestThemePickerCursorClamps(t *testing.T) {
	m := testModel(t)
	m = openThemePicker(t, m)
	n := len(m.themes.Names())
	for i := 0; i < n+10; i++ {
		m = press(t, m, "j")
	}
	if m.themeCursor != n-1 {
		t.Errorf("themeCursor = %d, want clamped at %d", m.themeCursor, n-1)
	}
	for i := 0; i < n+10; i++ {
		m = press(t, m, "k")
	}
	if m.themeCursor != 0 {
		t.Errorf("themeCursor = %d, want clamped at 0", m.themeCursor)
	}
}

// enter must persist the previewed theme to the real config file, in the
// fully-prefixed form Get expects -- not a bare name Get could never find
// again on the next start.
func TestThemePickerEnterPersists(t *testing.T) {
	m, path := themeTestModel(t)
	m = openThemePicker(t, m)
	names := m.themes.Names()
	if len(names) < 2 {
		t.Fatalf("registry has only %d theme(s); the embedded built-ins are broken", len(names))
	}
	m = press(t, m, "j") // move off system:default
	want := names[m.themeCursor]

	next, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = next.(Model)

	if m.view == viewThemePicker {
		t.Fatal("enter should have left the picker")
	}
	if m.themeName != want {
		t.Errorf("m.themeName = %q, want %q", m.themeName, want)
	}

	reloaded, err := readTheme(path)
	if err != nil {
		t.Fatalf("reloading config: %v", err)
	}
	if reloaded != want {
		t.Errorf("config file has theme %q after reload, want %q", reloaded, want)
	}
}

// esc must restore the styles active before the picker opened AND leave the
// config file byte-identical -- nothing was ever written.
func TestThemePickerEscRestoresAndDoesNotWrite(t *testing.T) {
	m, path := themeTestModel(t)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	beforeStyles := m.styles
	beforeName := m.themeName

	m = openThemePicker(t, m)
	names := m.themes.Names()
	if len(names) < 2 {
		t.Fatalf("registry has only %d theme(s); the embedded built-ins are broken", len(names))
	}
	m = press(t, m, "j") // preview a different theme

	next, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	m = next.(Model)

	if m.view == viewThemePicker {
		t.Fatal("esc should have left the picker")
	}
	if m.themeName != beforeName {
		t.Errorf("themeName = %q after esc, want unchanged %q", m.themeName, beforeName)
	}
	if m.styles.Base.GetForeground() != beforeStyles.Base.GetForeground() {
		t.Errorf("esc did not restore the original styles")
	}

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Errorf("config file changed after esc:\n--- before ---\n%s\n--- after ---\n%s", before, after)
	}
}

// A failed write must surface an error without corrupting the model's
// styles -- the previewed (valid) Styles stay in place, not some zero-value
// or half-built state.
func TestThemePickerFailedWriteDoesNotCorruptStyles(t *testing.T) {
	m := testModel(t)
	m.configPath = filepath.Join(t.TempDir(), "does-not-exist.toml") // SetTheme will fail: no such file
	m = openThemePicker(t, m)
	names := m.themes.Names()
	if len(names) < 2 {
		t.Fatalf("registry has only %d theme(s); the embedded built-ins are broken", len(names))
	}
	m = press(t, m, "j")
	previewed := m.styles

	next, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = next.(Model)

	if !m.statusIsErr {
		t.Errorf("a failed theme write should set an error status")
	}
	if m.status == "" {
		t.Errorf("a failed theme write should surface a message")
	}
	if m.styles.Base.GetForeground() != previewed.Base.GetForeground() {
		t.Errorf("a failed write changed the previewed styles instead of leaving them alone")
	}
}

// Quitting by any route other than esc must not silently discard a theme
// enter already applied. Because the write happens synchronously in
// applyTheme (see app.go), the file is already saved by the time any
// subsequent keypress -- including quit -- is even processed; this proves
// that empirically rather than by inspection.
func TestQuitAfterApplyingThemeDoesNotDiscardIt(t *testing.T) {
	m, path := themeTestModel(t)
	m = openThemePicker(t, m)
	names := m.themes.Names()
	if len(names) < 2 {
		t.Fatalf("registry has only %d theme(s); the embedded built-ins are broken", len(names))
	}
	m = press(t, m, "j")
	want := names[m.themeCursor]

	next, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = next.(Model)

	next, cmd := m.runAction("quit")
	m = next.(Model)
	if cmd == nil {
		t.Fatal("quit returned no command")
	}
	if msg := cmd(); msg == nil {
		t.Fatal("quit's command produced no message")
	} else if _, ok := msg.(tea.QuitMsg); !ok {
		t.Errorf("quit's command produced %T, want tea.QuitMsg", msg)
	}

	reloaded, err := readTheme(path)
	if err != nil {
		t.Fatalf("reloading config: %v", err)
	}
	if reloaded != want {
		t.Errorf("config file has theme %q after quit, want %q (applied theme was discarded)", reloaded, want)
	}
}

// TestDirectViewKeysLeaveThemePickerRestoringTheme covers the bug shape the
// task calls out by name: a selector that loses its state on every exit
// route except esc. bindings.go's runAction generalizes the calendar
// selector's single-branch interception into a switch that also drains the
// theme picker for a direct view key (m/w/d/a) -- NOT the leader chord, the
// KeyMap-bound direct key handled straight in handleKey's switch. Without
// that interception, a live preview started with j/k would leak out as the
// active look, applied to nothing, the moment the user switched views
// directly instead of pressing esc.
func TestDirectViewKeysLeaveThemePickerRestoringTheme(t *testing.T) {
	cases := []struct {
		key  string
		want viewKind
	}{
		{"m", viewMonth},
		{"w", viewWeek},
		{"d", viewDay},
		{"a", viewAgenda},
	}
	for _, tc := range cases {
		t.Run(tc.key, func(t *testing.T) {
			m := testModel(t)
			committed := m.styles

			m = openThemePicker(t, m)
			m = press(t, m, "j") // start a live preview
			previewed := m.styles
			if previewed.Base.GetForeground() == committed.Base.GetForeground() {
				t.Fatal("fixture problem: previewing did not change styles, so this test can't tell restore from no-op")
			}

			m = press(t, m, tc.key)

			if m.view != tc.want {
				t.Errorf("view = %v, want %v", m.view, tc.want)
			}
			if m.styles.Base.GetForeground() != committed.Base.GetForeground() {
				t.Errorf("styles were not restored to the committed theme after pressing %q", tc.key)
			}
			if m.styles.Base.GetForeground() == previewed.Base.GetForeground() {
				t.Errorf("styles still match the discarded preview after pressing %q", tc.key)
			}
		})
	}
}

// TestThemeKeyWhileCalendarsOpenSavesPendingToggleAndOpensPicker covers the
// calendars-to-theme cross transition (bindings.go's "theme" action): opening
// the theme picker while the calendar selector is open, with an unsaved
// toggle pending, must save that toggle (the same contract every other exit
// from the selector honours) before opening the picker.
func TestThemeKeyWhileCalendarsOpenSavesPendingToggleAndOpensPicker(t *testing.T) {
	m := testModel(t)
	m.meta = selectorMeta()
	next, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = next.(Model)
	m = press(t, m, "c") // open the calendar selector
	if m.view != viewCalendars {
		t.Fatalf("c did not open the selector, view = %v", m.view)
	}
	next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter}) // toggle -> calDirty
	m = next.(Model)
	if !m.calDirty {
		t.Fatal("fixture problem: toggling did not set calDirty")
	}

	next, cmd := m.runAction("theme")
	m = next.(Model)

	if m.view != viewThemePicker {
		t.Fatalf("view = %v, want viewThemePicker", m.view)
	}
	if cmd == nil {
		t.Error("opening the theme picker with a pending calendar toggle should return a save command")
	}
	if m.calDirty {
		t.Error("calDirty should have been cleared once the pending toggle was saved")
	}
	if want := indexOf(m.themes.Names(), m.themeName); m.themeCursor != want {
		t.Errorf("themeCursor = %d, want %d (the committed theme's row)", m.themeCursor, want)
	}
}

// TestCalendarsKeyWhileThemePickerOpenRestoresAndOpensCalendars covers the
// theme-to-calendars cross transition (bindings.go's "calendars" action):
// opening the calendar selector while the theme picker is open, mid-preview,
// must restore the committed theme first, not leave the preview applied to
// nothing.
func TestCalendarsKeyWhileThemePickerOpenRestoresAndOpensCalendars(t *testing.T) {
	m := testModel(t)
	committed := m.styles

	m = openThemePicker(t, m)
	m = press(t, m, "j") // start a live preview
	previewed := m.styles
	if previewed.Base.GetForeground() == committed.Base.GetForeground() {
		t.Fatal("fixture problem: previewing did not change styles")
	}

	m = press(t, m, "c") // direct key: open calendars

	if m.view != viewCalendars {
		t.Fatalf("view = %v, want viewCalendars", m.view)
	}
	if m.styles.Base.GetForeground() != committed.Base.GetForeground() {
		t.Error("styles were not restored to the committed theme")
	}
	if m.styles.Base.GetForeground() == previewed.Base.GetForeground() {
		t.Error("styles still match the discarded preview")
	}
}

// TestThemePickerPreviewChangesTheFullRenderedFrame asserts against the
// actual rendered bytes, not just the styles struct: two different cursor
// positions must produce two different View().Content frames, proving the
// whole screen -- not merely m.styles in isolation -- repaints from the
// live preview.
func TestThemePickerPreviewChangesTheFullRenderedFrame(t *testing.T) {
	m := testModel(t)
	m = openThemePicker(t, m)
	names := m.themes.Names()
	if len(names) < 2 {
		t.Fatalf("registry has only %d theme(s); the embedded built-ins are broken", len(names))
	}

	first := m.View().Content
	m = press(t, m, "j")
	second := m.View().Content

	if first == second {
		t.Error("moving the cursor did not change the rendered frame")
	}
	if !strings.Contains(second, names[m.themeCursor]) {
		t.Errorf("rendered frame after moving the cursor is missing the new candidate %q:\n%s", names[m.themeCursor], second)
	}
}

// readTheme reads just the `theme = "..."` value back out of a config file,
// without pulling in internal/config (which would make this test package
// depend on account validation quirks it doesn't care about).
func readTheme(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(b), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "theme") {
			_, v, ok := strings.Cut(trimmed, "=")
			if !ok {
				continue
			}
			v = strings.TrimSpace(v)
			return strings.Trim(v, `"`), nil
		}
	}
	return "", nil
}

// fixtureUserTheme is a minimal, valid user theme -- just enough to pass
// Palette.validate -- written into a t.TempDir() and injected as the user
// theme directory via loadThemes, so the picker's row for it is a real,
// filesystem-backed theme rather than a hardcoded name.
const fixtureUserTheme = `name = "Injected"
mode = ""

fg     = "#ffffff"
dim    = "#aaaaaa"
accent = "#ff00ff"
warn   = "#ff0000"

grid_line   = "#888888"
selected_fg = "#00ffff"
selected_bg = "#222222"
status_fg   = "#cccccc"
status_bg   = "#111111"

calendar_palette = ["#ff00ff", "#00ff00"]
`

// TestUserThemeDirIsInjectedNotReadFromRealHome is the proof the coordinator
// asked for: a theme file dropped into an arbitrary directory -- standing in
// for a real user's $XDG_CONFIG_HOME/calterm/themes -- shows up in the
// picker when that directory is explicitly injected via loadThemes, and a
// SEPARATE Model built the ordinary way (testModel, which injects "") does
// not see it. That second half is what actually demonstrates the injection
// is real: a test that only checked the first half could still pass if
// loadThemes silently fell back to the real home directory and this
// fixture's directory happened to be reachable some other way.
func TestUserThemeDirIsInjectedNotReadFromRealHome(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "injected.toml"), []byte(fixtureUserTheme), 0o600); err != nil {
		t.Fatal(err)
	}

	injected := testModel(t)
	injected = injected.loadThemes(dir)
	injected = openThemePicker(t, injected)

	const want = "user:injected"
	if !strings.Contains(injected.View().Content, want) {
		t.Errorf("picker with the injected theme dir is missing %q:\n%s", want, injected.View().Content)
	}
	found := false
	for _, n := range injected.themes.Names() {
		if n == want {
			found = true
		}
	}
	if !found {
		t.Errorf("Names() = %v, missing %q", injected.themes.Names(), want)
	}

	// A model built the ordinary way (testModel injects "") must NOT see it:
	// proves the picker's theme list tracks the injected directory, not some
	// process-wide default that this fixture directory leaked into.
	plain := testModel(t)
	plain = openThemePicker(t, plain)
	if strings.Contains(plain.View().Content, want) {
		t.Errorf("a Model built with the default (\"\") theme dir unexpectedly sees the injected theme:\n%s", plain.View().Content)
	}
	for _, n := range plain.themes.Names() {
		if n == want {
			t.Errorf("plain.themes.Names() unexpectedly contains %q: %v", want, plain.themes.Names())
		}
	}
	if gotN, wantN := len(plain.themes.Names()), len(injected.themes.Names())-1; gotN != wantN {
		t.Errorf("plain registry has %d themes, want %d (injected registry's %d minus the one fixture)",
			gotN, wantN, len(injected.themes.Names()))
	}
}
