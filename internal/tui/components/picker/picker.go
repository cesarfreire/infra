// Package picker is a filterable single-choice list used in dialogs
// (profile and region selectors). Typing filters; arrows move; enter picks.
package picker

import (
	"strconv"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"infra/internal/filter"
	"infra/internal/tui/theme"
)

// Top is the number of rows View renders above the first item
// (title, filter input, blank line); use it to map mouse clicks.
const Top = 3

// Item is a choice. Detail is shown dimmed and is also matched by the filter.
type Item struct {
	Value  string
	Label  string
	Detail string
}

// Result is the outcome of a key press.
type Result int

const (
	Pending Result = iota
	Chosen
	Cancelled
)

// Model is the picker state.
type Model struct {
	title   string
	items   []Item
	visible []Item
	input   textinput.Model
	cursor  int
	offset  int // first visible row when the list scrolls
	rows    int
	current string
	keys    struct{ up, down, choose, cancel key.Binding }
}

// New returns a picker with the cursor on the item whose value is current.
func New(title string, items []Item, current string, rows int) *Model {
	in := textinput.New()
	in.Prompt = "/ "
	in.Placeholder = "type to filter"
	in.Focus() // typing filters right away; Focus() only starts the blink
	m := &Model{title: title, items: items, input: in, rows: max(1, rows), current: current}
	m.keys.up = key.NewBinding(key.WithKeys("up", "ctrl+k"), key.WithHelp("↑", "up"))
	m.keys.down = key.NewBinding(key.WithKeys("down", "ctrl+j"), key.WithHelp("↓", "down"))
	m.keys.choose = key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "choose"))
	m.keys.cancel = key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "cancel"))
	m.applyFilter()
	for i, it := range m.visible {
		if it.Value == current {
			m.setCursor(i)
		}
	}
	return m
}

// SetItems replaces the choices, keeping what was typed and, when it is
// still there, the item under the cursor.
func (m *Model) SetItems(items []Item) {
	selected, hadSelection := m.Selected()
	m.items = items
	m.applyFilter()
	if hadSelection {
		for i, it := range m.visible {
			if it.Value == selected.Value {
				m.setCursor(i)
			}
		}
	}
}

// SetTitle changes the heading.
func (m *Model) SetTitle(title string) { m.title = title }

// Focus returns the command that blinks the filter input cursor.
func (m *Model) Focus() tea.Cmd { return m.input.Focus() }

// Selected returns the item under the cursor.
func (m *Model) Selected() (Item, bool) {
	if m.cursor < 0 || m.cursor >= len(m.visible) {
		return Item{}, false
	}
	return m.visible[m.cursor], true
}

// Update handles a message; for key presses it reports whether the user
// chose an item or cancelled.
func (m *Model) Update(msg tea.Msg) (Result, tea.Cmd) {
	k, ok := msg.(tea.KeyPressMsg)
	if !ok {
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return Pending, cmd
	}
	switch {
	case key.Matches(k, m.keys.cancel):
		return Cancelled, nil
	case key.Matches(k, m.keys.choose):
		if _, ok := m.Selected(); ok {
			return Chosen, nil
		}
		return Pending, nil
	case key.Matches(k, m.keys.up):
		m.setCursor(m.cursor - 1)
		return Pending, nil
	case key.Matches(k, m.keys.down):
		m.setCursor(m.cursor + 1)
		return Pending, nil
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(k)
	m.applyFilter()
	return Pending, cmd
}

// Click selects the item at content row y and reports whether there was one.
func (m *Model) Click(y int) bool {
	idx := m.offset + y - Top
	if y < Top || idx >= len(m.visible) || y-Top >= m.rows {
		return false
	}
	m.setCursor(idx)
	return true
}

// Help returns the dialog's bindings.
func (m *Model) Help() []key.Binding {
	return []key.Binding{m.keys.up, m.keys.down, m.keys.choose, m.keys.cancel}
}

func (m *Model) applyFilter() {
	q := filter.Fold(strings.TrimSpace(m.input.Value()))
	m.visible = m.visible[:0]
	for _, it := range m.items {
		if q == "" || strings.Contains(filter.Fold(it.Label+" "+it.Detail), q) {
			m.visible = append(m.visible, it)
		}
	}
	m.setCursor(0)
}

func (m *Model) setCursor(i int) {
	m.cursor = min(max(0, i), len(m.visible)-1)
	if m.cursor < m.offset {
		m.offset = max(0, m.cursor)
	}
	if m.cursor >= m.offset+m.rows {
		m.offset = m.cursor - m.rows + 1
	}
}

// View renders the title, filter input and the visible window of items.
func (m *Model) View(th theme.Theme, width int) string {
	m.input.SetWidth(max(10, width-4))
	lines := []string{th.Title.Render(m.title), m.input.View(), ""}
	if len(m.visible) == 0 {
		lines = append(lines, th.Muted.Render("No matches."))
	}
	end := min(len(m.visible), m.offset+m.rows)
	for i := m.offset; i < end; i++ {
		it := m.visible[i]
		cursor, current := " ", " "
		if i == m.cursor {
			cursor = th.Selected.Render("›")
		}
		if it.Value == m.current {
			current = "•"
		}
		line := cursor + current + " " + it.Label
		if it.Detail != "" {
			line += "  " + th.Muted.Render(it.Detail)
		}
		lines = append(lines, line)
	}
	if more := len(m.visible) - end; more > 0 {
		lines = append(lines, th.Muted.Render("  … "+strconv.Itoa(more)+" more"))
	}
	return strings.Join(lines, "\n")
}
