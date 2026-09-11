package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// While the which-key popup is open, an unrecognised key falls through to the
// direct-key switch in handleKey. There are exactly three outcomes: the key
// ACTS and closes the popup, MOVES while leaving it open, or does NOTHING and
// leaves it open. Like ginbox, the overlay stays visually stable for unmatched
// terminal shortcuts instead of inserting a transient warning row.
func TestPopupFallthroughBehaviour(t *testing.T) {
	t.Run("acts: popup closes, no warning needed", func(t *testing.T) {
		for _, tc := range []struct {
			name  string
			press func(m Model) Model
		}{
			{"m", func(m Model) Model { return press(t, m, "m") }},
			{"r", func(m Model) Model {
				m.store = nil // sync guards on nil store, still consumed and closes the popup
				return press(t, m, "r")
			}},
			{"/", func(m Model) Model { return press(t, m, "/") }},
			{"enter", func(m Model) Model {
				next, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
				return next.(Model)
			}},
		} {
			t.Run(tc.name, func(t *testing.T) {
				m := testModel(t)
				m = press(t, m, " ")
				m = tc.press(m)
				if g, _ := m.dispatcher.Pending(); g != nil {
					t.Errorf("%s should have closed the popup", tc.name)
				}
			})
		}
	})

	t.Run("moves: popup stays open, no warning", func(t *testing.T) {
		presses := map[string]func(m Model) Model{
			"j": func(m Model) Model { return press(t, m, "j") },
			"k": func(m Model) Model { return press(t, m, "k") },
			"pgdown": func(m Model) Model {
				next, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyPgDown})
				return next.(Model)
			},
		}
		for name, do := range presses {
			t.Run(name, func(t *testing.T) {
				m := testModel(t)
				m = press(t, m, " ")
				m = do(m)
				g, _ := m.dispatcher.Pending()
				if g == nil {
					t.Fatalf("%s should leave the popup open", name)
				}
				if strings.Contains(m.View().Content, "no binding") {
					t.Errorf("%s moved the cursor/date; must not show \"no binding\":\n%s", name, m.View().Content)
				}
			})
		}
	})

	t.Run("inert: popup stays open without changing layout", func(t *testing.T) {
		for _, k := range []string{"z", "5"} {
			t.Run(k, func(t *testing.T) {
				m := testModel(t)
				m = press(t, m, " ")
				m = press(t, m, k)
				g, _ := m.dispatcher.Pending()
				if g == nil {
					t.Fatalf("%s should leave the popup open", k)
				}
				if strings.Contains(m.View().Content, "no binding") {
					t.Errorf("%s should not inject a warning row into the popup:\n%s", k, m.View().Content)
				}
			})
		}
	})
}

// ? must keep toggling the popup closed on a second press even after the
// fallthrough-gate fix, not just reset-then-reopen.
func TestQuestionMarkStillTogglesClosedAfterFix(t *testing.T) {
	m := testModel(t)
	m = press(t, m, "?")
	if g, _ := m.dispatcher.Pending(); g == nil {
		t.Fatal("? should have opened the popup")
	}
	m = press(t, m, "?")
	if g, _ := m.dispatcher.Pending(); g != nil {
		t.Error("? should have closed the popup on the second press")
	}
}
