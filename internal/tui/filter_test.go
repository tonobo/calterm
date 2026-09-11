package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestFilterMatchesSummaryAndLocation(t *testing.T) {
	occs := agendaFixture()
	occs[0].Location = "Room 2"

	if got := Filter(occs, "standup"); len(got) != 1 || got[0].Summary != "Standup" {
		t.Errorf("case-insensitive summary match failed: %+v", got)
	}
	if got := Filter(occs, "room"); len(got) != 1 {
		t.Errorf("location match returned %d results, want 1", len(got))
	}
	if got := Filter(occs, ""); len(got) != len(occs) {
		t.Errorf("an empty query returned %d of %d", len(got), len(occs))
	}
	if got := Filter(occs, "zzz"); len(got) != 0 {
		t.Errorf("a non-matching query returned %d results", len(got))
	}
}

func TestFilterKeyOpensPromptAndCapturesKeys(t *testing.T) {
	m := testModel(t)
	m = press(t, m, "/")
	if !m.filtering {
		t.Fatal("/ did not open the filter prompt")
	}
	// While filtering, q must type rather than quit.
	next, cmd := m.Update(tea.KeyPressMsg{Code: 'q', Text: "q"})
	m = next.(Model)
	if cmd != nil {
		if _, isQuit := cmd().(tea.QuitMsg); isQuit {
			t.Error("q quit the program while the filter prompt was open")
		}
	}
	if !m.filtering {
		t.Error("typing closed the filter prompt")
	}
}

func TestFilterEscapeClears(t *testing.T) {
	m := testModel(t)
	m = press(t, m, "/")
	next, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = next.(Model)
	if m.filtering {
		t.Error("esc did not close the filter prompt")
	}
	if m.filterQuery != "" {
		t.Errorf("filterQuery = %q, want empty after esc", m.filterQuery)
	}
}
