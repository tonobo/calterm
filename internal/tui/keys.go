package tui

import "charm.land/bubbles/v2/key"

// KeyMap follows vim movement plus calendar conventions.
type KeyMap struct {
	Up        key.Binding
	Down      key.Binding
	Left      key.Binding
	Right     key.Binding
	Agenda    key.Binding
	Month     key.Binding
	Week      key.Binding
	Day       key.Binding
	Today     key.Binding
	Calendars key.Binding
	Top       key.Binding
	Bottom    key.Binding
	Enter     key.Binding
	Back      key.Binding
	Sync      key.Binding
	Filter    key.Binding
	Help      key.Binding
	Quit      key.Binding
	PageUp    key.Binding
	PageDown  key.Binding
}

func DefaultKeyMap() KeyMap {
	return KeyMap{
		Up:        key.NewBinding(key.WithKeys("k", "up"), key.WithHelp("k/↑", "up")),
		Down:      key.NewBinding(key.WithKeys("j", "down"), key.WithHelp("j/↓", "down")),
		Left:      key.NewBinding(key.WithKeys("h", "left"), key.WithHelp("h/←", "previous")),
		Right:     key.NewBinding(key.WithKeys("l", "right"), key.WithHelp("l/→", "next")),
		Agenda:    key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "agenda")),
		Month:     key.NewBinding(key.WithKeys("m"), key.WithHelp("m", "month")),
		Week:      key.NewBinding(key.WithKeys("w"), key.WithHelp("w", "week")),
		Day:       key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "day")),
		Today:     key.NewBinding(key.WithKeys("t"), key.WithHelp("t", "today")),
		Calendars: key.NewBinding(key.WithKeys("c"), key.WithHelp("c", "calendars")),
		Top:       key.NewBinding(key.WithKeys("g"), key.WithHelp("g", "start")),
		Bottom:    key.NewBinding(key.WithKeys("G"), key.WithHelp("G", "end")),
		Enter:     key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "details")),
		Back:      key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
		Sync:      key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "sync")),
		Filter:    key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "filter")),
		Help:      key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "help")),
		Quit:      key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
		PageUp:    key.NewBinding(key.WithKeys("pgup"), key.WithHelp("pgup", "page up")),
		PageDown:  key.NewBinding(key.WithKeys("pgdown"), key.WithHelp("pgdn", "page down")),
	}
}
