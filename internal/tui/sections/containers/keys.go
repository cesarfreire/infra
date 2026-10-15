package containers

import "charm.land/bubbles/v2/key"

type keyMap struct {
	OpenServices key.Binding
	OpenTasks    key.Binding
	Actions      key.Binding
	AllTasks     key.Binding
	Back         key.Binding
	Refresh      key.Binding
	Connect      key.Binding
	PortForward  key.Binding
	Console      key.Binding
	Details      key.Binding
	Start        key.Binding
	Favorite     key.Binding
	Deploy       key.Binding
	Scale        key.Binding
}

func defaultKeyMap() keyMap {
	return keyMap{
		OpenServices: key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "services")),
		OpenTasks:    key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "tasks")),
		Actions:      key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "actions")),
		AllTasks:     key.NewBinding(key.WithKeys("t"), key.WithHelp("t", "all tasks")),
		Back:         key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
		Refresh:      key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "refresh")),
		Connect:      key.NewBinding(key.WithKeys("c"), key.WithHelp("c", "connect")),
		PortForward:  key.NewBinding(key.WithKeys("p"), key.WithHelp("p", "port forward")),
		Console:      key.NewBinding(key.WithKeys("o"), key.WithHelp("o", "console")),
		Details:      key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "details")),
		Start:        key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "confirm")),
		Favorite:     key.NewBinding(key.WithKeys("*"), key.WithHelp("*", "favorite")),
		Deploy:       key.NewBinding(key.WithKeys("D"), key.WithHelp("D", "force deploy")),
		Scale:        key.NewBinding(key.WithKeys("N"), key.WithHelp("N", "desired count")),
	}
}
