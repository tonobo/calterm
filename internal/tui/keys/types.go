// Package keys models a leader-key chord tree and the dispatcher that walks
// it. It is deliberately standalone: it knows nothing about calterm's model,
// so it can be tested without one and cannot create an import cycle.
package keys

import (
	"sort"

	tea "charm.land/bubbletea/v2"
)

// Mode identifies an application state that can gate which chords apply.
// This package never interprets a Mode beyond comparing it for equality; the
// application assigns the meanings.
type Mode int

// Entry is a node in the chord tree. It is a sealed interface with exactly two
// implementations: a key slot is EITHER a leaf command OR a group, never both.
// That is what makes a prefix ambiguity -- "va" also being a prefix of "vab" --
// impossible to express, rather than something the dispatcher must resolve.
type Entry interface{ isEntry() }

// Command is a leaf: pressing its key runs it and ends the chord.
type Command struct {
	ID          string
	Description string
	// Modes lists the modes in which this command applies. Empty means every
	// mode.
	Modes []Mode
	// Run returns the command to execute. It may be nil.
	Run func() tea.Cmd
}

func (*Command) isEntry() {}

// Group is an interior node: pressing its key descends and shows the popup.
type Group struct {
	Description string
	Entries     map[string]Entry
	Modes       []Mode
}

func (*Group) isEntry() {}

// Root holds the top-level key slots. In practice it has one: the leader.
type Root struct {
	Entries map[string]Entry
}

// Item is one row the popup can render.
type Item struct {
	Key         string
	Description string
	IsGroup     bool
}

func modeAllowed(modes []Mode, mode Mode) bool {
	if len(modes) == 0 {
		return true
	}
	for _, m := range modes {
		if m == mode {
			return true
		}
	}
	return false
}

// visible reports whether an entry can be seen and matched in mode. A group is
// visible only if it can lead somewhere: a group whose commands are all gated
// out would otherwise be an empty box the user can enter but not leave usefully.
func visible(e Entry, mode Mode) bool {
	switch t := e.(type) {
	case *Command:
		return modeAllowed(t.Modes, mode)
	case *Group:
		return modeAllowed(t.Modes, mode) && groupHasVisibleCommand(t, mode)
	}
	return false
}

func groupHasVisibleCommand(g *Group, mode Mode) bool {
	for _, e := range g.Entries {
		switch t := e.(type) {
		case *Command:
			if modeAllowed(t.Modes, mode) {
				return true
			}
		case *Group:
			if modeAllowed(t.Modes, mode) && groupHasVisibleCommand(t, mode) {
				return true
			}
		}
	}
	return false
}

// VisibleItems returns the rows the popup should show for g in mode: groups
// first, then commands, alphabetically by key within each bucket. Map
// iteration order is random, so the sort is what makes rendering stable.
func VisibleItems(g *Group, mode Mode) []Item {
	var groups, commands []Item
	for key, e := range g.Entries {
		if !visible(e, mode) {
			continue
		}
		switch t := e.(type) {
		case *Group:
			groups = append(groups, Item{Key: key, Description: t.Description, IsGroup: true})
		case *Command:
			commands = append(commands, Item{Key: key, Description: t.Description})
		}
	}
	byKey := func(s []Item) { sort.Slice(s, func(i, j int) bool { return s[i].Key < s[j].Key }) }
	byKey(groups)
	byKey(commands)
	return append(groups, commands...)
}
