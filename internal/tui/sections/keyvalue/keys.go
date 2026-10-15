package keyvalue

import "charm.land/bubbles/v2/key"

type keyMap struct {
	Browse   key.Binding
	Inspect  key.Binding
	Find     key.Binding
	More     key.Binding
	Details  key.Binding
	Console  key.Binding
	Refresh  key.Binding
	Back     key.Binding
	Run      key.Binding
	Next     key.Binding
	Scroll   key.Binding
	Favorite key.Binding
	Edit     key.Binding
	Delete   key.Binding
	Save     key.Binding
}

func defaultKeyMap() keyMap {
	return keyMap{
		Browse:   key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "items")),
		Inspect:  key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "view item")),
		Find:     key.NewBinding(key.WithKeys("f"), key.WithHelp("f", "find by key")),
		More:     key.NewBinding(key.WithKeys("n"), key.WithHelp("n", "more items")),
		Details:  key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "details")),
		Console:  key.NewBinding(key.WithKeys("o"), key.WithHelp("o", "console")),
		Refresh:  key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "refresh")),
		Back:     key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
		Run:      key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "search")),
		Next:     key.NewBinding(key.WithKeys("tab", "shift+tab"), key.WithHelp("tab", "next field")),
		Scroll:   key.NewBinding(key.WithKeys("up", "down", "pgup", "pgdown"), key.WithHelp("↑↓/pgup/pgdn", "scroll")),
		Favorite: key.NewBinding(key.WithKeys("*"), key.WithHelp("*", "favorite")),
		Edit:     key.NewBinding(key.WithKeys("e"), key.WithHelp("e", "edit item")),
		Delete:   key.NewBinding(key.WithKeys("x"), key.WithHelp("x", "delete item")),
		Save:     key.NewBinding(key.WithKeys("ctrl+s"), key.WithHelp("ctrl+s", "save")),
	}
}
