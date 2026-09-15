// Package tui implements calterm's interactive terminal interface.
package tui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/tonobo/calterm/internal/config"
	"github.com/tonobo/calterm/internal/desktop"
	"github.com/tonobo/calterm/internal/model"
	"github.com/tonobo/calterm/internal/rsvp"
	"github.com/tonobo/calterm/internal/store"
	"github.com/tonobo/calterm/internal/tui/keys"
	"github.com/tonobo/calterm/internal/tui/theme"
	"github.com/tonobo/calterm/internal/tui/whichkey"
)

type viewKind int

const (
	viewAgenda viewKind = iota
	viewMonth
	viewWeek
	viewDay
	viewDetail
	viewCalendars
	viewThemePicker
	viewNotificationSounds
)

// Model is the root Bubble Tea model. It owns the loaded occurrence set, the
// active view, the cursor, and the status line; each view is rendered by a
// pure function.
type Model struct {
	cfg   *config.Config
	store *store.Store
	idx   *store.OccurrenceIndex
	meta  *store.Meta

	keys   KeyMap
	styles Styles
	names  map[string]string
	loc    *time.Location

	// dispatcher is a pointer on purpose: Model is copied by value on every
	// update, and the chord state has to survive those copies.
	dispatcher *keys.Dispatcher

	view     viewKind
	prevView viewKind
	// detailReturnView is kept separately because a selector may be opened on
	// top of a detail view and overwrite prevView. Returning from that selector
	// and then pressing back must still reach the original main view.
	detailReturnView viewKind
	// dayReturnView serves the same purpose for a day reached by drilling down
	// from month/week with Enter. A day selected directly with d is a main view;
	// only dayDrilldown makes it a back target. Keeping this separate from
	// prevView prevents a selector opened on top from losing the month/week
	// return path.
	dayReturnView viewKind
	dayDrilldown  bool
	cursor        int
	// calCursor indexes the calendar selector's rows. It is separate from
	// cursor on purpose: j/k in the selector must not scroll the agenda
	// underneath it.
	calCursor int
	// calDirty is true once a toggle has changed the selection since the
	// selector was last opened. esc only saves (and only reports "saved")
	// when this is true -- otherwise opening the selector and immediately
	// leaving would rewrite the config, a file holding the user's
	// password_cmd, for no reason.
	calDirty            bool
	focusDay            time.Time
	now                 time.Time
	width               int
	height              int
	status              string
	statusIsErr         bool
	syncing             bool
	testingNotification bool

	// themes is every theme calterm knows about: the embedded built-ins plus
	// whatever the user's theme directory contributed. It is loaded once, in
	// New, and never mutated afterwards.
	themes *theme.Registry
	// themeName is the CANONICAL, fully-prefixed name (e.g. "system:default"
	// or "user:mine") of the committed theme -- the one config.SetTheme has
	// (or will, on next apply) persisted. It is what esc, and any other route
	// out of the picker that isn't enter, restores to. It only changes when
	// the picker's enter key successfully applies a new theme.
	themeName string
	// isDark tracks the terminal's reported background mode, needed to
	// rebuild Styles from a Palette whenever the active theme changes.
	// Corrected by the first tea.BackgroundColorMsg; true until then.
	isDark bool
	// themeCursor indexes the theme picker's rows, the same shape calCursor
	// gives the calendar selector.
	themeCursor int
	// installedSounds contains the sound-theme event IDs discovered from the
	// XDG data paths. soundCursor is independent from every calendar cursor.
	installedSounds []string
	soundCursor     int

	filter      textinput.Model
	filtering   bool
	filterQuery string

	// hidden is the live set of calendars the user has deselected. It is
	// seeded from the config and mutated by the selector, so a toggle applies
	// on the next render rather than after a save.
	hidden map[string]bool

	// configPath is where saveHiddenCmd writes the selection back to. A zero
	// value means "do not save" -- the safe default for tests that build a
	// Model without a real config file.
	configPath string

	// detailOccurrence overrides the agenda cursor while a caller such as
	// `calterm open` launches directly into an event. It also lets a hidden
	// calendar's occurrence remain selected without changing the user's
	// visibility settings. A read-only override came from an external ICS that
	// could not be matched to a cached account/calendar and may never RSVP.
	detailOccurrence *model.Occurrence
	detailReadOnly   bool
	detailNotice     string
}

// FocusOptions describes an occurrence that should be selected immediately
// when the TUI starts. ReadOnly must be true for an external event that was
// not found in the synced cache.
type FocusOptions struct {
	Occurrence    model.Occurrence
	ReadOnly      bool
	Notice        string
	NoticeIsError bool
}

func New(cfg *config.Config, s *store.Store, idx *store.OccurrenceIndex, meta *store.Meta, now time.Time) Model {
	markIdentities(idx, cfg)
	names := map[string]string{}
	// Keyed by account AND calendar: calendar IDs are the last path segment of
	// a collection href, so two accounts each exposing /personal/ would
	// otherwise share one entry and one account's name and colour would win.
	for _, acct := range meta.Accounts {
		for _, cal := range acct.Calendars {
			key := model.CalendarKey(acct.Name, cal.ID)
			names[key] = cal.Name
		}
	}

	m := Model{
		cfg: cfg, store: s, idx: idx, meta: meta,
		keys: DefaultKeyMap(), names: names,
		loc: time.Local, now: now, focusDay: now,
		view: parseView(cfg.UI.DefaultView),
		// themes starts empty and themeName pinned to the always-registered
		// "system:default" -- New never touches the filesystem for themes.
		// loadThemes (below), which Run calls with the real user directory,
		// is what actually populates the registry; a Model built by New
		// alone (as every test but the theme picker's own does) is fully
		// hermetic and gets exactly today's built-in look regardless of
		// what a real user's ~/.config/calterm/themes happens to contain.
		themes: &theme.Registry{}, themeName: "system:default",
		isDark: true, // corrected on the first BackgroundColorMsg
	}
	m.styles = m.buildStyles(m.themeName)
	m.hidden = model.HiddenSet(cfg.Calendars.Hidden)
	m.dispatcher = keys.NewDispatcher(chordRoot())
	// Movement keys are handled directly by handleKey's switch, not by the
	// chord tree, so an unmatched movement key mid-chord still does something
	// (it scrolls) and must not be reported as "no binding for j".
	movementKeys := movementKeySet(m.keys)
	m.dispatcher.SetHandledElsewhere(func(key string) bool {
		return movementKeys[key]
	})
	// The index window reaches a month into the past, so occurrence 0 is the
	// OLDEST one and RenderAgenda scrolls to the cursor: starting at 0 opens
	// calterm on last month. Start on the first event that has not finished,
	// including a multi-day event carried into today's agenda group.
	m.cursor = firstNonPast(m.agendaVisible(), now, m.loc)
	if idx.GeneratedAt.IsZero() {
		m.status = "never synced — press r to sync now"
		m.statusIsErr = true
	}
	ti := textinput.New()
	ti.Placeholder = "filter events"
	ti.Prompt = "/"
	m.filter = ti
	return m
}

// NewFocused builds the normal model and opens one occurrence directly in
// the detail view. The explicit detail selection bypasses visibility filters,
// so opening an event from a hidden calendar does not have to mutate config.
func NewFocused(cfg *config.Config, s *store.Store, idx *store.OccurrenceIndex, meta *store.Meta, now time.Time, opts FocusOptions) Model {
	m := New(cfg, s, idx, meta, now)
	focused := opts.Occurrence
	if opts.ReadOnly {
		// Unknown cache ownership is a hard read-only boundary. Clear identity
		// fields even if a caller accidentally supplied them.
		focused.AccountID = ""
		focused.CalendarID = ""
		focused.AttendeeStatus = ""
		focused.RSVPRequested = false
		for i := range focused.Attendees {
			focused.Attendees[i].Self = false
		}
	} else {
		for _, account := range cfg.Accounts {
			if account.Name == focused.AccountID {
				focused.MarkIdentity(account.Email)
				break
			}
		}
	}

	m.detailOccurrence = &focused
	m.detailReadOnly = opts.ReadOnly
	m.detailNotice = opts.Notice
	m.focusDay = focused.Start
	m.detailReturnView = m.view
	m.prevView = m.view
	m.view = viewDetail
	if opts.Notice != "" {
		m.status = opts.Notice
		m.statusIsErr = opts.NoticeIsError
	}

	// Keep cursor aligned to the agenda position when the occurrence is visible.
	// A hidden event instead uses its raw occurrence-index position while the
	// explicit detail selection is active; leaving detail clamps back into the
	// still-filtered agenda without unhiding anything.
	foundVisible := false
	for i, occurrence := range m.agendaVisible() {
		if sameOccurrence(occurrence, focused) {
			m.cursor = i
			foundVisible = true
			break
		}
	}
	if !foundVisible && !opts.ReadOnly {
		for i, occurrence := range idx.Occurrences {
			if sameOccurrence(occurrence, focused) {
				m.cursor = i
				break
			}
		}
	}
	return m
}

// userThemesDir returns $XDG_CONFIG_HOME/calterm/themes, or "" if the user's
// config directory can't be determined -- theme.LoadRegistry treats "" the
// same as a missing directory: no user themes, not an error.
func userThemesDir() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "calterm", "themes")
}

// loadThemes loads the theme registry from themesDir -- exactly what
// theme.LoadRegistry does with it, "" or a missing/unreadable directory
// meaning "no user themes" -- resolves cfg.UI.Theme against the result, and
// rebuilds m.styles accordingly. It is kept separate from New, and called
// explicitly by Run with the real $XDG_CONFIG_HOME/calterm/themes, for the
// same reason configPath is a field Run sets after construction rather than
// a New parameter: which real directory the running program reads from is
// the caller's concern, not the model's. Tests that want a picker with a
// controlled, known theme list call this directly with a t.TempDir()
// (fixture themes) or "" (deterministic: the embedded built-ins only,
// unaffected by whatever a real user's theme directory happens to hold).
//
// A theme problem here -- LoadRegistry failing outright, or cfg.UI.Theme not
// resolving -- must never stop the TUI starting: it falls back to an empty
// registry and "system:default" respectively, same as New's own zero state.
func (m Model) loadThemes(themesDir string) Model {
	registry, err := theme.LoadRegistry(themesDir)
	if err != nil || registry == nil {
		registry = &theme.Registry{}
	}
	m.themes = registry
	if _, canon, ok := registry.Resolve(m.cfg.UI.Theme); ok {
		m.themeName = canon
	} else {
		m.themeName = "system:default"
	}
	m.styles = m.buildStyles(m.themeName)
	return m
}

// calendarColorsFromMeta extracts the server-supplied calendar colours from
// meta, keyed the same way Styles.Calendar looks them up. Shared by New, the
// BackgroundColorMsg handler, and buildStyles so the three don't drift.
func calendarColorsFromMeta(meta *store.Meta) map[string]string {
	colors := map[string]string{}
	for _, acct := range meta.Accounts {
		for _, cal := range acct.Calendars {
			if cal.Color != "" {
				colors[model.CalendarKey(acct.Name, cal.ID)] = cal.Color
			}
		}
	}
	return colors
}

// buildStyles resolves name against the registry and builds Styles for it,
// in the model's current light/dark mode, carrying over the calendar
// colours discovery reported. name not resolving falls back to
// "system:default"; that not resolving either (a broken build, or an empty
// registry from a failed LoadRegistry) falls back to the hardcoded
// theme.DefaultPalette -- so a broken or missing theme can never stop
// calterm from rendering.
func (m Model) buildStyles(name string) Styles {
	p, ok := m.themes.Get(name)
	if !ok {
		p, ok = m.themes.Get("system:default")
	}
	if !ok {
		p = theme.DefaultPalette()
	}
	st := theme.BuildStyles(p, m.isDark)
	st.SetCalendarColors(calendarColorsFromMeta(m.meta))
	return st
}

// activeThemeName is the name of the theme currently ON SCREEN: the
// picker's previewed candidate while it's open, otherwise the committed
// theme. It is what a BackgroundColorMsg mid-preview should rebuild Styles
// from, so a light/dark correction never clobbers a live preview back to
// the committed theme.
func (m Model) activeThemeName() string {
	if m.view == viewThemePicker {
		if names := m.themes.Names(); m.themeCursor >= 0 && m.themeCursor < len(names) {
			return names[m.themeCursor]
		}
	}
	return m.themeName
}

// firstNonPast returns the index of the earliest occurrence that has not yet
// finished, or the last index when everything is in the past (and 0 when
// there is nothing at all). Both New and the Today key use it, so "where today
// is" cannot drift between opening the app and pressing t.
func firstNonPast(occs []model.Occurrence, now time.Time, loc *time.Location) int {
	for i, o := range occs {
		if !occurrencePast(o, now, loc) {
			return i
		}
	}
	if len(occs) > 0 {
		return len(occs) - 1
	}
	return 0
}

// agendaVisible is visible in the same order RenderAgenda uses. Keeping the
// cursor and detail view on this slice prevents a carried-over occurrence from
// rendering in one row while selecting a different raw-index occurrence.
func (m Model) agendaVisible() []model.Occurrence {
	return orderAgendaOccurrences(m.visible(), m.now, m.loc)
}

// visible returns the occurrences after filtering: first the calendars the
// user has deselected, then the / query. The index itself stays complete, so
// re-showing a calendar is instant and needs no sync.
func (m Model) visible() []model.Occurrence {
	occs := m.idx.Occurrences
	if len(m.hidden) > 0 {
		kept := make([]model.Occurrence, 0, len(occs))
		for _, o := range occs {
			if !model.IsHidden(m.hidden, o.AccountID, o.CalendarID) {
				kept = append(kept, o)
			}
		}
		occs = kept
	}
	return Filter(occs, m.filterQuery)
}

func parseView(s string) viewKind {
	switch s {
	case "month":
		return viewMonth
	case "week":
		return viewWeek
	case "day":
		return viewDay
	default:
		return viewAgenda
	}
}

func (m Model) Init() tea.Cmd {
	return tea.RequestBackgroundColor
}

// syncDoneMsg reports the result of a background sync.
type syncDoneMsg struct {
	idx  *store.OccurrenceIndex
	meta *store.Meta
	err  error
}

type rsvpDoneMsg struct {
	decision  rsvp.Decision
	idx       *store.OccurrenceIndex
	meta      *store.Meta
	responded bool
	err       error
}

type notificationTestDoneMsg struct {
	sound string
	err   error
}

// respondRSVP is replaceable in tests so key routing can be exercised without
// changing a real calendar object.
var respondRSVP = rsvp.Respond

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil

	case tea.BackgroundColorMsg:
		m.isDark = msg.IsDark()
		m.styles = m.buildStyles(m.activeThemeName())
		return m, nil

	case syncDoneMsg:
		m.syncing = false
		if msg.err != nil {
			// The cached data stays on screen: stale-but-present beats empty.
			m.status = "sync failed: " + msg.err.Error()
			m.statusIsErr = true
			return m, nil
		}
		markIdentities(msg.idx, m.cfg)
		m.idx, m.meta = msg.idx, msg.meta
		m.refreshDetailOccurrence()
		m.clampCursor()
		m.status = fmt.Sprintf("synced — %d occurrences", len(m.idx.Occurrences))
		m.statusIsErr = false
		return m, nil

	case rsvpDoneMsg:
		m.syncing = false
		if msg.idx != nil && msg.meta != nil {
			markIdentities(msg.idx, m.cfg)
			m.idx, m.meta = msg.idx, msg.meta
			m.refreshDetailOccurrence()
			m.clampCursor()
		}
		if msg.err != nil {
			if msg.responded {
				m.status = "invitation updated, but sync failed: " + msg.err.Error()
			} else {
				m.status = "could not update invitation: " + msg.err.Error()
			}
			m.statusIsErr = true
			return m, nil
		}
		m.status = "invitation " + strings.ToLower(string(msg.decision)) + " — synced"
		m.statusIsErr = false
		return m, nil

	case notificationTestDoneMsg:
		m.testingNotification = false
		if msg.err != nil {
			m.status = "test notification failed: " + msg.err.Error()
			m.statusIsErr = true
			return m, nil
		}
		if msg.sound == "" {
			m.status = "test notification sent — sound disabled"
		} else {
			m.status = "test notification sent — requested sound: " + msg.sound
		}
		m.statusIsErr = false
		return m, nil

	case hiddenSaveMsg:
		if msg.err != nil {
			m.status = "could not save calendar selection: " + msg.err.Error()
			m.statusIsErr = true
			return m, nil
		}
		m.status = "calendar selection saved"
		m.statusIsErr = false
		return m, nil

	case actionMsg:
		return m.runAction(msg.id)

	case tea.KeyPressMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

// markIdentities resolves each account's configured mail address against the
// ATTENDEE list retained in the index. It runs both on startup and after a
// live sync because occurrences.json deliberately remains useful without a
// particular UI config and therefore stores participants, not a baked-in
// notion of "self".
func markIdentities(idx *store.OccurrenceIndex, cfg *config.Config) {
	if idx == nil || cfg == nil {
		return
	}
	emails := make(map[string]string, len(cfg.Accounts))
	for _, acct := range cfg.Accounts {
		emails[acct.Name] = acct.Email
	}
	for i := range idx.Occurrences {
		idx.Occurrences[i].MarkIdentity(emails[idx.Occurrences[i].AccountID])
	}
}

// navigateBack performs one non-destructive navigation step. The bool tells a
// contextual quit whether there was anything to leave; only when it is false
// is the model already on a main calendar screen and may exit.
func (m Model) navigateBack() (Model, tea.Cmd, bool) {
	switch m.view {
	case viewCalendars:
		next, cmd := m.leaveCalendars()
		return next, cmd, true
	case viewThemePicker:
		next, cmd := m.leaveThemePicker()
		return next, cmd, true
	case viewNotificationSounds:
		return m.leaveNotificationSounds(), nil, true
	case viewDetail:
		m.view = m.detailReturnView
		m.detailOccurrence = nil
		m.detailReadOnly = false
		m.detailNotice = ""
		m.clampCursor()
		return m, nil, true
	case viewDay:
		if m.dayDrilldown {
			m.view = m.dayReturnView
			m.dayDrilldown = false
			return m, nil, true
		}
	}
	if m.filterQuery != "" {
		m.filterQuery = ""
		m.filter.SetValue("")
		m.clampCursor()
		return m, nil, true
	}
	return m, nil, false
}

func (m Model) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if m.filtering {
		switch msg.String() {
		case "enter":
			m.filtering = false
			m.filterQuery = m.filter.Value()
			m.cursor = 0
			return m, nil
		case "esc":
			m.filtering = false
			m.filter.SetValue("")
			m.filterQuery = ""
			return m, nil
		}
		var cmd tea.Cmd
		m.filter, cmd = m.filter.Update(msg)
		return m, cmd
	}

	// The filter gate above runs first on purpose: while a text prompt is
	// focused the dispatcher never sees a key, so the leader cannot steal a
	// space the user is typing into a filter.
	// q mirrors esc whenever the which-key popup is open: it closes the popup
	// before it is allowed to act on the underlying view. In particular,
	// pressing space then q must never turn a discoverability overlay into a
	// surprising instant exit.
	if pending, _ := m.dispatcher.Pending(); pending != nil && msg.String() == "q" {
		m.dispatcher.Reset()
		return m, nil
	}
	if cmd, handled := m.dispatcher.Handle(msg.String(), m.keyMode()); handled {
		return m, cmd
	}

	// A key the dispatcher did not recognise falls through to the direct keys
	// below. Movement keys deliberately keep the popup open -- scrolling while
	// browsing chords is useful. Anything else that actually does something must
	// close the popup so the menu never lingers over a view it no longer
	// describes. wasPending is captured before the
	// reset so the Help case below can tell "? just closed it" from "? should
	// open it" -- by the time that case runs, Pending() would otherwise always
	// read nil.
	g, _ := m.dispatcher.Pending()
	wasPending := g != nil
	if wasPending && directKeyActs(msg, m.keys) {
		m.dispatcher.Reset()
	}

	switch {
	case key.Matches(msg, m.keys.Quit):
		// Ctrl+C remains the conventional unconditional escape hatch. Plain q
		// is contextual: it walks back through subviews before it may quit.
		if msg.String() == "ctrl+c" {
			return m.quitNow()
		}
		return m.runAction("quit")

	case key.Matches(msg, m.keys.Help):
		// ? toggles: it opens the popup, and closes it if already open, which
		// is what the help screen it replaced did. If the popup was open, the
		// fallthrough reset above (shared with every other consumed key) has
		// already closed it, so there is nothing left to do here but not
		// reopen it.
		if wasPending {
			return m, nil
		}
		cmd, _ := m.dispatcher.Handle(leaderKey, m.keyMode())
		return m, cmd

	case key.Matches(msg, m.keys.Back):
		next, cmd, _ := m.navigateBack()
		return next, cmd

	case key.Matches(msg, m.keys.Agenda):
		return m.runAction("view.agenda")
	case key.Matches(msg, m.keys.Month):
		return m.runAction("view.month")
	case key.Matches(msg, m.keys.Week):
		return m.runAction("view.week")
	case key.Matches(msg, m.keys.Day):
		return m.runAction("view.day")

	case key.Matches(msg, m.keys.Today):
		return m.runAction("goto.today")

	case key.Matches(msg, m.keys.Calendars):
		return m.runAction("calendars")

	case key.Matches(msg, m.keys.Down):
		return m.move(0, 1), nil
	case key.Matches(msg, m.keys.Up):
		return m.move(0, -1), nil
	case key.Matches(msg, m.keys.Right):
		return m.move(1, 0), nil
	case key.Matches(msg, m.keys.Left):
		return m.move(-1, 0), nil

	case key.Matches(msg, m.keys.PageDown):
		return m.page(1), nil
	case key.Matches(msg, m.keys.PageUp):
		return m.page(-1), nil

	case key.Matches(msg, m.keys.Top):
		return m.runAction("goto.first")
	case key.Matches(msg, m.keys.Bottom):
		return m.runAction("goto.last")

	case key.Matches(msg, m.keys.Enter):
		// The month grid, the week grid, and the day timeline all select a
		// DAY (focusDay) and have no per-event selection of their own, so
		// Enter drills down a level rather than opening an event: month and
		// week go to the day view, and the day view has nowhere further to
		// go. Opening viewDetail from those views would show visible()[cursor]
		// -- the agenda's cursor, untouched by any of their navigation, and
		// so almost always an event unrelated to what the user is looking at.
		// The selector has no per-event selection; enter toggles the calendar
		// under its own cursor. Space is deliberately not bound here so the
		// leader still opens the which-key popup.
		if m.view == viewThemePicker {
			return m.applyTheme()
		}
		if m.view == viewNotificationSounds {
			return m.testSelectedNotificationSound()
		}
		if m.view == viewCalendars {
			rows := calendarRows(m.meta)
			if m.calCursor >= 0 && m.calCursor < len(rows) {
				r := rows[m.calCursor]
				key := model.CalendarKey(r.Account, r.ID)
				if m.hidden == nil {
					m.hidden = map[string]bool{}
				}
				if model.IsHidden(m.hidden, r.Account, r.ID) {
					// A bare (unqualified) entry hides that calendar ID in
					// EVERY account -- model.IsHidden treats it that way, and a
					// legacy config can genuinely contain one. Deleting it
					// outright the moment ANY one of those calendars is
					// toggled back on would silently un-hide every other
					// account's same-ID calendar too, which the user never
					// touched. So before removing the bare key, migrate the
					// hiding it was providing to every OTHER row sharing that
					// ID into precise, qualified entries, then remove the bare
					// key and this row's own qualified key.
					if m.hidden[r.ID] {
						for _, other := range rows {
							if other.ID == r.ID && other.Account != r.Account {
								m.hidden[model.CalendarKey(other.Account, other.ID)] = true
							}
						}
					}
					delete(m.hidden, key)
					delete(m.hidden, r.ID) // a bare entry from an older config
				} else {
					m.hidden[key] = true
				}
				m.calDirty = true
				// visible() is filtered by m.hidden, so hiding the calendar
				// the agenda cursor currently points at can shrink (or empty)
				// the list out from under it. Re-clamp now rather than
				// leaving a stale index that would only get fixed by some
				// unrelated later re-clamp -- otherwise the highlighted
				// event silently changes underneath the user.
				m.clampCursor()
			}
			return m, nil
		}
		switch m.view {
		case viewMonth, viewWeek:
			m.dayReturnView = m.view
			m.dayDrilldown = true
			m.view = viewDay
			return m, nil
		case viewDay:
			return m, nil
		}
		if m.view != viewDetail && len(m.visible()) > 0 {
			m.detailOccurrence = nil
			m.detailReadOnly = false
			m.detailNotice = ""
			m.detailReturnView = m.view
			m.prevView, m.view = m.view, viewDetail
		}
		return m, nil

	case key.Matches(msg, m.keys.Sync):
		return m.runAction("sync")

	case key.Matches(msg, m.keys.Filter):
		return m.runAction("filter")
	}
	return m, nil
}

// movementKeySet returns the key strings (as tea.KeyPressMsg.String() would
// report them) for the bindings that may be used while the which-key popup is
// open without dismissing it. It is built from km rather than a second
// hardcoded list so the dispatcher's HandledElsewhere predicate cannot drift
// from the movement bindings actually wired into handleKey's switch.
func movementKeySet(km KeyMap) map[string]bool {
	set := map[string]bool{}
	for _, b := range []key.Binding{km.Up, km.Down, km.Left, km.Right, km.PageUp, km.PageDown} {
		for _, k := range b.Keys() {
			set[k] = true
		}
	}
	return set
}

// directKeyActs reports whether msg matches a direct binding that DOES
// something, excluding the movement keys (which may be used while the popup
// is open without dismissing it). It mirrors handleKey's switch below: a key
// that acts dismisses the popup, while an unrelated key leaves the menu
// visually stable.
func directKeyActs(msg tea.KeyPressMsg, km KeyMap) bool {
	return key.Matches(msg, km.Quit) ||
		key.Matches(msg, km.Help) ||
		key.Matches(msg, km.Back) ||
		key.Matches(msg, km.Agenda) ||
		key.Matches(msg, km.Month) ||
		key.Matches(msg, km.Week) ||
		key.Matches(msg, km.Day) ||
		key.Matches(msg, km.Today) ||
		key.Matches(msg, km.Calendars) ||
		key.Matches(msg, km.Top) ||
		key.Matches(msg, km.Bottom) ||
		key.Matches(msg, km.Enter) ||
		key.Matches(msg, km.Sync) ||
		key.Matches(msg, km.Filter)
}

func (m Model) startRSVP(decision rsvp.Decision) (Model, tea.Cmd) {
	if m.view != viewDetail {
		return m, nil
	}
	if m.detailReadOnly {
		m.status = "RSVP unavailable — not present in synced calendar"
		m.statusIsErr = true
		return m, nil
	}
	if m.syncing {
		return m, nil
	}
	o, ok := m.selected()
	if !ok || o.AttendeeStatus == "" {
		m.status = "this event is not an invitation for the configured account email"
		m.statusIsErr = true
		return m, nil
	}
	var acct *config.Account
	for i := range m.cfg.Accounts {
		if m.cfg.Accounts[i].Name == o.AccountID {
			acct = &m.cfg.Accounts[i]
			break
		}
	}
	if acct == nil {
		m.status = "calendar account " + o.AccountID + " is not configured"
		m.statusIsErr = true
		return m, nil
	}
	if acct.Email == "" {
		m.status = "set email for " + acct.Name + " in config.toml"
		m.statusIsErr = true
		return m, nil
	}
	account := *acct
	cfg := m.cfg
	s := m.store
	m.syncing = true
	m.status = "updating invitation as " + strings.ToLower(string(decision)) + "…"
	m.statusIsErr = false
	return m, func() tea.Msg {
		if err := respondRSVP(context.Background(), account, s, o, decision); err != nil {
			return rsvpDoneMsg{decision: decision, err: err}
		}
		msg := rsvpDoneMsg{decision: decision, responded: true}
		msg.err = syncChangedCalendar(context.Background(), cfg, s, o.AccountID, o.CalendarID, time.Now())
		idx, idxErr := s.LoadOccurrences()
		if idxErr == nil {
			msg.idx = idx
		} else if msg.err == nil {
			msg.err = idxErr
		}
		meta, metaErr := s.LoadMeta()
		if metaErr == nil {
			msg.meta = meta
		} else if msg.err == nil {
			msg.err = metaErr
		}
		return msg
	}
}

// move applies a horizontal (days) and vertical (rows or weeks) step. What a
// step means depends on the view: the agenda walks a cursor through the event
// list, while the calendar views move the focused date.
func (m Model) move(dx, dy int) Model {
	switch m.view {
	case viewMonth, viewWeek:
		m.focusDay = m.focusDay.AddDate(0, 0, dx+dy*7)
	case viewDay:
		m.focusDay = m.focusDay.AddDate(0, 0, dx+dy)
	case viewCalendars:
		m.calCursor += dy
		if m.calCursor < 0 {
			m.calCursor = 0
		}
		if n := len(calendarRows(m.meta)); m.calCursor >= n {
			m.calCursor = n - 1
		}
		if m.calCursor < 0 {
			m.calCursor = 0
		}
	case viewThemePicker:
		names := m.themes.Names()
		m.themeCursor += dy
		if m.themeCursor < 0 {
			m.themeCursor = 0
		}
		if n := len(names); m.themeCursor >= n {
			m.themeCursor = n - 1
		}
		if m.themeCursor < 0 {
			m.themeCursor = 0
		}
		// Live preview: every pane repaints in the candidate theme as soon
		// as the cursor lands on it, not deferred to enter.
		if m.themeCursor >= 0 && m.themeCursor < len(names) {
			m.styles = m.buildStyles(names[m.themeCursor])
		}
	case viewNotificationSounds:
		m.soundCursor += dy
		m.clampSoundCursor()
	default:
		m.cursor += dy
		m.clampCursor()
	}
	return m
}

// daysInMonth returns the number of days in a month: day 0 of the NEXT
// month is the last day of this one.
func daysInMonth(y int, m time.Month) int {
	return time.Date(y, m+1, 0, 0, 0, 0, 0, time.UTC).Day()
}

// addMonths moves by whole months, clamping the day to the target month's
// length. time.AddDate normalises overflow instead of clamping: 31 January
// plus one month is 3 March, which skips February entirely on a single
// keypress and does not come back on the way up.
func addMonths(t time.Time, n int) time.Time {
	y, mo, d := t.Date()
	// Anchor on the 1st, which can never overflow, then re-attach the day.
	first := time.Date(y, mo, 1, t.Hour(), t.Minute(), t.Second(), t.Nanosecond(), t.Location()).AddDate(0, n, 0)
	if last := daysInMonth(first.Year(), first.Month()); d > last {
		d = last
	}
	return time.Date(first.Year(), first.Month(), d, t.Hour(), t.Minute(), t.Second(), t.Nanosecond(), t.Location())
}

// page moves by a screenful in the agenda and by one period in the calendar
// views. dir is -1 for up and +1 for down.
//
// A month grid has no meaningful "screenful", which is why the unit follows the
// view rather than the renderer.
func (m Model) page(dir int) Model {
	switch m.view {
	case viewMonth:
		m.focusDay = addMonths(m.focusDay, dir)
	case viewWeek:
		m.focusDay = m.focusDay.AddDate(0, 0, 7*dir)
	case viewDay:
		m.focusDay = m.focusDay.AddDate(0, 0, dir)
	case viewNotificationSounds:
		step := m.height - 3
		if step < 1 {
			step = 1
		}
		m.soundCursor += step * dir
		m.clampSoundCursor()
	default:
		step := m.height - 3 // status and a little context
		if step < 1 {
			step = 1
		}
		m.cursor += step * dir
		m.clampCursor()
	}
	return m
}

func (m *Model) clampCursor() {
	if m.cursor < 0 {
		m.cursor = 0
	}
	if n := len(m.visible()); m.cursor >= n {
		m.cursor = n - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
}

func (m *Model) clampSoundCursor() {
	if m.soundCursor < 0 {
		m.soundCursor = 0
	}
	if n := len(m.notificationSoundOptions()); m.soundCursor >= n {
		m.soundCursor = n - 1
	}
	if m.soundCursor < 0 {
		m.soundCursor = 0
	}
}

// selected returns the occurrence under the cursor, if any. A direct-open
// detail selection wins over the visible agenda slice so hidden calendars and
// temporary external events remain addressable without changing filters.
func (m Model) selected() (model.Occurrence, bool) {
	if m.view == viewDetail && m.detailOccurrence != nil {
		return *m.detailOccurrence, true
	}
	occs := m.agendaVisible()
	if m.cursor < 0 || m.cursor >= len(occs) {
		return model.Occurrence{}, false
	}
	return occs[m.cursor], true
}

func sameOccurrence(a, b model.Occurrence) bool {
	return a.UID == b.UID &&
		a.RecurrenceID == b.RecurrenceID &&
		a.AccountID == b.AccountID &&
		a.CalendarID == b.CalendarID
}

// refreshDetailOccurrence adopts the fresh cached copy after a sync or RSVP.
// Temporary external details stay immutable and cannot accidentally acquire
// cache ownership merely because a later sync happens to produce the UID.
func (m *Model) refreshDetailOccurrence() {
	if m.detailOccurrence == nil || m.detailReadOnly || m.idx == nil {
		return
	}
	for _, occurrence := range m.idx.Occurrences {
		if sameOccurrence(occurrence, *m.detailOccurrence) {
			fresh := occurrence
			m.detailOccurrence = &fresh
			m.focusDay = fresh.Start
			return
		}
	}
}

func (m Model) View() tea.View {
	v := tea.NewView(m.render())
	v.AltScreen = true
	return v
}

// bodyLeftPad is the left inset applied uniformly to every view's body. Only
// the status bar stays flush: it is a full-width bar whose background reaches
// both edges, not text sitting beside one.
const bodyLeftPad = 1

func (m Model) render() string {
	width, height := m.width, m.height
	if width <= 0 {
		width = 80
	}
	if height <= 0 {
		height = 24
	}
	bodyHeight := height - 1 // status line
	if bodyHeight < 1 {
		bodyHeight = 1
	}

	// The body is inset by bodyLeftPad; every renderer below gets the
	// narrower width so its own internal clip still guarantees the result
	// fits, and the padding is prepended once, uniformly, after they've all
	// run -- see the join at the bottom of this function.
	bodyWidth := width - bodyLeftPad
	if bodyWidth < 1 {
		bodyWidth = 1
	}

	var body string
	switch m.view {
	case viewMonth:
		// The expanded month already previews events inside its day cells. A
		// second focused-day list below it made the grid jump whenever focus
		// moved between sparse and busy days; Enter is the stable drill-down to
		// the full day view.
		body = RenderMonth(m.visible(), m.focusDay, bodyWidth, bodyHeight, m.now, m.loc, m.styles, m.cfg.UI.WeekStart)
	case viewWeek:
		// The timeline consumes the complete body. Enter is the stable drill-down
		// to the focused day instead of repeating it in a variable-height pane.
		body = RenderWeek(m.visible(), m.focusDay, bodyWidth, bodyHeight, m.now, m.loc, m.styles, false, m.cfg.UI.WeekStart)
	case viewDay:
		// Unchanged: the day view already shows the day itself, so it gets
		// no pane -- and RenderWeek(dayOnly) fills whatever height it's given.
		body = RenderWeek(m.visible(), m.focusDay, bodyWidth, bodyHeight, m.now, m.loc, m.styles, true, m.cfg.UI.WeekStart)
	case viewDetail:
		if o, ok := m.selected(); ok {
			detailHeight := bodyHeight
			if m.detailNotice != "" && detailHeight > 0 {
				noticeStyle := m.styles.Pending
				if m.statusIsErr {
					noticeStyle = m.styles.Error
				}
				body = noticeStyle.Bold(true).Render(truncate(m.detailNotice, bodyWidth))
				detailHeight--
				if detailHeight > 0 {
					body += "\n" + RenderDetail(o, bodyWidth, detailHeight, m.loc, m.styles, m.names)
				}
			} else {
				body = RenderDetail(o, bodyWidth, detailHeight, m.loc, m.styles, m.names)
			}
		}
	case viewCalendars:
		body = RenderCalendars(m.meta, m.hidden, m.calCursor, bodyWidth, bodyHeight, m.styles)
	case viewThemePicker:
		body = RenderThemePicker(m.themes.Names(), m.themeCursor, bodyWidth, bodyHeight, m.styles)
	case viewNotificationSounds:
		body = RenderNotificationSounds(m.notificationSoundOptions(), m.soundCursor, bodyWidth, bodyHeight, m.styles)
	default:
		body = RenderAgenda(m.visible(), m.cursor, bodyWidth, bodyHeight, m.now, m.loc, m.styles, m.names, len(m.hidden) > 0)
	}

	body = padBottomLines(clampLines(body, bodyHeight), bodyHeight)
	content := strings.Join([]string{padLeftLines(body, bodyLeftPad), m.renderStatus(width)}, "\n")

	if g, path := m.dispatcher.Pending(); g != nil {
		// Match ginbox: float a rounded box above the full screen and keep the
		// status bar visible below it. One cell on either side prevents the box
		// from touching the terminal edge.
		popupWidth := width - 2
		popupHeight := height - 1 // keep the status bar available
		if popupWidth > 0 && popupHeight > 0 {
			popup := whichkey.Render(g, path, m.keyMode(), popupWidth, popupHeight, m.popupStyles())
			content = overlayBottomRight(content, popup, 1, 1, width)
		}
	}
	return content
}

// padLeftLines prepends n spaces to every line of s, including blank ones --
// the single place bodyLeftPad is actually applied, so every view's body is
// inset by construction rather than each renderer remembering to do it.
func padLeftLines(s string, n int) string {
	if s == "" || n <= 0 {
		return s
	}
	prefix := strings.Repeat(" ", n)
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		lines[i] = prefix + line
	}
	return strings.Join(lines, "\n")
}

// lineCount counts rendered lines, treating the empty string as no lines.
// padBottomLines makes the body consume its complete vertical budget so the
// status bar is always the terminal's final row, including sparse detail and
// month views. The empty string already represents one blank body row when it
// is joined with the status by render.
func padBottomLines(s string, height int) string {
	if height <= 0 {
		return ""
	}
	lines := []string{}
	if s != "" {
		lines = strings.Split(s, "\n")
	}
	for len(lines) < height {
		lines = append(lines, "")
	}
	return strings.Join(lines[:height], "\n")
}

// clampLines clips a rendered block to at most n lines.
func clampLines(s string, n int) string {
	if n <= 0 {
		return ""
	}
	lines := strings.Split(s, "\n")
	if len(lines) <= n {
		return s
	}
	return strings.Join(lines[:n], "\n")
}

// periodLabel names the active period for the status bar, and its ISO week
// where one naturally applies: a full week has exactly one Wk, but a month
// spans several and has no one Wk to carry. Views with no period of their own
// (the agenda, detail, the selectors) report "".
//
// viewDay reports "" too, deliberately: the grid's own single-day header
// (renderWeekHeader's dayOnly branch, week.go) already prints
// "Wed 10 · Wk 24".
func (m Model) periodLabel() string {
	loc := m.loc
	if loc == nil {
		loc = time.UTC
	}
	switch m.view {
	case viewMonth:
		return m.focusDay.In(loc).Format("January 2006")
	case viewWeek:
		return weekPeriodLabel(weekDays(m.focusDay.In(loc), m.cfg.UI.WeekStart))
	default:
		return ""
	}
}

// weekPeriodLabel formats a full seven-day row as its date span plus the
// row's ISO week (isoWeekForDays, week.go -- the row's Monday, regardless of
// week_start). days shorter than seven is not a shape any caller produces;
// it reports "" rather than indexing on faith.
func weekPeriodLabel(days []time.Time) string {
	if len(days) != 7 {
		return ""
	}
	start, end := days[0], days[6]
	wk := isoWeekForDays(days)
	var span string
	switch {
	case start.Year() == end.Year() && start.Month() == end.Month():
		span = fmt.Sprintf("%d–%d %s", start.Day(), end.Day(), end.Format("January 2006"))
	case start.Year() == end.Year():
		span = fmt.Sprintf("%s – %s", start.Format("2 Jan"), end.Format("2 Jan 2006"))
	default:
		span = fmt.Sprintf("%s – %s", start.Format("2 Jan 2006"), end.Format("2 Jan 2006"))
	}
	return fmt.Sprintf("%s · Wk %d", span, wk)
}

// statusBadgeNames names the view badge segment of the segmented status bar.
var statusBadgeNames = map[viewKind]string{
	viewAgenda: "Agenda", viewMonth: "Month", viewWeek: "Week",
	viewDay: "Day", viewDetail: "Event", viewCalendars: "Calendars",
	viewThemePicker: "Themes", viewNotificationSounds: "Sounds",
}

// oldestAccountLastSync returns the oldest of every account's LastSync in
// meta. An account that has never synced at all carries a zero LastSync,
// which -- being earlier than any real timestamp -- automatically wins as
// "oldest", so a single dead account among several healthy ones still
// reports honestly rather than being hidden by the others' fresher syncs.
// A meta with no accounts at all returns the zero time the same way.
func oldestAccountLastSync(meta *store.Meta) time.Time {
	var oldest time.Time
	first := true
	for _, acct := range meta.Accounts {
		if first || acct.LastSync.Before(oldest) {
			oldest = acct.LastSync
			first = false
		}
	}
	return oldest
}

// formatSyncAge renders an elapsed duration as the compact form the status
// bar's sync-age segment shows, e.g. "3m", "1h", "1d". Anything under a
// minute reads as "<1m" rather than "0m", which would misleadingly claim no
// time has passed at all.
func formatSyncAge(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "<1m"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours())/24)
	}
}

func (m Model) renderStatus(width int) string {
	if m.filtering {
		// Only the background is added, not the foreground: the filter
		// prompt's own text (bubbles' textinput.View(), already carrying
		// its own prompt/cursor/placeholder styling) must stay legible and
		// unchanged, but the bar itself -- the ground it sits on -- has to
		// be the same StatusBg every other status state uses, or the one
		// moment the user is actively typing and looking at this line is
		// exactly the moment it stops reading as a bar.
		bar := lipgloss.NewStyle().Background(m.styles.StatusBar.GetBackground())
		return truncate(bar.Render(pad(m.filter.View(), width)), width)
	}

	bg := m.styles.StatusBar.GetBackground()
	// Every non-badge segment carries the bar's own background explicitly:
	// each is rendered (and reset) independently, so without this the
	// plain-text gaps between segments would fall back to the terminal's
	// own background instead of reading as part of one continuous bar.
	dim := m.styles.StatusDim.Background(bg)
	errFg := m.styles.Error.GetForeground()

	badge := m.styles.StatusKey.Render(" " + statusBadgeNames[m.view] + " ")
	count := dim.Render(fmt.Sprintf(" %d  ", len(m.visible())))
	left := badge + count

	oldest := oldestAccountLastSync(m.meta)
	syncStyle := dim
	var syncText string
	if oldest.IsZero() {
		syncText = "never synced"
		syncStyle = syncStyle.Foreground(errFg)
	} else {
		age := m.now.Sub(oldest)
		syncText = "↺ " + formatSyncAge(age)
		if age > m.cfg.Waybar.StaleAfter {
			syncStyle = syncStyle.Foreground(errFg)
		}
	}
	qAction := "quit"
	pending, _ := m.dispatcher.Pending()
	if pending != nil || m.view == viewDetail || m.view == viewCalendars || m.view == viewThemePicker || m.view == viewNotificationSounds ||
		(m.view == viewDay && m.dayDrilldown) || m.filterQuery != "" {
		qAction = "back"
	}
	right := syncStyle.Render(syncText) + dim.Render("  space keys · q "+qAction)

	// Degrade at narrow widths rather than overflow: drop the right
	// cluster first, then the count, then the badge.
	if lipgloss.Width(left)+lipgloss.Width(right) > width {
		right = ""
	}
	if lipgloss.Width(left)+lipgloss.Width(right) > width {
		left = badge
	}
	if lipgloss.Width(left)+lipgloss.Width(right) > width {
		left = ""
	}

	statusText := m.status
	if period := m.periodLabel(); period != "" {
		if statusText == "" {
			statusText = period
		} else {
			statusText = period + " · " + statusText
		}
	}
	if m.filterQuery != "" {
		statusText = fmt.Sprintf("filter: %q · esc to clear · %s", m.filterQuery, statusText)
	}
	midStyle := lipgloss.NewStyle().Background(bg)
	if m.statusIsErr {
		// Swap in only the foreground: the bar's own background (StatusBg)
		// is what makes it read as a bar rather than plain text, and an
		// error is still a status, not a different kind of chrome.
		midStyle = midStyle.Foreground(errFg).Bold(true)
	}
	// Reserve one cell between the middle and the right cluster: a status
	// message long enough to fill the middle exactly would otherwise abut
	// the sync glyph with no separator at all. Only reserved when there is
	// room for it (avail > 0) and a right cluster survived degradation --
	// otherwise the reservation itself would push the bar past width, and
	// the width invariant always wins over the cosmetic gap.
	avail := width - lipgloss.Width(left) - lipgloss.Width(right)
	gapW := 0
	if right != "" && avail > 0 {
		gapW = 1
	}
	midW := avail - gapW
	if midW < 0 {
		midW = 0
	}
	// Pad to width BEFORE styling so the background fills the entire
	// segment, not just the text -- the same "pad inside the style" shape
	// focusedCellStyle (month.go) uses for the full-cell inversion.
	mid := midStyle.Render(pad(truncate(statusText, midW), midW))
	gap := ""
	if gapW > 0 {
		gap = lipgloss.NewStyle().Background(bg).Render(" ")
	}

	return truncate(left+mid+gap+right, width)
}

// syncCmd runs a sync in the background. Sync must never block rendering, so
// this returns a tea.Cmd rather than doing the work inline.
func (m Model) syncCmd() tea.Cmd {
	cfg, s, now := m.cfg, m.store, m.now
	return func() tea.Msg {
		if err := syncAll(context.Background(), cfg, s, now); err != nil {
			return syncDoneMsg{err: err}
		}
		idx, err := s.LoadOccurrences()
		if err != nil {
			return syncDoneMsg{err: err}
		}
		meta, err := s.LoadMeta()
		if err != nil {
			return syncDoneMsg{err: err}
		}
		return syncDoneMsg{idx: idx, meta: meta}
	}
}

// leaveCalendars restores the previous view and, if a toggle changed the
// selection since it was last saved, returns the command that persists it.
// Every route out of the selector -- esc, switching straight to another
// view, or pressing the calendars key a second time while already inside it
// -- funnels through this one helper, so no exit route can silently drop a
// pending save. calDirty is cleared here and ONLY here, once a save has
// actually been issued for it.
func (m Model) leaveCalendars() (Model, tea.Cmd) {
	m.view = m.prevView
	if !m.calDirty {
		return m, nil
	}
	m.calDirty = false
	return m, m.saveHiddenCmd()
}

// leaveThemePicker leaves the picker WITHOUT persisting: it rebuilds Styles
// from the committed theme (m.themeName), discarding whatever candidate was
// being previewed. Every route out of the picker that isn't a successful
// enter -- esc, switching straight to another view, pressing the theme key a
// second time while already inside it -- funnels through this one helper,
// so a live preview can never leak out as the active look without having
// gone through applyTheme's persistence.
func (m Model) leaveThemePicker() (Model, tea.Cmd) {
	m.view = m.prevView
	m.styles = m.buildStyles(m.themeName)
	return m, nil
}

func (m Model) openNotificationSounds() (Model, tea.Cmd) {
	if m.view == viewNotificationSounds {
		return m.leaveNotificationSounds(), nil
	}

	var leaveCmd tea.Cmd
	switch m.view {
	case viewCalendars:
		m, leaveCmd = m.leaveCalendars()
	case viewThemePicker:
		m, _ = m.leaveThemePicker()
	}
	m.prevView, m.view = m.view, viewNotificationSounds
	m.soundCursor = 0
	return m, leaveCmd
}

func (m Model) leaveNotificationSounds() Model {
	m.view = m.prevView
	return m
}

func (m Model) testSelectedNotificationSound() (Model, tea.Cmd) {
	if m.testingNotification {
		return m, nil
	}
	options := m.notificationSoundOptions()
	if m.soundCursor < 0 || m.soundCursor >= len(options) {
		return m, nil
	}
	sound := options[m.soundCursor].Sound
	m.testingNotification = true
	m.status = "sending test notification…"
	if sound != "" {
		m.status = "testing sound: " + sound
	}
	m.statusIsErr = false
	return m, m.notificationTestCmd(sound)
}

// applyTheme commits the theme under the picker's cursor: it stays the
// active preview, and config.SetTheme persists it as the fully-prefixed
// canonical name (e.g. "system:catppuccin-mocha") -- the same form Get
// expects, so a later Resolve at startup isn't needed to find it again.
//
// The write happens synchronously here, in Update, rather than as a tea.Cmd
// goroutine. That is deliberate: Bubble Tea does not wait on command
// goroutines at exit, so if the write were deferred, a quit fired on the
// very next keypress could race it and the applied theme would silently
// fail to survive the exit -- exactly the defect an earlier selector in
// this project shipped for its toggles. Writing synchronously means the
// file is already saved (or the failure already reported) by the time this
// call returns, before any subsequent keypress -- including quit -- is even
// read.
func (m Model) applyTheme() (Model, tea.Cmd) {
	names := m.themes.Names()
	if m.themeCursor < 0 || m.themeCursor >= len(names) {
		m.view = m.prevView
		return m, nil
	}
	name := names[m.themeCursor]
	m.styles = m.buildStyles(name) // already what's previewed; rebuilt for certainty
	m.view = m.prevView

	if m.configPath == "" {
		// No config path means a test-built Model (or some other caller that
		// opted out of persistence). The theme still applies for this
		// session; it just isn't saved.
		m.themeName = name
		m.status = "theme applied (not saved: no config path)"
		m.statusIsErr = false
		return m, nil
	}
	if err := config.SetTheme(m.configPath, name); err != nil {
		// The write failed, but the previewed styles above are already a
		// complete, valid Styles -- nothing about the model is corrupted,
		// only the persistence didn't happen.
		m.status = "could not save theme: " + err.Error()
		m.statusIsErr = true
		return m, nil
	}
	m.themeName = name
	m.cfg.UI.Theme = name
	m.status = "theme set to " + name
	m.statusIsErr = false
	return m, nil
}

// hiddenSaveMsg reports the outcome of writing the selection to the config.
type hiddenSaveMsg struct{ err error }

// saveHiddenCmd writes the live selection back to the config file. It runs as a
// command rather than inline so a slow or failing write never blocks a render;
// the selection is already applied in memory either way.
func (m Model) saveHiddenCmd() tea.Cmd {
	path := m.configPath
	hidden := make([]string, 0, len(m.hidden))
	for k := range m.hidden {
		hidden = append(hidden, k)
	}
	sort.Strings(hidden) // a stable order keeps the config diff small
	return func() tea.Msg {
		if path == "" {
			return hiddenSaveMsg{err: errNoConfigPath}
		}
		return hiddenSaveMsg{err: config.SetHidden(path, hidden)}
	}
}

// errNoConfigPath means the model was built without a config path, which
// happens in tests. Saving is skipped rather than guessed at.
var errNoConfigPath = errors.New("no config path: selection not saved")

// SyncFunc performs a full sync. It is a package variable so that cmd/calterm
// can supply the implementation without internal/tui importing it, and so
// tests can substitute a stub.
var SyncFunc func(ctx context.Context, cfg *config.Config, s *store.Store, now time.Time) error

// SyncCalendarFunc refreshes one already-discovered calendar after an RSVP
// write. Command wiring supplies the targeted implementation; falling back to
// SyncFunc keeps embedders compatible while still producing correct data.
var SyncCalendarFunc func(ctx context.Context, cfg *config.Config, s *store.Store, accountID, calendarID string, now time.Time) error

// sendTestNotification is replaceable in tests so exercising the key binding
// never contacts the real desktop session.
var sendTestNotification = desktop.NotifyWithDuration

func (m Model) notificationTestCmd(sound string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		err := sendTestNotification(ctx, "Calterm notification test", "Desktop notifications are working.", sound, m.cfg.Notifications.Duration)
		return notificationTestDoneMsg{
			sound: sound,
			err:   err,
		}
	}
}

func (m Model) notificationSoundOptions() []notificationSoundOption {
	return notificationSoundOptions(m.cfg.Notifications, m.installedSounds)
}

func syncAll(ctx context.Context, cfg *config.Config, s *store.Store, now time.Time) error {
	if SyncFunc == nil {
		return fmt.Errorf("sync is not wired up")
	}
	return SyncFunc(ctx, cfg, s, now)
}

func syncChangedCalendar(ctx context.Context, cfg *config.Config, s *store.Store, accountID, calendarID string, now time.Time) error {
	if SyncCalendarFunc != nil {
		return SyncCalendarFunc(ctx, cfg, s, accountID, calendarID, now)
	}
	return syncAll(ctx, cfg, s, now)
}

// Run loads the cache and starts the interactive program.
func Run(cfg *config.Config, s *store.Store, now time.Time, configPath string) error {
	idx, err := s.LoadOccurrences()
	if err != nil {
		return err
	}
	meta, err := s.LoadMeta()
	if err != nil {
		meta = &store.Meta{}
	}
	m := New(cfg, s, idx, meta, now)
	return runModel(m, configPath)
}

// RunFocused loads the same cache and starts the same program as Run, but
// enters directly into opts.Occurrence's detail view. Keeping this separate
// preserves Run's API for existing callers.
func RunFocused(cfg *config.Config, s *store.Store, now time.Time, configPath string, opts FocusOptions) error {
	idx, err := s.LoadOccurrences()
	if err != nil {
		return err
	}
	meta, err := s.LoadMeta()
	if err != nil {
		meta = &store.Meta{}
	}
	m := NewFocused(cfg, s, idx, meta, now, opts)
	return runModel(m, configPath)
}

func runModel(m Model, configPath string) error {
	m = m.loadThemes(userThemesDir())
	m.installedSounds = desktop.SoundNames()
	m.configPath = configPath
	p := tea.NewProgram(m)
	_, err := p.Run()
	return err
}
