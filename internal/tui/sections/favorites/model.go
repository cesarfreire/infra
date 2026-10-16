// Package favorites is the menu section listing favorite and recently used
// resources of the current profile and region, to jump straight to them.
package favorites

import (
	"fmt"
	"slices"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"infra/internal/domain"
	"infra/internal/filter"
	"infra/internal/tui/components/listview"
	"infra/internal/tui/section"
)

// Title is the menu label of this section.
const Title = "Favorites"

// entry is a row: a bookmark and whether it is a favorite (else a recent).
type entry struct {
	domain.Bookmark
	favorite bool
}

type keyMap struct {
	Open     key.Binding
	Remove   key.Binding
	Favorite key.Binding
}

// Model implements section.Section.
type Model struct {
	deps     section.Deps
	sections map[domain.ResourceKind]string // kind → title of the section owning it
	keys     keyMap
	list     *listview.Model[entry]
	shown    []entry // what the list shows, to rebuild only on changes
}

// New creates the Favorites section. sections maps each resource kind to the
// title of the section that shows it.
func New(deps section.Deps, sections map[domain.ResourceKind]string) *Model {
	m := &Model{
		deps:     deps,
		sections: sections,
		list:     listview.New("Favorites and recents", "favorites or recent resources", columns(sections), fields, "name, kind:instance", deps.Theme),
		keys: keyMap{
			Open:     key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "go to")),
			Remove:   key.NewBinding(key.WithKeys("x", "delete"), key.WithHelp("x", "remove")),
			Favorite: key.NewBinding(key.WithKeys("*"), key.WithHelp("*", "favorite")),
		},
	}
	m.sync()
	return m
}

func columns(sections map[domain.ResourceKind]string) []listview.Column[entry] {
	return []listview.Column[entry]{
		{Title: "Resource", Value: func(e entry) string {
			if e.favorite {
				return "★ " + e.Label
			}
			return "  " + e.Label
		}},
		{Title: "Kind", Width: 14, Value: func(e entry) string { return e.Kind.Label() }},
		{Title: "Section", Width: 16, Value: func(e entry) string { return sections[e.Kind] }},
	}
}

func fields(e entry) []filter.Field {
	return []filter.Field{{Name: "name", Value: e.Label}, {Name: "kind", Value: string(e.Kind)}}
}

// sync rebuilds the rows when the bookmarks changed: favorites first, then
// recents that are not favorites.
func (m *Model) sync() {
	favs := m.deps.Bookmarks.Favorites()
	rows := make([]entry, 0, len(favs))
	for _, b := range favs {
		rows = append(rows, entry{Bookmark: b, favorite: true})
	}
	for _, b := range m.deps.Bookmarks.Recents() {
		if !slices.ContainsFunc(favs, b.Same) {
			rows = append(rows, entry{Bookmark: b})
		}
	}
	if !slices.Equal(rows, m.shown) {
		m.shown = rows
		m.list.SetItems(rows)
	}
}

func (m *Model) Title() string { return Title }
func (m *Model) Init() tea.Cmd { m.sync(); return nil }

func (m *Model) Focus() {
	m.sync()
	m.list.Focus()
}

func (m *Model) Blur()                     { m.list.Blur() }
func (m *Model) SetSize(width, height int) { m.list.SetSize(width, height) }

func (m *Model) InputMode() section.InputMode {
	if m.list.Capturing() {
		return section.ModeCapture
	}
	return section.ModeNormal
}

func (m *Model) Update(msg tea.Msg) (section.Section, tea.Cmd) {
	k, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return m, m.list.Update(msg)
	}
	if m.list.Filtering() {
		return m, m.list.Update(k)
	}
	e, selected := m.list.Selected()
	switch {
	case selected && key.Matches(k, m.keys.Open):
		title, known := m.sections[e.Kind]
		if !known {
			return m, section.Status("No section shows " + e.Kind.Label() + "s")
		}
		b := e.Bookmark
		return m, tea.Sequence(section.Navigate(title), func() tea.Msg { return section.RevealMsg{Bookmark: b} })
	case selected && key.Matches(k, m.keys.Remove):
		var cmd tea.Cmd
		if e.favorite {
			cmd = m.deps.ToggleFavorite(e.Bookmark)
		} else if err := m.deps.Bookmarks.Forget(e.Bookmark); err != nil {
			cmd = section.Error(fmt.Errorf("saving recents: %w", err))
		}
		m.sync()
		return m, cmd
	case selected && key.Matches(k, m.keys.Favorite):
		cmd := m.deps.ToggleFavorite(e.Bookmark)
		m.sync()
		return m, cmd
	}
	return m, m.list.Update(k)
}

func (m *Model) Help() []key.Binding {
	if m.list.Filtering() {
		return m.list.Help()
	}
	return append([]key.Binding{m.keys.Open, m.keys.Favorite, m.keys.Remove}, m.list.Help()...)
}

func (m *Model) View() string {
	m.sync()
	if len(m.shown) == 0 {
		th := m.deps.Theme
		return th.Title.Render("Favorites") + "\n\n" +
			"Press " + th.Selected.Render("*") + " on an instance, ECS cluster or service, table, cache, queue or load balancer\n" +
			"to add it here. Resources you open also show up as recents.\n\n" +
			th.Muted.Render("Favorites and recents are kept per profile and region in ~/.config/infra/state.json.")
	}
	return m.list.View()
}
