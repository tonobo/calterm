package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/tonobo/calterm/internal/store"
	"github.com/tonobo/calterm/internal/tui/keys"
)

// The tree is data-only, so it can be walked without a Model.
func TestChordRootHasTheDocumentedShape(t *testing.T) {
	root := chordRoot()
	leader, ok := root.Entries[leaderKey].(*keys.Group)
	if !ok {
		t.Fatal("the leader key does not open a group")
	}
	view, ok := leader.Entries["v"].(*keys.Group)
	if !ok {
		t.Fatal("<leader>v is not a group")
	}
	for _, k := range []string{"a", "m", "w", "d"} {
		if _, ok := view.Entries[k].(*keys.Command); !ok {
			t.Errorf("<leader>v%s is not a command", k)
		}
	}
	goTo, ok := leader.Entries["g"].(*keys.Group)
	if !ok {
		t.Fatal("<leader>g is not a group")
	}
	for _, k := range []string{"t", "g", "G"} {
		if _, ok := goTo.Entries[k].(*keys.Command); !ok {
			t.Errorf("<leader>g%s is not a command", k)
		}
	}
	for _, k := range []string{"s", "f"} {
		if _, ok := leader.Entries[k].(*keys.Command); !ok {
			t.Errorf("<leader>%s is not a command", k)
		}
	}
	if _, ok := leader.Entries["q"]; ok {
		t.Error("q must not be a leader command: while the popup is open it means back")
	}
	respond, ok := leader.Entries["r"].(*keys.Group)
	if !ok {
		t.Fatal("<leader>r is not a response group")
	}
	for _, k := range []string{"a", "d"} {
		cmd, ok := respond.Entries[k].(*keys.Command)
		if !ok {
			t.Errorf("<leader>r%s is not a command", k)
			continue
		}
		if len(cmd.Modes) != 1 || cmd.Modes[0] != keys.Mode(viewDetail) {
			t.Errorf("<leader>r%s modes = %v, want detail only", k, cmd.Modes)
		}
	}
}

// Every command must reach an entry in actionTable, which is what runAction
// looks up -- an ID missing there would be a chord that silently does
// nothing. hasAction and runAction now derive from the same table, so this
// check and what runAction actually does cannot drift apart.
func TestEveryChordCommandHasAnAction(t *testing.T) {
	m := testModel(t)
	var walk func(g *keys.Group)
	walk = func(g *keys.Group) {
		for _, e := range g.Entries {
			switch t2 := e.(type) {
			case *keys.Group:
				walk(t2)
			case *keys.Command:
				if !m.hasAction(t2.ID) {
					t.Errorf("chord command %q has no case in runAction", t2.ID)
				}
				if _, ok := actionTable[t2.ID]; !ok {
					t.Errorf("chord command %q is not in actionTable, so runAction cannot reach it", t2.ID)
				}
			}
		}
	}
	walk(chordRoot().Entries[leaderKey].(*keys.Group))
}

func TestLeaderOpensThePopup(t *testing.T) {
	m := testModel(t)
	m = press(t, m, " ")
	if g, _ := m.dispatcher.Pending(); g == nil {
		t.Fatal("the leader did not open the popup")
	}
	if !strings.Contains(m.View().Content, "+view") {
		t.Errorf("the popup should be visible in the rendered view:\n%s", m.View().Content)
	}
}

func TestChordSwitchesView(t *testing.T) {
	m := testModel(t)
	next, _ := m.Update(tea.KeyPressMsg{Code: ' ', Text: " "})
	m = next.(Model)
	next, _ = m.Update(tea.KeyPressMsg{Code: 'v', Text: "v"})
	m = next.(Model)
	next, cmd := m.Update(tea.KeyPressMsg{Code: 'm', Text: "m"})
	m = next.(Model)
	if cmd == nil {
		t.Fatal("completing a chord produced no command")
	}
	next, _ = m.Update(cmd())
	m = next.(Model)
	if m.view != viewMonth {
		t.Errorf("view = %v, want month", m.view)
	}
	if g, _ := m.dispatcher.Pending(); g != nil {
		t.Error("the popup should close once the chord completes")
	}
}

// The leader must never reach the dispatcher while the filter is open, or it
// would be impossible to type a space into a filter -- and calterm's filters
// match event titles, where spaces are the common case.
func TestFilterSwallowsTheLeader(t *testing.T) {
	m := testModel(t)
	m = press(t, m, "/")
	if !m.filtering {
		t.Fatal("/ did not open the filter")
	}
	next, _ := m.Update(tea.KeyPressMsg{Code: ' ', Text: " "})
	m = next.(Model)
	if g, _ := m.dispatcher.Pending(); g != nil {
		t.Error("the leader opened the popup while the filter was focused")
	}
	if !strings.Contains(m.filter.Value(), " ") {
		t.Errorf("space should have been typed into the filter, value = %q", m.filter.Value())
	}
}

func TestQuestionMarkOpensThePopup(t *testing.T) {
	m := testModel(t)
	m = press(t, m, "?")
	if g, _ := m.dispatcher.Pending(); g == nil {
		t.Error("? should open the popup now that the help view is gone")
	}
}

// ? toggles: opens the popup, and closes it again rather than reporting "no
// binding for space" -- which is what the old flat help screen did too.
// MINOR 4 in the final review.
func TestQuestionMarkTogglesThePopupClosed(t *testing.T) {
	m := testModel(t)
	m = press(t, m, "?")
	if g, _ := m.dispatcher.Pending(); g == nil {
		t.Fatal("? should have opened the popup")
	}
	m = press(t, m, "?")
	if g, _ := m.dispatcher.Pending(); g != nil {
		t.Error("? should have closed the popup on the second press")
	}
	if strings.Contains(m.View().Content, "no binding for space") {
		t.Errorf("? must never surface \"no binding for space\":\n%s", m.View().Content)
	}
}

func TestEscClosesThePopup(t *testing.T) {
	m := testModel(t)
	m = press(t, m, " ")
	next, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = next.(Model)
	if g, _ := m.dispatcher.Pending(); g != nil {
		t.Error("esc should close the popup")
	}
}

func TestQClosesThePopupWithoutQuitting(t *testing.T) {
	for _, path := range [][]string{{" "}, {" ", "v"}} {
		m := testModel(t)
		m = press(t, m, path...)
		if g, _ := m.dispatcher.Pending(); g == nil {
			t.Fatalf("test setup %v did not open the popup", path)
		}
		next, cmd := m.Update(tea.KeyPressMsg{Code: 'q', Text: "q"})
		m = next.(Model)
		if cmd != nil {
			t.Errorf("q at popup path %v returned a command; it must only close the popup", path)
		}
		if g, _ := m.dispatcher.Pending(); g != nil {
			t.Errorf("q at popup path %v left the popup open", path)
		}
	}
}

// An unbound key mid-chord must still reach the view underneath.
func TestUnknownKeyMidChordStillMovesTheCursor(t *testing.T) {
	m := testModel(t)
	start := m.cursor
	m = press(t, m, " ")
	m = press(t, m, "j")
	if m.cursor == start {
		t.Error("j mid-chord should still move the cursor")
	}
	if g, _ := m.dispatcher.Pending(); g == nil {
		t.Error("the popup should stay open after an unbound key")
	}
}

// The ginbox-style root footer stays compact and complete at narrow widths.
func TestRootFooterFitsAnEightyColumnTerminal(t *testing.T) {
	m := testModel(t)
	m = press(t, m, " ")
	lines := strings.Split(m.View().Content, "\n")
	var footer string
	for _, l := range lines {
		if strings.Contains(stripANSI(l), "q/esc close") {
			footer = l
			break
		}
	}
	if footer == "" {
		t.Fatal("could not find the footer line in the rendered popup")
	}
	plain := stripANSI(footer)
	if w := lipgloss.Width(plain); w > 80 {
		t.Errorf("footer width = %d, want <= 80:\n%q", w, plain)
	}
	if !strings.Contains(plain, "bksp back") {
		t.Errorf("footer is truncated, want both complete actions:\n%q", plain)
	}
}

// Decorative blank rows are dropped before commands at a small height, so all
// reachable root entries remain visible whenever they physically fit.
func TestPopupShowsAllRootEntriesAtASmallTerminal(t *testing.T) {
	m := testModel(t)
	next, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 12})
	m = next.(Model)
	m = press(t, m, " ")
	got := m.View().Content
	for _, want := range []string{"+view", "+goto", "sync now", "filter events", "choose theme"} {
		if !strings.Contains(got, want) {
			t.Errorf("80x12 popup is missing %q:\n%s", want, got)
		}
	}
}

// The overflow indicator must still fire when the tree genuinely does not
// fit -- the fix must not simply remove clipping altogether.
func TestPopupStillOverflowsAtAVeryShortTerminal(t *testing.T) {
	m := testModel(t)
	next, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 8})
	m = next.(Model)
	m = press(t, m, " ")
	got := m.View().Content
	if !strings.Contains(got, "more") {
		t.Errorf("a very short terminal should still show a +N more overflow indicator:\n%s", got)
	}
}

// A fall-through key that actually does something must close the popup:
// leaving it open would show "no binding for m" directly above a view that m
// just switched to. IMPORTANT 1 in the final review.
func TestPopupClosesWhenFallThroughKeyActs(t *testing.T) {
	t.Run("m switches the view and closes the popup", func(t *testing.T) {
		m := testModel(t)
		m = press(t, m, " ")
		m = press(t, m, "m")
		if m.view != viewMonth {
			t.Errorf("view = %v, want month", m.view)
		}
		if g, _ := m.dispatcher.Pending(); g != nil {
			t.Error("the popup should have closed once m switched the view")
		}
	})

	t.Run("r starts a sync and closes the popup", func(t *testing.T) {
		m := testModel(t)
		m.store = store.New(t.TempDir())
		m = press(t, m, " ")
		m = press(t, m, "r")
		if !m.syncing {
			t.Error("r should have started a sync")
		}
		if g, _ := m.dispatcher.Pending(); g != nil {
			t.Error("the popup should have closed once r started the sync")
		}
	})

	t.Run("enter opens the detail view and closes the popup", func(t *testing.T) {
		m := testModel(t)
		m = press(t, m, " ")
		next, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
		m = next.(Model)
		if m.view != viewDetail {
			t.Errorf("view = %v, want the detail view", m.view)
		}
		if g, _ := m.dispatcher.Pending(); g != nil {
			t.Error("the popup should have closed once enter opened the detail view")
		}
	})

	t.Run("/ opens the filter and closes the popup, even mid-filter", func(t *testing.T) {
		m := testModel(t)
		m = press(t, m, " ")
		m = press(t, m, "/")
		if !m.filtering {
			t.Error("/ should have opened the filter")
		}
		if g, _ := m.dispatcher.Pending(); g != nil {
			t.Error("the popup should have closed once / opened the filter")
		}
	})
}

// Movement keys are the deliberate exception: scrolling the list while
// browsing chords is a feature, so they must leave the popup open. Pinned so
// a later change to IMPORTANT 1's fix cannot quietly remove the exception.
func TestPopupStaysOpenForMovementKeysMidChord(t *testing.T) {
	for _, k := range []string{"j", "k"} {
		m := testModel(t)
		m = press(t, m, " ")
		m = press(t, m, k)
		if g, _ := m.dispatcher.Pending(); g == nil {
			t.Errorf("key %q should leave the popup open", k)
		}
	}
	for _, code := range []tea.Key{{Code: tea.KeyPgUp}, {Code: tea.KeyPgDown}} {
		m := testModel(t)
		m = press(t, m, " ")
		next, _ := m.Update(tea.KeyPressMsg(code))
		m = next.(Model)
		if g, _ := m.dispatcher.Pending(); g == nil {
			t.Errorf("key %v should leave the popup open", code)
		}
	}
}

// Direct keys must keep working exactly as before.
func TestDirectKeysStillWork(t *testing.T) {
	m := testModel(t)
	m = press(t, m, "m")
	if m.view != viewMonth {
		t.Errorf("direct m: view = %v, want month", m.view)
	}
	m = press(t, m, "a")
	if m.view != viewAgenda {
		t.Errorf("direct a: view = %v, want agenda", m.view)
	}
}
