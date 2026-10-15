// Package listview is a filterable, loadable table of resources shared by
// every section: heading with counts, filter line, spinner, error and empty
// states, and a table whose columns adapt to the width.
package listview

import (
	"fmt"
	"slices"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/table"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"infra/internal/filter"
	"infra/internal/tui/theme"
)

// Column describes a table column. The first column has no fixed width: it
// stretches to fill the panel. The others are dropped from the right when
// they don't fit.
type Column[T any] struct {
	Title string
	Width int
	Value func(T) string
}

// Rows of the view above the table (title line + filter line).
const headerRows = 2

const (
	minFlexWidth = 16
	cellPadding  = 2
)

// KeyMap holds the bindings the list handles itself.
type KeyMap struct {
	Filter      key.Binding
	ClearFilter key.Binding
	ApplyFilter key.Binding
	Up          key.Binding
	Down        key.Binding
}

func defaultKeyMap() KeyMap {
	return KeyMap{
		Filter:      key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "filter")),
		ClearFilter: key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "clear filter")),
		ApplyFilter: key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "apply")),
		Up:          key.NewBinding(key.WithKeys("up", "k")),
		Down:        key.NewBinding(key.WithKeys("down", "j")),
	}
}

// Model is a list of T. Create it with New.
type Model[T any] struct {
	title   string
	noun    string // plural, for empty states ("instances")
	columns []Column[T]
	fields  func(T) []filter.Field
	theme   theme.Theme
	keys    KeyMap

	table   table.Model
	spinner spinner.Model
	input   textinput.Model
	visCols []Column[T]

	marker func(T) string // prefix of the first column (e.g. ★)
	reveal func(T) bool   // pending Reveal, applied when items arrive

	items     []T
	visible   []T
	query     filter.Query
	filtering bool
	loading   bool
	err       error
	width     int
	height    int
}

// New creates a list. fields exposes each item to the filter language;
// placeholder documents the filterable fields to the user.
func New[T any](title, noun string, columns []Column[T], fields func(T) []filter.Field, placeholder string, th theme.Theme) *Model[T] {
	in := textinput.New()
	in.Prompt = "/ "
	in.Placeholder = placeholder
	m := &Model[T]{
		title:   title,
		noun:    noun,
		columns: columns,
		fields:  fields,
		theme:   th,
		keys:    defaultKeyMap(),
		table:   table.New(table.WithStyles(th.Table)),
		spinner: spinner.New(spinner.WithSpinner(spinner.Dot), spinner.WithStyle(th.Selected)),
		input:   in,
	}
	m.SetSize(80, 20)
	return m
}

// SetTitle changes the heading (e.g. a breadcrumb).
func (m *Model[T]) SetTitle(title string) { m.title = title }

// SetMarker sets a prefix for the first column of each row (e.g. a star
// for favorites). Call Refresh after the marked set changes.
func (m *Model[T]) SetMarker(marker func(T) string) {
	m.marker = marker
	m.refreshTable()
}

// Refresh redraws the rows (e.g. after markers changed).
func (m *Model[T]) Refresh() { m.refreshTable() }

// Reveal moves the cursor to the first item matching match, clearing a
// filter that hides it. If the items are not loaded yet, it is applied when
// they are. It reports whether the item was found now.
func (m *Model[T]) Reveal(match func(T) bool) bool {
	if m.loading || len(m.items) == 0 {
		m.reveal = match
		return false
	}
	m.reveal = nil
	if !slices.ContainsFunc(m.visible, match) && slices.ContainsFunc(m.items, match) {
		m.filtering = false
		m.input.Blur()
		m.input.SetValue("")
		m.applyFilter(true)
	}
	if i := slices.IndexFunc(m.visible, match); i >= 0 {
		m.table.SetCursor(i)
		return true
	}
	return false
}

// SetColumns replaces the columns, for lists whose shape depends on the
// data (e.g. table items).
func (m *Model[T]) SetColumns(columns []Column[T]) {
	m.columns = columns
	m.visCols = visibleColumns(columns, m.width)
	m.refreshTable()
}

// StartLoading shows the spinner; return the command from Update's caller.
func (m *Model[T]) StartLoading() tea.Cmd {
	m.loading = true
	m.err = nil
	return m.spinner.Tick
}

// SetItems ends loading and shows items, keeping the current filter.
func (m *Model[T]) SetItems(items []T) {
	m.loading = false
	m.err = nil
	m.items = items
	m.applyFilter(false)
	if m.reveal != nil {
		m.Reveal(m.reveal)
		m.reveal = nil
	}
}

// SetError ends loading and shows err instead of the table.
func (m *Model[T]) SetError(err error) {
	m.loading = false
	m.err = err
}

// Reset clears items and the filter, e.g. when the parent resource changes.
func (m *Model[T]) Reset() {
	m.items = nil
	m.err = nil
	m.filtering = false
	m.input.Blur()
	m.input.SetValue("")
	m.applyFilter(true)
}

// Items returns every loaded item.
func (m *Model[T]) Items() []T { return m.items }

// Visible returns the items that pass the filter, in table order.
func (m *Model[T]) Visible() []T { return m.visible }

// Filtering reports whether the filter input is being edited.
func (m *Model[T]) Filtering() bool { return m.filtering }

// Query returns the applied filter.
func (m *Model[T]) Query() filter.Query { return m.query }

// Capturing reports whether esc/printable keys belong to the list: while
// editing the filter, or while one is applied (esc clears it).
func (m *Model[T]) Capturing() bool { return m.filtering || !m.query.Empty() }

// Loading reports whether a load is in progress.
func (m *Model[T]) Loading() bool { return m.loading }

// Selected returns the item under the cursor.
func (m *Model[T]) Selected() (T, bool) {
	var zero T
	idx := m.table.Cursor()
	if m.loading || m.err != nil || idx < 0 || idx >= len(m.visible) {
		return zero, false
	}
	return m.visible[idx], true
}

func (m *Model[T]) Focus() { m.table.Focus() }
func (m *Model[T]) Blur()  { m.table.Blur() }

// SetSize sets the size available for View.
func (m *Model[T]) SetSize(width, height int) {
	m.width, m.height = width, height
	m.visCols = visibleColumns(m.columns, width)
	m.input.SetWidth(max(10, width-4))
	m.table.SetWidth(width)
	m.table.SetHeight(max(1, height-headerRows))
	m.refreshTable()
}

// Update handles filter editing, table navigation, spinner ticks and input
// blinks. Parents call it for keys they don't handle themselves; while
// Filtering() is true they should send every key here first.
func (m *Model[T]) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case spinner.TickMsg:
		if !m.loading {
			return nil
		}
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return cmd
	case tea.KeyPressMsg:
		return m.handleKey(msg)
	}
	if m.filtering {
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return cmd
	}
	return nil
}

func (m *Model[T]) handleKey(msg tea.KeyPressMsg) tea.Cmd {
	if m.filtering {
		switch {
		case key.Matches(msg, m.keys.ClearFilter):
			m.filtering = false
			m.input.Blur()
			m.input.SetValue("")
			m.applyFilter(true)
			return nil
		case key.Matches(msg, m.keys.ApplyFilter):
			m.filtering = false
			m.input.Blur()
			return nil
		case key.Matches(msg, m.keys.Up, m.keys.Down):
			// Arrows leave the input and move straight in the table.
			m.filtering = false
			m.input.Blur()
		default:
			var cmd tea.Cmd
			m.input, cmd = m.input.Update(msg)
			m.applyFilter(true)
			return cmd
		}
	}

	switch {
	case key.Matches(msg, m.keys.Filter):
		m.filtering = true
		return m.input.Focus()
	case key.Matches(msg, m.keys.ClearFilter) && !m.query.Empty():
		m.input.SetValue("")
		m.applyFilter(true)
		return nil
	}
	var cmd tea.Cmd
	m.table, cmd = m.table.Update(msg)
	return cmd
}

// Help returns the list's own bindings for the footer.
func (m *Model[T]) Help() []key.Binding {
	if m.filtering {
		return []key.Binding{m.keys.ApplyFilter, m.keys.ClearFilter}
	}
	if !m.query.Empty() {
		return []key.Binding{m.keys.Filter, m.keys.ClearFilter}
	}
	return []key.Binding{m.keys.Filter}
}

func (m *Model[T]) applyFilter(resetCursor bool) {
	m.query = filter.Parse(m.input.Value())
	m.visible = filter.Apply(m.query, m.items, m.fields)
	m.refreshTable()
	if resetCursor {
		m.table.SetCursor(0)
	}
}

// refreshTable rebuilds columns and rows, keeping the cursor. bubbles/table
// sets the cursor to -1 on empty rows and never restores it, and rows must
// be cleared before columns shrink.
func (m *Model[T]) refreshTable() {
	cursor := m.table.Cursor()
	m.table.SetRows(nil)
	m.table.SetColumns(tableColumns(m.visCols, m.width))
	m.table.SetRows(tableRows(m.visCols, m.visible, m.marker))
	m.table.SetCursor(max(0, cursor))
}

func (m *Model[T]) View() string {
	var b strings.Builder
	count := fmt.Sprintf("%d found", len(m.items))
	if !m.query.Empty() {
		count = fmt.Sprintf("%d of %d", len(m.visible), len(m.items))
	}
	b.WriteString(m.theme.Title.Render(m.title) + " " + m.theme.Muted.Render(count) + "\n")
	switch {
	case m.filtering:
		b.WriteString(m.input.View())
	case !m.query.Empty():
		b.WriteString(m.theme.Selected.Render("/ ") + m.query.String())
	default:
		b.WriteString(m.theme.Muted.Render("press / to filter"))
	}
	b.WriteString("\n")
	switch {
	case m.loading:
		b.WriteString(m.spinner.View() + " Loading " + m.noun + "…")
	case m.err != nil:
		b.WriteString(m.theme.StatusError.Render(m.err.Error()) + "\n\n" + m.theme.Muted.Render("Press r to retry."))
	case len(m.items) == 0:
		b.WriteString(m.theme.Muted.Render("No " + m.noun + " found."))
	case len(m.visible) == 0:
		b.WriteString(m.theme.Muted.Render("No " + m.noun + " match the filter."))
	default:
		b.WriteString(m.table.View())
	}
	return b.String()
}

func visibleColumns[T any](cols []Column[T], width int) []Column[T] {
	for len(cols) > 2 && fixedWidth(cols)+minFlexWidth+cellPadding > width {
		cols = cols[:len(cols)-1]
	}
	return cols
}

func fixedWidth[T any](cols []Column[T]) int {
	used := 0
	for _, c := range cols[1:] {
		used += c.Width + cellPadding
	}
	return used
}

func tableColumns[T any](cols []Column[T], width int) []table.Column {
	out := make([]table.Column, len(cols))
	for i, c := range cols {
		out[i] = table.Column{Title: c.Title, Width: c.Width}
	}
	if len(out) > 0 {
		out[0].Width = max(minFlexWidth, width-fixedWidth(cols)-cellPadding)
	}
	return out
}

func tableRows[T any](cols []Column[T], items []T, marker func(T) string) []table.Row {
	rows := make([]table.Row, len(items))
	for i, it := range items {
		row := make(table.Row, len(cols))
		for j, c := range cols {
			row[j] = c.Value(it)
		}
		if marker != nil && len(row) > 0 {
			row[0] = marker(it) + row[0]
		}
		rows[i] = row
	}
	return rows
}
