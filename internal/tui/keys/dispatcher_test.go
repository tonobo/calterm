package keys

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

const (
	modeA Mode = iota
	modeB
)

// fixture builds a small tree whose commands append their ID to log.
func fixture(log *[]string) *Root {
	cmd := func(id string, modes ...Mode) *Command {
		return &Command{
			ID:          id,
			Description: "does " + id,
			Modes:       modes,
			Run: func() tea.Cmd {
				*log = append(*log, id)
				return nil
			},
		}
	}
	return &Root{Entries: map[string]Entry{
		" ": &Group{
			Description: "leader",
			Entries: map[string]Entry{
				"v": &Group{
					Description: "+view",
					Entries: map[string]Entry{
						"a": cmd("view.agenda"),
						"m": cmd("view.month"),
					},
				},
				"s": cmd("sync"),
				"x": cmd("only.a", modeA),
				"g": &Group{
					Description: "+gated",
					Entries:     map[string]Entry{"z": cmd("gated.z", modeA)},
				},
			},
		},
	}}
}

func press(t *testing.T, d *Dispatcher, mode Mode, ks ...string) []bool {
	t.Helper()
	var handled []bool
	for _, k := range ks {
		cmd, ok := d.Handle(k, mode)
		handled = append(handled, ok)
		if cmd != nil {
			cmd() // run it so the fixture's log records the call
		}
	}
	return handled
}

func TestChordRunsCommand(t *testing.T) {
	var log []string
	d := NewDispatcher(fixture(&log))
	press(t, d, modeA, " ", "v", "m")
	if len(log) != 1 || log[0] != "view.month" {
		t.Fatalf("log = %v, want [view.month]", log)
	}
	if g, _ := d.Pending(); g != nil {
		t.Error("dispatcher should reset after running a command")
	}
}

func TestLeaderAloneOpensGroupWithoutRunningAnything(t *testing.T) {
	var log []string
	d := NewDispatcher(fixture(&log))
	press(t, d, modeA, " ")
	g, path := d.Pending()
	if g == nil {
		t.Fatal("leader did not open a group")
	}
	if len(path) != 1 || path[0] != " " {
		t.Errorf("path = %v, want [\" \"]", path)
	}
	if len(log) != 0 {
		t.Errorf("nothing should have run, got %v", log)
	}
}

func TestUnknownKeyWhileIdleFallsThrough(t *testing.T) {
	var log []string
	d := NewDispatcher(fixture(&log))
	if _, ok := d.Handle("j", modeA); ok {
		t.Error("an unbound key while idle must not be handled")
	}
}

// An unknown key mid-chord keeps the popup open and falls through, so the
// underlying view still scrolls.
func TestUnknownKeyMidChordFallsThroughButKeepsPopupOpen(t *testing.T) {
	var log []string
	d := NewDispatcher(fixture(&log))
	press(t, d, modeA, " ", "v")
	if _, ok := d.Handle("j", modeA); ok {
		t.Error("unknown key mid-chord must fall through")
	}
	if g, _ := d.Pending(); g == nil {
		t.Error("popup should stay open after an unknown key")
	}
	if d.Unknown() != "j" {
		t.Errorf("Unknown() = %q, want j", d.Unknown())
	}
}

func TestUnknownIsClearedByTheNextKey(t *testing.T) {
	var log []string
	d := NewDispatcher(fixture(&log))
	press(t, d, modeA, " ", "v")
	d.Handle("j", modeA)
	press(t, d, modeA, "a")
	if d.Unknown() != "" {
		t.Errorf("Unknown() = %q, want cleared", d.Unknown())
	}
}

func TestEscResets(t *testing.T) {
	var log []string
	d := NewDispatcher(fixture(&log))
	press(t, d, modeA, " ", "v")
	if _, ok := d.Handle("esc", modeA); !ok {
		t.Error("esc mid-chord must be handled")
	}
	if g, _ := d.Pending(); g != nil {
		t.Error("esc should close the popup")
	}
}

func TestBackspaceStepsBackOneLevel(t *testing.T) {
	var log []string
	d := NewDispatcher(fixture(&log))
	press(t, d, modeA, " ", "v")
	press(t, d, modeA, "backspace")
	g, path := d.Pending()
	if g == nil {
		t.Fatal("backspace from a nested group should land on its parent")
	}
	if len(path) != 1 || path[0] != " " {
		t.Errorf("path = %v, want [\" \"]", path)
	}
	// and from the leader level, backspace closes entirely
	press(t, d, modeA, "backspace")
	if g, _ := d.Pending(); g != nil {
		t.Error("backspace at the leader level should close the popup")
	}
}

func TestModeGatingHidesAndBlocksACommand(t *testing.T) {
	var log []string
	d := NewDispatcher(fixture(&log))
	press(t, d, modeB, " ")
	if _, ok := d.Handle("x", modeB); ok {
		t.Error("a command excluded by mode must not be matchable")
	}
	if len(log) != 0 {
		t.Errorf("nothing should have run, got %v", log)
	}
	// but it works in its own mode
	d.Reset()
	press(t, d, modeA, " ", "x")
	if len(log) != 1 || log[0] != "only.a" {
		t.Fatalf("log = %v, want [only.a]", log)
	}
}

func TestGroupWithNoVisibleCommandIsNotEnterable(t *testing.T) {
	var log []string
	d := NewDispatcher(fixture(&log))
	press(t, d, modeB, " ")
	if _, ok := d.Handle("g", modeB); ok {
		t.Error("a group whose only command is mode-gated out must not be enterable")
	}
}

func TestVisibleItemsOrdersGroupsBeforeCommands(t *testing.T) {
	var log []string
	root := fixture(&log)
	leader := root.Entries[" "].(*Group)
	items := VisibleItems(leader, modeA)

	var keysSeen []string
	for _, it := range items {
		keysSeen = append(keysSeen, it.Key)
	}
	// groups (g, v) alphabetically, then commands (s, x) alphabetically
	want := []string{"g", "v", "s", "x"}
	if len(keysSeen) != len(want) {
		t.Fatalf("items = %v, want %v", keysSeen, want)
	}
	for i := range want {
		if keysSeen[i] != want[i] {
			t.Fatalf("items = %v, want %v", keysSeen, want)
		}
	}
	if !items[0].IsGroup || items[2].IsGroup {
		t.Errorf("IsGroup flags wrong: %+v", items)
	}
}

func TestVisibleItemsDropsModeInvalidEntries(t *testing.T) {
	var log []string
	root := fixture(&log)
	leader := root.Entries[" "].(*Group)
	for _, it := range VisibleItems(leader, modeB) {
		if it.Key == "x" || it.Key == "g" {
			t.Errorf("mode-invalid entry %q should not be listed", it.Key)
		}
	}
}

func TestNilRunDoesNotPanic(t *testing.T) {
	root := &Root{Entries: map[string]Entry{
		" ": &Group{Entries: map[string]Entry{"n": &Command{ID: "noop"}}},
	}}
	d := NewDispatcher(root)
	d.Handle(" ", modeA)
	if _, ok := d.Handle("n", modeA); !ok {
		t.Error("a command with no Run should still be handled")
	}
}

// HandledElsewhere lets the caller (the tui package's view) tell the
// dispatcher which unmatched keys it will act on itself, so an unmatched key
// mid-chord that actually does something (movement) is not reported as
// unbound the way a genuinely unbound key still is.
func TestHandledElsewhereSuppressesUnknownForClaimedKeys(t *testing.T) {
	var log []string
	d := NewDispatcher(fixture(&log))
	d.SetHandledElsewhere(func(key string) bool { return key == "j" })
	press(t, d, modeA, " ", "v")

	if _, ok := d.Handle("j", modeA); ok {
		t.Error("a movement key claimed by HandledElsewhere must still fall through (ok=false)")
	}
	if d.Unknown() != "" {
		t.Errorf("Unknown() = %q, want empty: j is claimed by HandledElsewhere", d.Unknown())
	}
	g, _ := d.Pending()
	if g == nil {
		t.Error("the popup should stay open after a claimed key")
	}

	// An unclaimed key still reports as unbound.
	if _, ok := d.Handle("z", modeA); ok {
		t.Error("an unclaimed unbound key must still fall through (ok=false)")
	}
	if d.Unknown() != "z" {
		t.Errorf("Unknown() = %q, want z", d.Unknown())
	}
}

// A dispatcher built without calling SetHandledElsewhere must behave exactly
// as before: every unmatched key mid-chord is reported via Unknown().
func TestNoHandledElsewherePredicateBehavesAsBefore(t *testing.T) {
	var log []string
	d := NewDispatcher(fixture(&log))
	press(t, d, modeA, " ", "v")
	if _, ok := d.Handle("j", modeA); ok {
		t.Error("unknown key mid-chord must fall through")
	}
	if d.Unknown() != "j" {
		t.Errorf("Unknown() = %q, want j (no predicate set)", d.Unknown())
	}
}
