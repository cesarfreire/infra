// Package theme centralizes colors and styles so every section looks the same.
package theme

import (
	"charm.land/bubbles/v2/table"
	"charm.land/lipgloss/v2"
)

var (
	Accent  = lipgloss.Color("#7D56F4")
	Subtle  = lipgloss.Color("#626262")
	Text    = lipgloss.Color("#E4E4E4")
	Success = lipgloss.Color("#04B575")
	Warning = lipgloss.Color("#E5C07B")
	Danger  = lipgloss.Color("#FF5F87")
)

// Theme groups the styles shared by the root model and sections.
type Theme struct {
	Header        lipgloss.Style
	HeaderContext lipgloss.Style
	HeaderChip    lipgloss.Style
	ReadOnlyChip  lipgloss.Style
	Dialog        lipgloss.Style
	DialogAlert   lipgloss.Style
	Panel         lipgloss.Style
	PanelFocused  lipgloss.Style
	MenuTitle     lipgloss.Style
	MenuItem      lipgloss.Style
	MenuActive    lipgloss.Style
	Title         lipgloss.Style
	Muted         lipgloss.Style
	Label         lipgloss.Style
	Selected      lipgloss.Style
	StatusInfo    lipgloss.Style
	StatusError   lipgloss.Style
	Warning       lipgloss.Style
	Table         table.Styles
}

// Default returns the default dark theme.
func Default() Theme {
	panel := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(Subtle).
		Padding(0, 1)

	tbl := table.DefaultStyles()
	tbl.Header = tbl.Header.
		Foreground(Accent).
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(Subtle).
		BorderBottom(true)
	tbl.Selected = tbl.Selected.Foreground(Text).Background(Accent)

	return Theme{
		Header:        lipgloss.NewStyle().Bold(true).Foreground(Text).Background(Accent).Padding(0, 1),
		HeaderContext: lipgloss.NewStyle().Foreground(Subtle).Padding(0, 1),
		HeaderChip:    lipgloss.NewStyle().Foreground(Text).Background(lipgloss.Color("#3A3A3A")).Padding(0, 1),
		ReadOnlyChip:  lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#1C1C1C")).Background(Warning).Padding(0, 1),
		Dialog:        lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(Accent).Padding(0, 1),
		DialogAlert:   lipgloss.NewStyle().Border(lipgloss.ThickBorder()).BorderForeground(Danger).Padding(0, 1),
		Panel:         panel,
		PanelFocused:  panel.BorderForeground(Accent),
		MenuTitle:     lipgloss.NewStyle().Bold(true).Foreground(Subtle),
		MenuItem:      lipgloss.NewStyle().Foreground(Text).PaddingLeft(1),
		MenuActive:    lipgloss.NewStyle().Bold(true).Foreground(Text).Background(Accent).PaddingLeft(1),
		Title:         lipgloss.NewStyle().Bold(true).Foreground(Accent),
		Muted:         lipgloss.NewStyle().Foreground(Subtle),
		Label:         lipgloss.NewStyle().Bold(true).Foreground(Subtle),
		Selected:      lipgloss.NewStyle().Bold(true).Foreground(Accent),
		StatusInfo:    lipgloss.NewStyle().Foreground(Success),
		StatusError:   lipgloss.NewStyle().Foreground(Danger),
		Warning:       lipgloss.NewStyle().Foreground(Warning),
		Table:         tbl,
	}
}
