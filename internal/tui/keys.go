package tui

import "charm.land/bubbles/v2/key"

type keyMap struct {
	Up         key.Binding
	Down       key.Binding
	Open       key.Binding
	Back       key.Binding
	Quit       key.Binding
	ForceQuit  key.Binding
	Profile    key.Binding
	Region     key.Binding
	CloseAlert key.Binding
	Login      key.Binding
	Yes        key.Binding
	No         key.Binding
}

func defaultKeyMap() keyMap {
	return keyMap{
		Up:         key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "up")),
		Down:       key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "down")),
		Open:       key.NewBinding(key.WithKeys("enter", "right", "l", "tab"), key.WithHelp("enter/→", "open")),
		Back:       key.NewBinding(key.WithKeys("esc", "shift+tab"), key.WithHelp("esc", "menu")),
		Quit:       key.NewBinding(key.WithKeys("q"), key.WithHelp("q", "quit")),
		ForceQuit:  key.NewBinding(key.WithKeys("ctrl+c")),
		Profile:    key.NewBinding(key.WithKeys("ctrl+p"), key.WithHelp("ctrl+p", "profile")),
		Region:     key.NewBinding(key.WithKeys("ctrl+r"), key.WithHelp("ctrl+r", "region")),
		CloseAlert: key.NewBinding(key.WithKeys("enter", "esc", "q"), key.WithHelp("enter/esc", "close")),
		Login:      key.NewBinding(key.WithKeys("l"), key.WithHelp("l", "aws sso login")),
		Yes:        key.NewBinding(key.WithKeys("y"), key.WithHelp("y", "yes")),
		No:         key.NewBinding(key.WithKeys("n", "esc"), key.WithHelp("n/esc", "no")),
	}
}

// bindings adapts a slice of bindings to help.KeyMap.
type bindings []key.Binding

func (b bindings) ShortHelp() []key.Binding  { return b }
func (b bindings) FullHelp() [][]key.Binding { return [][]key.Binding{b} }
