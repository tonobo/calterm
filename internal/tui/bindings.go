package tui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/tonobo/calterm/internal/rsvp"
	"github.com/tonobo/calterm/internal/tui/keys"
)

// leaderKey opens the chord tree. Space matches ginbox, so muscle memory
// carries between the two TUIs.
//
// This is "space", not the literal " " rune. Bubble Tea's KeyPressMsg.String()
// -- what handleKey feeds the dispatcher -- reports a space keypress as the
// string "space" for both {Code: ' '} and {Code: tea.KeySpace} (verified
// against the pinned charm.land/bubbletea/v2 v2.0.9). Keying chordRoot's map
// on " " would mean a literal space press is never found in the tree.
const leaderKey = "space"

// actionMsg asks the model to run a named action.
//
// The chord tree emits these rather than closing over the Model. Model is a
// value type, so a captured closure could not mutate the live model anyway --
// and keeping the tree data-only means chordRoot() can be built and walked in
// tests with no model in sight.
type actionMsg struct{ id string }

func action(id, desc string, modes ...keys.Mode) *keys.Command {
	return &keys.Command{
		ID:          id,
		Description: desc,
		Modes:       modes,
		Run: func() tea.Cmd {
			return func() tea.Msg { return actionMsg{id: id} }
		},
	}
}

// chordRoot is calterm's chord tree. Both chord and direct-key routes funnel
// through runAction, which is what stops overlapping bindings drifting apart.
func chordRoot() *keys.Root {
	return &keys.Root{Entries: map[string]keys.Entry{
		leaderKey: &keys.Group{
			Description: "leader",
			Entries: map[string]keys.Entry{
				"v": &keys.Group{
					Description: "+view",
					Entries: map[string]keys.Entry{
						"a": action("view.agenda", "agenda"),
						"m": action("view.month", "month"),
						"w": action("view.week", "week"),
						"d": action("view.day", "day"),
					},
				},
				"g": &keys.Group{
					Description: "+goto",
					Entries: map[string]keys.Entry{
						"t": action("goto.today", "today"),
						"g": action("goto.first", "first event"),
						"G": action("goto.last", "last event"),
					},
				},
				"s": action("sync", "sync now"),
				"n": action("notification.sounds", "sounds · enter to test"),
				"r": &keys.Group{
					Description: "+respond",
					Entries: map[string]keys.Entry{
						"a": action("rsvp.accept", "accept", keys.Mode(viewDetail)),
						"d": action("rsvp.decline", "decline", keys.Mode(viewDetail)),
					},
				},
				"f": action("filter", "filter events"),
				"c": action("calendars", "choose calendars"),
				"t": action("theme", "choose theme"),
			},
		},
	}}
}

// modeOf maps the active view to a dispatcher mode. The keys package never
// interprets a Mode beyond comparing it, so this is just an identity that
// keeps the coupling explicit and greppable.
func modeOf(v viewKind) keys.Mode { return keys.Mode(v) }

// readOnlyDetailMode retains every global command but gates out the RSVP-only
// leaves whose mode is viewDetail. That makes the respond group disappear for
// a temporary external ICS instead of advertising actions which cannot run.
const readOnlyDetailMode keys.Mode = -1

func (m Model) keyMode() keys.Mode {
	if m.view == viewDetail && m.detailReadOnly {
		return readOnlyDetailMode
	}
	return modeOf(m.view)
}

// actionTable is the single source of truth for what an action ID does.
// hasAction and runAction both derive from it rather than restating it in a
// second switch: two switches kept in sync by hand is exactly the drift this
// whole leader/direct-key funnel exists to prevent, and a hand-written
// membership checker would have been the first thing to rot.
//
// It is a package-level var, not a function rebuilding the map per call: the
// closures below capture nothing (Model arrives as a parameter), so building
// the table once is safe and avoids reallocating it on every keypress.
var actionTable = map[string]func(Model) (Model, tea.Cmd){
	"view.agenda": func(m Model) (Model, tea.Cmd) { return m.switchMainView(viewAgenda), nil },
	"view.month":  func(m Model) (Model, tea.Cmd) { return m.switchMainView(viewMonth), nil },
	"view.week":   func(m Model) (Model, tea.Cmd) { return m.switchMainView(viewWeek), nil },
	"view.day":    func(m Model) (Model, tea.Cmd) { return m.switchMainView(viewDay), nil },

	// The calendar views track focusDay; the agenda tracks the cursor, and for
	// it "today" means the same first-non-past occurrence the app opens on --
	// resetting to 0 would jump a month backwards.
	"goto.today": func(m Model) (Model, tea.Cmd) {
		m.focusDay = m.now
		m.cursor = firstNonPast(m.agendaVisible(), m.now, m.loc)
		return m, nil
	},
	"goto.first": func(m Model) (Model, tea.Cmd) { m.cursor = 0; return m, nil },
	"goto.last": func(m Model) (Model, tea.Cmd) {
		m.cursor = len(m.visible()) - 1
		m.clampCursor()
		return m, nil
	},

	"sync": func(m Model) (Model, tea.Cmd) {
		if m.syncing || m.store == nil {
			return m, nil
		}
		m.syncing = true
		m.status = "syncing…"
		m.statusIsErr = false
		return m, m.syncCmd()
	},
	"notification.sounds": func(m Model) (Model, tea.Cmd) { return m.openNotificationSounds() },
	"rsvp.accept":         func(m Model) (Model, tea.Cmd) { return m.startRSVP(rsvp.Accepted) },
	"rsvp.decline":        func(m Model) (Model, tea.Cmd) { return m.startRSVP(rsvp.Declined) },

	"filter": func(m Model) (Model, tea.Cmd) {
		m.filtering = true
		m.filter.Focus()
		return m, nil
	},

	// Pressing c while already in the selector must close it (and save, if
	// dirty), not re-enter -- re-entering would overwrite prevView with
	// viewCalendars and trap esc for the rest of the session. calDirty is
	// deliberately NOT reset on entry: it should already be false (every exit
	// route clears it only once a save has actually been issued), and
	// resetting it unconditionally here would drop a save that is still owed.
	"calendars": func(m Model) (Model, tea.Cmd) {
		if m.view == viewCalendars {
			return m.leaveCalendars()
		}
		// Opening the calendar selector straight out of the theme picker must
		// restore the preview first -- otherwise the picker's live-swapped
		// styles would be left in place, applied to nothing, the same way an
		// unguarded switch away from the calendar selector used to drop a
		// pending toggle.
		if m.view == viewThemePicker {
			m, _ = m.leaveThemePicker()
		}
		if m.view == viewNotificationSounds {
			m = m.leaveNotificationSounds()
		}
		m.prevView, m.view = m.view, viewCalendars
		m.calCursor = 0
		return m, nil
	},

	// space t opens the theme picker. Pressing it again while already open
	// closes it, mirroring "calendars" above rather than re-entering (which
	// would clobber prevView and trap esc).
	"theme": func(m Model) (Model, tea.Cmd) {
		if m.view == viewThemePicker {
			return m.leaveThemePicker()
		}
		if m.view == viewCalendars {
			var leaveCmd tea.Cmd
			m, leaveCmd = m.leaveCalendars()
			m.prevView, m.view = m.view, viewThemePicker
			m.themeCursor = indexOf(m.themes.Names(), m.themeName)
			return m, leaveCmd
		}
		if m.view == viewNotificationSounds {
			m = m.leaveNotificationSounds()
		}
		m.prevView, m.view = m.view, viewThemePicker
		m.themeCursor = indexOf(m.themes.Names(), m.themeName)
		return m, nil
	},

	"quit": func(m Model) (Model, tea.Cmd) { return m.quitOrBack() },
}

// switchMainView is an explicit top-level navigation. It clears drill-down
// state so q exits from the selected main view instead of following stale
// history left by a detail or day opened earlier in the session.
func (m Model) switchMainView(view viewKind) Model {
	m.view = view
	m.dayDrilldown = false
	m.detailOccurrence = nil
	m.detailReadOnly = false
	m.detailNotice = ""
	return m
}

// quitOrBack gives q the same first step as esc. Only a main view with no
// applied filter has no back target and therefore exits.
func (m Model) quitOrBack() (Model, tea.Cmd) {
	if next, cmd, handled := m.navigateBack(); handled {
		return next, cmd
	}
	return m.quitNow()
}

// quitNow is the unconditional exit used once q reaches a main view and by
// Ctrl+C. A pending calendar toggle is persisted before Bubble Tea exits.
func (m Model) quitNow() (Model, tea.Cmd) {
	if m.calDirty {
		m.calDirty = false
		return m, tea.Sequence(m.saveHiddenCmd(), tea.Quit)
	}
	return m, tea.Quit
}

// viewChangingActions are the actions that switch m.view outright. When one
// of these fires while the selector is open, the selector must be left (and
// a pending toggle saved) first -- see runAction.
var viewChangingActions = map[string]bool{
	"view.agenda": true,
	"view.month":  true,
	"view.week":   true,
	"view.day":    true,
}

// hasAction reports whether id reaches a case in runAction. It exists so a
// test can prove no chord is wired to nothing.
func (m Model) hasAction(id string) bool {
	_, ok := actionTable[id]
	return ok
}

// runAction is the single place an action is carried out, whether it arrived
// from a direct key or from a chord. An unknown id is left unhandled rather
// than panicking.
//
// A view-changing action fired while EITHER the calendar selector or the
// theme picker is open must leave it first -- otherwise switching straight
// from the calendar selector to, say, month view would silently drop a
// pending toggle instead of saving it, and switching straight from the theme
// picker would leave a live preview applied to nothing instead of restoring
// the committed theme. This is the one interception point that covers every
// such action for both selectors without threading the check through each
// of their closures individually.
func (m Model) runAction(id string) (tea.Model, tea.Cmd) {
	fn, ok := actionTable[id]
	if !ok {
		return m, nil
	}
	if viewChangingActions[id] {
		switch m.view {
		case viewCalendars:
			var leaveCmd tea.Cmd
			m, leaveCmd = m.leaveCalendars()
			next, cmd := fn(m)
			return next, tea.Batch(leaveCmd, cmd)
		case viewThemePicker:
			// Leaving the picker for an ordinary view discards the live
			// preview (nothing was applied), restoring the committed theme --
			// the same "esc genuinely restores" contract, just reached by a
			// different key.
			m, _ = m.leaveThemePicker()
		}
	}
	return fn(m)
}
