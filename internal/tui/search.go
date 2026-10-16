package tui

import (
	"fmt"

	tea "charm.land/bubbletea/v2"

	"infra/internal/domain"
	"infra/internal/tui/components/picker"
	"infra/internal/tui/section"
)

// searchEntry is where a global search result lives.
type searchEntry struct {
	section  string // title of the section to navigate to
	bookmark domain.Bookmark
}

// openSearch shows every resource the sections have loaded. Searchable
// sections never opened are initialized now, so their resources join the
// results as they load (see refreshSearch).
func (m *Model) openSearch() tea.Cmd {
	var cmds []tea.Cmd
	for i, s := range m.sections {
		if _, ok := s.(section.Searchable); ok && !m.initialized[i] {
			m.initialized[i] = true
			cmds = append(cmds, s.Init())
		}
	}
	items := m.searchItems()
	m.dialog = dialogSearch
	m.picker = picker.New(searchTitle(len(items)), items, "", max(3, min(14, m.height-12)))
	return tea.Batch(append(cmds, m.picker.Focus())...)
}

// refreshSearch updates the results while the search is open.
func (m *Model) refreshSearch() {
	if m.dialog != dialogSearch || m.picker == nil {
		return
	}
	items := m.searchItems()
	m.picker.SetItems(items)
	m.picker.SetTitle(searchTitle(len(items)))
}

// searchItems collects the resources of every searchable section.
func (m *Model) searchItems() []picker.Item {
	m.found = map[string]searchEntry{}
	var items []picker.Item
	for _, s := range m.sections {
		searchable, ok := s.(section.Searchable)
		if !ok {
			continue
		}
		for _, it := range searchable.SearchItems() {
			b := it.Bookmark
			value := fmt.Sprintf("%s|%s|%s", b.Kind, b.Parent, b.ID)
			if _, dup := m.found[value]; dup {
				continue
			}
			m.found[value] = searchEntry{section: s.Title(), bookmark: b}
			detail := b.Kind.Label()
			if it.Detail != "" {
				detail += " · " + it.Detail
			}
			items = append(items, picker.Item{Value: value, Label: b.Label, Detail: detail})
		}
	}
	return items
}

func searchTitle(n int) string {
	return fmt.Sprintf("Search %d resources", n)
}

// chooseSearch goes to the resource, like opening a favorite.
func (m *Model) chooseSearch(value string) tea.Cmd {
	entry, ok := m.found[value]
	if !ok {
		return nil
	}
	b := entry.bookmark
	return tea.Sequence(section.Navigate(entry.section), func() tea.Msg { return section.RevealMsg{Bookmark: b} })
}
