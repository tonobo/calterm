package keys

import tea "charm.land/bubbletea/v2"

// Dispatcher walks the chord tree. It is the only mutable state in this
// package; the popup is rendered from it rather than holding its own copy.
//
// There is no timeout. A pending chord is always visible in the popup, so it
// is not a hidden mode the user can get stuck in.
type Dispatcher struct {
	root             *Root
	pending          *Group
	path             []string
	unknown          string
	handledElsewhere func(key string) bool
}

func NewDispatcher(root *Root) *Dispatcher { return &Dispatcher{root: root} }

// SetHandledElsewhere registers a predicate for keys the application will act
// on itself even when they match nothing in the chord tree. Such a key still
// falls through (Handle still returns ok=false for it), but it must not be
// reported as unbound: the user pressed j, the cursor moved, and "no binding
// for j" would simply be false. A nil predicate (the default) reproduces the
// old behaviour exactly -- every unmatched key mid-chord is reported.
func (d *Dispatcher) SetHandledElsewhere(fn func(key string) bool) {
	d.handledElsewhere = fn
}

// Pending returns the group whose popup should be shown, and the breadcrumb
// path to it. A nil group means no popup.
func (d *Dispatcher) Pending() (*Group, []string) { return d.pending, d.path }

// Unknown returns the last key pressed mid-chord that matched nothing, so the
// popup can say so. It is cleared by the next key that does match.
func (d *Dispatcher) Unknown() string { return d.unknown }

func (d *Dispatcher) Reset() {
	d.pending = nil
	d.path = nil
	d.unknown = ""
}

// Handle advances the chord state.
//
// The bool reports whether the key was consumed. A false return means the
// caller should go on to handle the key itself -- which is how an unbound key
// still scrolls the list while the popup happens to be open.
func (d *Dispatcher) Handle(key string, mode Mode) (tea.Cmd, bool) {
	if d.pending == nil {
		e, ok := d.root.Entries[key]
		if !ok || !visible(e, mode) {
			return nil, false
		}
		return d.enter(key, e)
	}

	switch key {
	case "esc":
		d.Reset()
		return nil, true
	case "backspace":
		d.unknown = ""
		d.back()
		return nil, true
	}

	e, ok := d.pending.Entries[key]
	if !ok || !visible(e, mode) {
		// Keep the popup open and let the key through. Record it so the popup
		// can show that it matched nothing -- otherwise a typo looks identical
		// to a key that did something invisible. Except when the caller has
		// claimed this key via HandledElsewhere: it IS bound, just not in the
		// chord tree, and reporting it as unknown would be false.
		if d.handledElsewhere == nil || !d.handledElsewhere(key) {
			d.unknown = key
		}
		return nil, false
	}
	d.unknown = ""
	return d.enter(key, e)
}

func (d *Dispatcher) enter(key string, e Entry) (tea.Cmd, bool) {
	switch t := e.(type) {
	case *Command:
		d.Reset()
		if t.Run == nil {
			return nil, true
		}
		return t.Run(), true
	case *Group:
		d.pending = t
		d.path = append(d.path, key)
		return nil, true
	}
	return nil, false
}

// back pops one level, re-deriving the parent by walking from the root so that
// the pending group and the path can never disagree.
func (d *Dispatcher) back() {
	if len(d.path) <= 1 {
		d.Reset()
		return
	}
	d.path = d.path[:len(d.path)-1]
	g := d.groupAtPath(d.path)
	if g == nil {
		d.Reset()
		return
	}
	d.pending = g
}

func (d *Dispatcher) groupAtPath(path []string) *Group {
	var cur Entry = &Group{Entries: d.root.Entries}
	for _, key := range path {
		g, ok := cur.(*Group)
		if !ok {
			return nil
		}
		next, ok := g.Entries[key]
		if !ok {
			return nil
		}
		cur = next
	}
	g, _ := cur.(*Group)
	return g
}
