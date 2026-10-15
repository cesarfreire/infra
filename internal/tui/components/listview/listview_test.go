package listview

import (
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"infra/internal/filter"
	"infra/internal/tui/theme"
)

type item struct{ name, state string }

var cols = []Column[item]{
	{Title: "Name", Value: func(i item) string { return i.name }},
	{Title: "State", Width: 10, Value: func(i item) string { return i.state }},
	{Title: "Extra", Width: 30, Value: func(item) string { return "x" }},
}

func fields(i item) []filter.Field {
	return []filter.Field{{Name: "name", Value: i.name}, {Name: "state", Value: i.state}}
}

func newList() *Model[item] {
	m := New("Things", "things", cols, fields, "", theme.Default())
	m.SetSize(100, 10)
	m.Focus()
	m.SetItems([]item{{"alpha", "up"}, {"beta", "down"}, {"gamma", "up"}})
	return m
}

func typeText(m *Model[item], s string) {
	for _, r := range s {
		m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
}

func TestColumnsAdaptToWidth(t *testing.T) {
	if got := len(visibleColumns(cols, 200)); got != 3 {
		t.Errorf("wide: %d columns, want 3", got)
	}
	if got := len(visibleColumns(cols, 40)); got != 2 {
		t.Errorf("narrow: %d columns, want 2", got)
	}
}

func TestFilterLifecycle(t *testing.T) {
	m := newList()
	m.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	typeText(m, "state:up")
	if !m.Filtering() || len(m.Visible()) != 2 || !m.Capturing() {
		t.Fatalf("filtering = %v, visible = %v", m.Filtering(), m.Visible())
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	if sel, _ := m.Selected(); sel.name != "gamma" {
		t.Errorf("Selected() = %v, want gamma", sel)
	}
	// New items keep the filter.
	m.SetItems(append(m.Items(), item{"delta", "up"}))
	if len(m.Visible()) != 3 {
		t.Errorf("filter should survive SetItems; visible = %v", m.Visible())
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.Capturing() || len(m.Visible()) != 4 {
		t.Error("esc should clear an applied filter")
	}
}

func TestStates(t *testing.T) {
	m := newList()
	m.StartLoading()
	if _, ok := m.Selected(); ok || !strings.Contains(m.View(), "Loading things") {
		t.Error("loading state")
	}
	m.SetError(errors.New("boom"))
	if !strings.Contains(m.View(), "boom") {
		t.Error("error state")
	}
	m.Reset()
	if !strings.Contains(m.View(), "No things found") {
		t.Error("empty state")
	}
}

func TestRevealAndMarker(t *testing.T) {
	m := newList()
	m.SetMarker(func(i item) string {
		if i.name == "gamma" {
			return "★ "
		}
		return ""
	})
	if !strings.Contains(m.View(), "★ gamma") {
		t.Errorf("marker missing:\n%s", m.View())
	}

	// A filter hiding the item is cleared.
	m.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	typeText(m, "alpha")
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if !m.Reveal(func(i item) bool { return i.name == "gamma" }) {
		t.Fatal("gamma should be found")
	}
	if sel, _ := m.Selected(); sel.name != "gamma" || !m.Query().Empty() {
		t.Errorf("Selected() = %v, query = %q", sel, m.Query())
	}

	// Before the items load, the reveal waits for them.
	m.StartLoading()
	m.Reveal(func(i item) bool { return i.name == "beta" })
	m.SetItems([]item{{"alpha", "up"}, {"beta", "down"}})
	if sel, _ := m.Selected(); sel.name != "beta" {
		t.Errorf("pending reveal not applied: %v", sel)
	}
}
