// Package actionmenu is a vertical menu of labeled choices with optional
// hotkeys, navigable with the keyboard and clickable with the mouse.
package actionmenu

import (
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"infra/internal/tui/theme"
)

// Top is the number of rows View renders above the first item
// (title + blank line); use it to map mouse clicks.
const Top = 2

// Item is a menu entry. Key is optional.
type Item struct {
	Label string
	Key   key.Binding
}

// Model is the menu state.
type Model struct {
	items  []Item
	cursor int
	up     key.Binding
	down   key.Binding
	enter  key.Binding
}

// New returns an empty menu.
func New() Model {
	return Model{
		up:    key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "up")),
		down:  key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "down")),
		enter: key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "select")),
	}
}

// SetItems replaces the entries and moves the cursor to the first one.
func (m *Model) SetItems(items []Item) {
	m.items = items
	m.cursor = 0
}

// Update moves the cursor or returns the index chosen with enter or a hotkey.
func (m *Model) Update(msg tea.KeyPressMsg) (int, bool) {
	if len(m.items) == 0 {
		return 0, false
	}
	switch {
	case key.Matches(msg, m.up):
		m.cursor = (m.cursor - 1 + len(m.items)) % len(m.items)
	case key.Matches(msg, m.down):
		m.cursor = (m.cursor + 1) % len(m.items)
	case key.Matches(msg, m.enter):
		return m.cursor, true
	default:
		for i, it := range m.items {
			if len(it.Key.Keys()) > 0 && key.Matches(msg, it.Key) {
				m.cursor = i
				return i, true
			}
		}
	}
	return 0, false
}

// Click returns the index of the item at content row y.
func (m *Model) Click(y int) (int, bool) {
	idx := y - Top
	if idx < 0 || idx >= len(m.items) {
		return 0, false
	}
	m.cursor = idx
	return idx, true
}

// Help returns the navigation bindings.
func (m *Model) Help() []key.Binding { return []key.Binding{m.up, m.down, m.enter} }

// View renders a heading followed by the items.
func (m Model) View(th theme.Theme, title, subtitle string) string {
	var b strings.Builder
	b.WriteString(th.Title.Render(title) + " " + th.Muted.Render(subtitle) + "\n\n")
	for i, it := range m.items {
		hint := ""
		if h := it.Key.Help().Key; h != "" {
			hint = "  " + th.Muted.Render("("+h+")")
		}
		if i == m.cursor {
			b.WriteString(th.Selected.Render("› "+it.Label) + hint + "\n")
		} else {
			b.WriteString("  " + it.Label + hint + "\n")
		}
	}
	return b.String()
}
