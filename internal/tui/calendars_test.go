package tui

import (
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/tonobo/calterm/internal/model"
	"github.com/tonobo/calterm/internal/store"
)

// ansiRE strips SGR escape sequences from a rendered line. RenderCalendars
// always styles its output (NewStyles is never given a forced ascii color
// profile in this package's tests), so an exact-equality check against a bare
// account name -- as TestRenderCalendarsHidingIsPerAccount below needs, to
// find the account boundary line -- has to strip escapes first, or the ESC
// bytes wrapping "personal" mean it never equals "personal".
var ansiRE = regexp.MustCompile("\x1b\\[[0-9;]*m")

func selectorMeta() *store.Meta {
	return &store.Meta{Accounts: []store.AccountMeta{
		{Name: "personal", Calendars: []store.CalendarMeta{
			{ID: "work", Name: "Work", Color: "#3366ff"},
			{ID: "home", Name: "Home"},
		}},
		{Name: "other", Calendars: []store.CalendarMeta{
			{ID: "work", Name: "Other Work"},
		}},
	}}
}

func TestCalendarRowsFlattensEveryAccount(t *testing.T) {
	rows := calendarRows(selectorMeta())
	if len(rows) != 3 {
		t.Fatalf("got %d rows, want 3", len(rows))
	}
	if rows[0].Account != "personal" || rows[0].ID != "work" {
		t.Errorf("row 0 = %+v", rows[0])
	}
	if rows[2].Account != "other" || rows[2].ID != "work" {
		t.Errorf("row 2 = %+v", rows[2])
	}
}

func TestRenderCalendarsShowsNamesIDsAndBoxes(t *testing.T) {
	hidden := model.HiddenSet([]string{"personal/home"})
	got := RenderCalendars(selectorMeta(), hidden, 0, 80, 20, NewStyles(true))

	for _, want := range []string{"personal", "other", "Work", "Home", "Other Work", "[x]", "[ ]"} {
		if !strings.Contains(got, want) {
			t.Errorf("selector is missing %q:\n%s", want, got)
		}
	}
	if !strings.Contains(got, "enter") || !strings.Contains(got, "esc") {
		t.Errorf("selector should document its keys:\n%s", got)
	}
}

func TestRenderCalendarsMarksHiddenOnesUnchecked(t *testing.T) {
	got := RenderCalendars(selectorMeta(), model.HiddenSet([]string{"personal/home"}), 0, 80, 20, NewStyles(true))
	for _, line := range strings.Split(got, "\n") {
		if strings.Contains(line, "Home") && !strings.Contains(line, "Other") {
			if !strings.Contains(line, "[ ]") {
				t.Errorf("hidden calendar should be unchecked: %q", line)
			}
		}
		if strings.Contains(line, "Other Work") && !strings.Contains(line, "[x]") {
			t.Errorf("shown calendar should be checked: %q", line)
		}
	}
}

// Hiding personal/work must not uncheck other/work.
func TestRenderCalendarsHidingIsPerAccount(t *testing.T) {
	got := RenderCalendars(selectorMeta(), model.HiddenSet([]string{"personal/work"}), 0, 80, 20, NewStyles(true))
	var personalWork, otherWork string
	acct := ""
	for _, line := range strings.Split(got, "\n") {
		trimmed := strings.TrimSpace(ansiRE.ReplaceAllString(line, ""))
		if trimmed == "personal" || trimmed == "other" {
			acct = trimmed
			continue
		}
		if strings.Contains(line, "Other Work") {
			otherWork = line
		} else if strings.Contains(line, "Work") && acct == "personal" {
			personalWork = line
		}
	}
	if !strings.Contains(personalWork, "[ ]") {
		t.Errorf("personal/work should be unchecked: %q", personalWork)
	}
	if !strings.Contains(otherWork, "[x]") {
		t.Errorf("other/work should still be checked: %q", otherWork)
	}
}

func TestRenderCalendarsMarksTheCursor(t *testing.T) {
	a := RenderCalendars(selectorMeta(), nil, 0, 80, 20, NewStyles(true))
	b := RenderCalendars(selectorMeta(), nil, 1, 80, 20, NewStyles(true))
	if a == b {
		t.Error("moving the cursor should change the rendering")
	}
	if strings.Count(a, cursorMarker) != 1 {
		t.Errorf("exactly one row should be marked, got %d", strings.Count(a, cursorMarker))
	}
}

func TestRenderCalendarsWithNoAccounts(t *testing.T) {
	got := RenderCalendars(&store.Meta{}, nil, 0, 80, 20, NewStyles(true))
	if !strings.Contains(strings.ToLower(got), "no calendars") {
		t.Errorf("an empty meta should say so:\n%s", got)
	}
}

// An account that discovered nothing is shown, not omitted -- a failed account
// must be visible rather than silently absent.
func TestRenderCalendarsShowsAnAccountWithNoCalendars(t *testing.T) {
	meta := &store.Meta{Accounts: []store.AccountMeta{{Name: "broken"}}}
	got := RenderCalendars(meta, nil, 0, 80, 20, NewStyles(true))
	if !strings.Contains(got, "broken") {
		t.Errorf("an account with no calendars should still be listed:\n%s", got)
	}
	if !strings.Contains(strings.ToLower(got), "none discovered") {
		t.Errorf("it should say why it is empty:\n%s", got)
	}
}

// bigSelectorMeta returns 30 calendars in one account, enough to overflow a
// short terminal and force the selector to scroll.
func bigSelectorMeta(n int) *store.Meta {
	acct := store.AccountMeta{Name: "personal"}
	for i := 0; i < n; i++ {
		acct.Calendars = append(acct.Calendars, store.CalendarMeta{
			ID: strings.Repeat("x", 3), Name: "cal"})
	}
	return &store.Meta{Accounts: []store.AccountMeta{acct}}
}

// With 30 calendars in an 80x18 window, the cursor moved down to row 25 must
// still be visible -- clampBlock truncating the rendered block from the top
// leaves the cursor marker (and the footer) off-screen entirely, so j/k move
// an invisible cursor and enter toggles a calendar the user cannot see.
func TestRenderCalendarsScrollsToKeepTheCursorVisible(t *testing.T) {
	meta := bigSelectorMeta(30)
	got := RenderCalendars(meta, nil, 25, 80, 18, NewStyles(true))
	lines := strings.Split(got, "\n")
	if len(lines) > 18 {
		t.Fatalf("got %d lines, want <= 18", len(lines))
	}
	if !strings.Contains(got, cursorMarker) {
		t.Errorf("cursor marker is missing from the rendered output at cursor=25:\n%s", got)
	}
	if !strings.Contains(got, "esc close and save") {
		t.Errorf("footer is missing from the rendered output at cursor=25:\n%s", got)
	}
}

// The window must not simply always jump to the end of the list: at cursor 0
// the top of the list (and the cursor marker) must still be visible.
func TestRenderCalendarsScrollsFromTheTopAtCursorZero(t *testing.T) {
	meta := bigSelectorMeta(30)
	got := RenderCalendars(meta, nil, 0, 80, 18, NewStyles(true))
	lines := strings.Split(got, "\n")
	if len(lines) > 18 {
		t.Fatalf("got %d lines, want <= 18", len(lines))
	}
	if !strings.Contains(got, cursorMarker) {
		t.Errorf("cursor marker is missing from the rendered output at cursor=0:\n%s", got)
	}
	if !strings.Contains(got, "esc close and save") {
		t.Errorf("footer is missing from the rendered output at cursor=0:\n%s", got)
	}
}

func TestRenderCalendarsRespectsBounds(t *testing.T) {
	big := &store.Meta{}
	for a := 0; a < 4; a++ {
		acct := store.AccountMeta{Name: strings.Repeat("account", 3)}
		for c := 0; c < 10; c++ {
			acct.Calendars = append(acct.Calendars, store.CalendarMeta{
				ID: strings.Repeat("id", 10), Name: strings.Repeat("a long calendar name ", 3)})
		}
		big.Accounts = append(big.Accounts, acct)
	}
	for _, w := range []int{30, 50, 80, 120} {
		for _, h := range []int{4, 8, 16, 40} {
			out := RenderCalendars(big, nil, 20, w, h, NewStyles(true))
			lines := strings.Split(out, "\n")
			if len(lines) > h {
				t.Errorf("w=%d h=%d: %d lines, want <= %d", w, h, len(lines), h)
			}
			for i, ln := range lines {
				if got := lipgloss.Width(ln); got > w {
					t.Errorf("w=%d h=%d: line %d is %d cells", w, h, i, got)
				}
			}
		}
	}
}

func TestSelectorTogglesWithEnterAndNotSpace(t *testing.T) {
	m := testModel(t)
	m.meta = selectorMeta()
	next, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = next.(Model)
	m = press(t, m, "c")
	if m.view != viewCalendars {
		t.Fatalf("c did not open the selector, view = %v", m.view)
	}

	// enter toggles the row under the cursor
	next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = next.(Model)
	if !model.IsHidden(m.hidden, "personal", "work") {
		t.Error("enter should have hidden the first calendar")
	}
	next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = next.(Model)
	if model.IsHidden(m.hidden, "personal", "work") {
		t.Error("enter again should have shown it")
	}

	// space must open the which-key popup, not toggle
	before := len(m.hidden)
	next, _ = m.Update(tea.KeyPressMsg{Code: ' ', Text: " "})
	m = next.(Model)
	if g, _ := m.dispatcher.Pending(); g == nil {
		t.Error("space in the selector should open the leader popup")
	}
	if len(m.hidden) != before {
		t.Error("space must not toggle a calendar")
	}
}

// j/k must move the selector's cursor, leaving the agenda's alone.
func TestSelectorCursorIsSeparateFromTheAgendaCursor(t *testing.T) {
	m := testModel(t)
	m.meta = selectorMeta()
	next, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = next.(Model)
	agendaCursor := m.cursor
	m = press(t, m, "c", "j")
	if m.calCursor != 1 {
		t.Errorf("calCursor = %d, want 1", m.calCursor)
	}
	if m.cursor != agendaCursor {
		t.Errorf("the agenda cursor moved to %d, want %d", m.cursor, agendaCursor)
	}
}

func TestSelectorCursorClamps(t *testing.T) {
	m := testModel(t)
	m.meta = selectorMeta()
	next, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = next.(Model)
	m = press(t, m, "c")
	for i := 0; i < 20; i++ {
		m = press(t, m, "j")
	}
	if m.calCursor != 2 {
		t.Errorf("calCursor = %d, want it clamped at 2", m.calCursor)
	}
	for i := 0; i < 20; i++ {
		m = press(t, m, "k")
	}
	if m.calCursor != 0 {
		t.Errorf("calCursor = %d, want it clamped at 0", m.calCursor)
	}
}

// A legacy config may hide a calendar by a bare ID, which model.IsHidden
// treats as hiding that ID in EVERY account. Toggling ONE of those calendars
// back on must not silently un-hide the other account's calendar that
// happens to share the ID -- the bare key has to be migrated to precise,
// per-account entries rather than deleted wholesale.
func TestTogglingOneBareHiddenCalendarDoesNotUnhideAnother(t *testing.T) {
	m := testModel(t)
	m.meta = &store.Meta{Accounts: []store.AccountMeta{
		{Name: "personal", Calendars: []store.CalendarMeta{{ID: "work", Name: "Work"}}},
		{Name: "other", Calendars: []store.CalendarMeta{{ID: "work", Name: "Other Work"}}},
	}}
	m.hidden = model.HiddenSet([]string{"work"}) // bare: hides both accounts' "work"
	next, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = next.(Model)
	m = press(t, m, "c") // cursor starts on row 0: personal/work

	next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = next.(Model)

	if model.IsHidden(m.hidden, "personal", "work") {
		t.Error("personal/work should now be shown")
	}
	if !model.IsHidden(m.hidden, "other", "work") {
		t.Error("other/work should still be hidden -- toggling personal/work must not affect it")
	}
	if m.hidden["work"] {
		t.Error("the bare key should be gone, migrated to qualified entries")
	}
	if !m.hidden["other/work"] {
		t.Error("other/work's hiding should now be recorded as a qualified entry")
	}
}

// Pressing c a second time while already in the selector must close it (and
// save, if dirty) rather than re-entering, which would overwrite prevView and
// trap esc for the rest of the session.
func TestPressingCalendarsKeyTwiceClosesRatherThanTraps(t *testing.T) {
	m := testModel(t)
	m.meta = selectorMeta()
	next, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = next.(Model)
	startView := m.view

	m = press(t, m, "c")
	if m.view != viewCalendars {
		t.Fatalf("first c did not open the selector, view = %v", m.view)
	}
	m = press(t, m, "c")
	if m.view == viewCalendars {
		t.Fatalf("second c should close the selector, view = %v", m.view)
	}

	// esc must still work: it must not have been trapped by a clobbered prevView.
	next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	m = next.(Model)
	if m.view != startView {
		t.Errorf("esc after double-c left view = %v, want %v (esc should never be trapped)", m.view, startView)
	}
}

// Every exit route from the selector must persist a pending toggle, not only esc.
func TestSwitchingViewFromSelectorSavesAPendingToggle(t *testing.T) {
	m := testModel(t)
	m.meta = selectorMeta()
	next, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = next.(Model)
	m = press(t, m, "c")
	next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter}) // toggle -> calDirty
	m = next.(Model)

	next, cmd := m.runAction("view.month")
	m = next.(Model)
	if cmd == nil {
		t.Error("switching to month view with a pending toggle should return a save command")
	}
	if m.view != viewMonth {
		t.Errorf("view = %v, want viewMonth", m.view)
	}
}

// Ctrl+C is the unconditional exit route. Even there, a pending toggle must
// save before the process exits rather than being dropped.
func TestCtrlCWithPendingToggleSavesFirst(t *testing.T) {
	m := testModel(t)
	m.meta = selectorMeta()
	next, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = next.(Model)
	m = press(t, m, "c")
	next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = next.(Model)

	next, cmd := m.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	m = next.(Model)
	if cmd == nil {
		t.Fatal("quit with a pending toggle returned no command")
	}
	// tea.Sequence wraps its commands in an unexported slice type, so the
	// commands are run out via reflection to confirm the sequence ends in a
	// QuitMsg -- i.e. the save happens BEFORE the process exits, not dropped.
	msg := cmd()
	v := reflect.ValueOf(msg)
	if v.Kind() != reflect.Slice {
		t.Fatalf("quit's command produced %T, want a sequence of commands (save then quit)", msg)
	}
	var last tea.Msg
	for i := 0; i < v.Len(); i++ {
		c := v.Index(i).Interface().(tea.Cmd)
		last = c()
	}
	if _, ok := last.(tea.QuitMsg); !ok {
		t.Errorf("last message in the quit sequence = %T, want tea.QuitMsg", last)
	}
}

// Hiding the calendar under the agenda's cursor must re-clamp that cursor
// immediately, not leave it pointing past the (now shorter, possibly empty)
// visible list until some unrelated re-clamp happens to run.
func TestTogglingACalendarReClampsTheAgendaCursor(t *testing.T) {
	m := testModel(t)
	next, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = next.(Model)
	m.cursor = len(m.visible()) - 1 // "G" -- jump to the last event
	if m.cursor <= 0 {
		t.Fatalf("fixture too small to exercise this: cursor = %d", m.cursor)
	}

	m = press(t, m, "c") // testModel's fixture has exactly one calendar
	next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = next.(Model)

	if len(m.visible()) != 0 {
		t.Fatalf("hiding the only calendar should leave nothing visible, got %d", len(m.visible()))
	}
	if m.cursor != 0 {
		t.Errorf("cursor = %d after hiding everything, want 0 (re-clamped)", m.cursor)
	}
}

// This is the one test that drives the TUI -> config seam end to end: it
// builds a Model with configPath pointing at a real file, toggles a
// calendar, exits through esc, RUNS the returned command (rather than only
// asserting it is non-nil, as the older esc test does), and reads the file
// back. This is what would have caught IMPORTANT 1, IMPORTANT 2, and
// IMPORTANT 4's persistence consequences: a command being returned proves
// nothing about what ends up on disk.
func TestEscWritesTheToggleToTheRealConfigFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	body := `[[account]]
name = "personal"
url = "https://example.com/dav"
username = "u"
password_cmd = "echo p"

[calendars]
hidden = []
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	m := testModel(t)
	m.meta = selectorMeta()
	m.configPath = path
	next, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = next.(Model)

	m = press(t, m, "c") // cursor starts on row 0: personal/work
	next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = next.(Model)

	next, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	m = next.(Model)
	if cmd == nil {
		t.Fatal("esc after a toggle returned no command")
	}
	msg := cmd()
	if saved, ok := msg.(hiddenSaveMsg); !ok || saved.err != nil {
		t.Fatalf("running esc's command produced %#v, want a successful hiddenSaveMsg", msg)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), `hidden = ["personal/work"]`) {
		t.Errorf("config file was not updated with the toggle:\n%s", got)
	}
}

// esc must not write the config when nothing changed.
func TestEscDoesNotSaveWhenNothingChanged(t *testing.T) {
	m := testModel(t)
	m.meta = selectorMeta()
	next, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = next.(Model)
	m = press(t, m, "c")

	next, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	m = next.(Model)
	if cmd != nil {
		t.Error("esc without a change should not attempt a save")
	}

	// Now toggle, reopen the selector, and esc: a save must be attempted.
	m = press(t, m, "c")
	next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = next.(Model)
	next, cmd = m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	m = next.(Model)
	if cmd == nil {
		t.Error("esc after a change should attempt a save")
	}
}
