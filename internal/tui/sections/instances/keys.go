package instances

import "charm.land/bubbles/v2/key"

type keyMap struct {
	Select      key.Binding
	Back        key.Binding
	Refresh     key.Binding
	Connect     key.Binding
	PortForward key.Binding
	Console     key.Binding
	Details     key.Binding
	Start       key.Binding
	Favorite    key.Binding
	PowerOn     key.Binding
	PowerOff    key.Binding
	Reboot      key.Binding
}

func defaultKeyMap() keyMap {
	return keyMap{
		Select:      key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "actions")),
		Back:        key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
		Refresh:     key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "refresh")),
		Connect:     key.NewBinding(key.WithKeys("c"), key.WithHelp("c", "connect")),
		PortForward: key.NewBinding(key.WithKeys("p"), key.WithHelp("p", "port forward")),
		Console:     key.NewBinding(key.WithKeys("o"), key.WithHelp("o", "console")),
		Details:     key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "details")),
		Start:       key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "start")),
		Favorite:    key.NewBinding(key.WithKeys("*"), key.WithHelp("*", "favorite")),
		PowerOn:     key.NewBinding(key.WithKeys("S"), key.WithHelp("S", "start instance")),
		PowerOff:    key.NewBinding(key.WithKeys("X"), key.WithHelp("X", "stop instance")),
		Reboot:      key.NewBinding(key.WithKeys("B"), key.WithHelp("B", "reboot")),
	}
}
