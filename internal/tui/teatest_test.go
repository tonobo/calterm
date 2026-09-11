package tui

import (
	"bytes"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/exp/teatest/v2"

	"github.com/tonobo/calterm/internal/config"
	"github.com/tonobo/calterm/internal/store"
)

func TestProgramStartsRendersAndQuits(t *testing.T) {
	cfg := &config.Config{UI: config.UIConfig{DefaultView: "agenda", WeekStart: "monday"}}
	idx := &store.OccurrenceIndex{
		GeneratedAt: time.Date(2026, 6, 10, 7, 0, 0, 0, time.UTC),
		Occurrences: agendaFixture(),
	}
	meta := &store.Meta{Accounts: []store.AccountMeta{{
		Name: "personal", Calendars: []store.CalendarMeta{{ID: "work", Name: "Work"}},
	}}}
	m := New(cfg, nil, idx, meta, time.Date(2026, 6, 10, 8, 0, 0, 0, time.UTC))
	m.loc = time.UTC

	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(80, 24))

	teatest.WaitFor(t, tm.Output(), func(b []byte) bool {
		return bytes.Contains(b, []byte("Standup"))
	}, teatest.WithDuration(3*time.Second))

	tm.Send(tea.KeyPressMsg{Code: 'q', Text: "q"})
	tm.WaitFinished(t, teatest.WithFinalTimeout(3*time.Second))
}
