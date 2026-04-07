// Copyright 2026 ICAP Mock

package tui

import (
	"github.com/charmbracelet/bubbles/key"
)

// keyMap defines the global key bindings.
type keyMap struct {
	Quit      key.Binding
	Save      key.Binding
	Back      key.Binding
	Refresh   key.Binding
	Search    key.Binding
	NextMatch key.Binding
	PrevMatch key.Binding
	Help      key.Binding
	Screen1   key.Binding
	Screen2   key.Binding
	Screen3   key.Binding
	Screen4   key.Binding
	Screen5   key.Binding
	Screen6   key.Binding
}

// defaultKeyMap returns the default key bindings.
func defaultKeyMap() keyMap {
	return keyMap{
		Quit: key.NewBinding(
			key.WithKeys("ctrl+c"),
			key.WithHelp("ctrl+c", "quit"),
		),
		Save: key.NewBinding(
			key.WithKeys("ctrl+s"),
			key.WithHelp("ctrl+s", "save"),
		),
		Back: key.NewBinding(
			key.WithKeys("esc"),
			key.WithHelp("esc", "back"),
		),
		Refresh: key.NewBinding(
			key.WithKeys("r"),
			key.WithHelp("r", "refresh"),
		),
		Search: key.NewBinding(
			key.WithKeys("/"),
			key.WithHelp("/", "search"),
		),
		NextMatch: key.NewBinding(
			key.WithKeys("n"),
			key.WithHelp("n", "next"),
		),
		PrevMatch: key.NewBinding(
			key.WithKeys("N"),
			key.WithHelp("N", "previous"),
		),
		Help: key.NewBinding(
			key.WithKeys("?"),
			key.WithHelp("?", "toggle help"),
		),
		Screen1: key.NewBinding(
			key.WithKeys("1"),
			key.WithHelp("1", "dashboard"),
		),
		Screen2: key.NewBinding(
			key.WithKeys("2"),
			key.WithHelp("2", "config"),
		),
		Screen3: key.NewBinding(
			key.WithKeys("3"),
			key.WithHelp("3", "scenarios"),
		),
		Screen4: key.NewBinding(
			key.WithKeys("4"),
			key.WithHelp("4", "logs"),
		),
		Screen5: key.NewBinding(
			key.WithKeys("5"),
			key.WithHelp("5", "replay"),
		),
		Screen6: key.NewBinding(
			key.WithKeys("6"),
			key.WithHelp("6", "health"),
		),
	}
}

// ShortHelp returns context-sensitive help for the current screen.
func (m *Model) ShortHelp() []key.Binding {
	keys := defaultKeyMap()
	common := []key.Binding{keys.Quit, keys.Help}

	switch m.currentScreen {
	case ScreenDashboard:
		return append([]key.Binding{
			key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "start")),
			key.NewBinding(key.WithKeys("t"), key.WithHelp("t", "stop")),
			key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "restart")),
		}, common...)
	case ScreenConfig:
		return append([]key.Binding{
			keys.Save,
			keys.Back,
		}, common...)
	case ScreenScenarios:
		return append([]key.Binding{
			keys.Back,
		}, common...)
	case ScreenLogs:
		return append([]key.Binding{
			keys.Search,
			key.NewBinding(key.WithKeys("f"), key.WithHelp("f", "filter")),
			key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "auto-scroll")),
		}, common...)
	case ScreenReplay:
		return append([]key.Binding{
			key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "start replay")),
			key.NewBinding(key.WithKeys("t"), key.WithHelp("t", "stop replay")),
		}, common...)
	case ScreenHealth:
		return append([]key.Binding{
			keys.Back,
		}, common...)
	default:
		return common
	}
}

// FullHelp returns full help text.
func (m *Model) FullHelp() [][]key.Binding {
	keys := defaultKeyMap()
	return [][]key.Binding{
		{
			keys.Quit,
			keys.Back,
			keys.Refresh,
			keys.Help,
		},
		{
			keys.Save,
			keys.Search,
			key.NewBinding(key.WithKeys("f"), key.WithHelp("f", "filter (logs)")),
			key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "auto-scroll (logs)")),
		},
		{
			keys.Screen1,
			keys.Screen2,
			keys.Screen3,
			keys.Screen4,
			keys.Screen5,
			keys.Screen6,
		},
		{
			key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "start (dashboard/replay)")),
			key.NewBinding(key.WithKeys("t"), key.WithHelp("t", "stop (dashboard/replay)")),
			key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "restart (dashboard)")),
			key.NewBinding(key.WithKeys("tab"), key.WithHelp("Tab", "next screen")),
			key.NewBinding(key.WithKeys("shift+tab"), key.WithHelp("Shift+Tab", "prev screen")),
		},
	}
}
