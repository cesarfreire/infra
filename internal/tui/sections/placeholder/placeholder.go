// Package placeholder is a section for menu entries not implemented yet.
package placeholder

import (
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"infra/internal/tui/section"
	"infra/internal/tui/theme"
)

// Model implements section.Section with a static message.
type Model struct {
	title       string
	description string
	theme       theme.Theme
}

// New returns a placeholder section.
func New(title, description string, th theme.Theme) *Model {
	return &Model{title: title, description: description, theme: th}
}

func (m *Model) Title() string                             { return m.title }
func (m *Model) Init() tea.Cmd                             { return nil }
func (m *Model) Update(tea.Msg) (section.Section, tea.Cmd) { return m, nil }
func (m *Model) SetSize(int, int)                          {}
func (m *Model) Focus()                                    {}
func (m *Model) Blur()                                     {}
func (m *Model) Help() []key.Binding                       { return nil }
func (m *Model) InputMode() section.InputMode              { return section.ModeNormal }

func (m *Model) View() string {
	return m.theme.Title.Render(m.title) + "\n\n" +
		m.description + "\n\n" +
		m.theme.Muted.Render("Coming soon.")
}
