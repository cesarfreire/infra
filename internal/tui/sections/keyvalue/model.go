// Package keyvalue is the menu section for key-value tables (DynamoDB):
// list tables, browse their items page by page, find items by key and
// inspect them as JSON.
package keyvalue

import (
	"context"
	"fmt"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"

	"infra/internal/app"
	"infra/internal/domain"
	"infra/internal/tui/components/detailpanel"
	"infra/internal/tui/components/listview"
	"infra/internal/tui/section"
	"infra/internal/tui/theme"
)

// Title is the menu label of this section.
const Title = "Key-value DBs"

const loadTimeout = 30 * time.Second

// Service is the subset of use cases this section needs. It is satisfied by
// *app.KeyValueService and by fakes in tests.
type Service interface {
	ListTables(ctx context.Context) ([]domain.Table, error)
	Scan(ctx context.Context, table domain.Table, cursor domain.Cursor) (domain.ItemPage, error)
	Query(ctx context.Context, table domain.Table, q domain.KeyQuery, cursor domain.Cursor) (domain.ItemPage, error)
	OpenConsole(table domain.Table) (string, error)
	Metrics(ctx context.Context, table domain.Table, w domain.MetricWindow) ([]domain.Series, error)
	SaveItem(ctx context.Context, table domain.Table, original domain.Item, editedJSON string) (domain.Item, error)
	DeleteItem(ctx context.Context, table domain.Table, item domain.Item) error
}

type level int

const (
	levelTables level = iota
	levelItems
)

type viewState int

const (
	viewList viewState = iota
	viewTableDetails
	viewQuery
	viewItem
	viewEdit
)

// itemChangedMsg reports a saved or deleted item.
type itemChangedMsg struct {
	text    string
	err     error
	saved   domain.Item // the item written, or the one deleted
	deleted bool
}

type tablesLoadedMsg struct {
	items []domain.Table
	err   error
}

type itemsLoadedMsg struct {
	seq  int
	page domain.ItemPage
	more bool // appends to the loaded items
	err  error
}

// Model implements section.Section.
type Model struct {
	svc    Service
	deps   section.Deps
	theme  theme.Theme
	keys   keyMap
	editor textarea.Model
	item   domain.Item // item shown or edited

	tables  *listview.Model[domain.Table]
	items   *listview.Model[domain.Item]
	form    queryForm
	viewer  viewport.Model
	details *detailpanel.Model

	level level
	view  viewState

	table       domain.Table     // table being browsed or detailed
	query       *domain.KeyQuery // nil when scanning
	loaded      []domain.Item
	next        domain.Cursor
	loadingMore bool
	seq         int // identifies the current browse; older results are dropped
}

// New creates the Key-value DBs section.
func New(svc Service, deps section.Deps) *Model {
	th := deps.Theme
	editor := textarea.New()
	editor.CharLimit = 0 // DynamoDB items go up to 400 KB
	editor.MaxHeight = 0
	m := &Model{
		svc:     svc,
		deps:    deps,
		editor:  editor,
		theme:   th,
		keys:    defaultKeyMap(),
		tables:  listview.New("DynamoDB tables", "tables", tableColumns, tableFields, tablePlaceholder, th),
		items:   listview.New("Items", "items", nil, itemFields, itemPlaceholder, th),
		form:    newQueryForm(),
		viewer:  viewport.New(),
		details: detailpanel.New(th).WithPrefs(deps.Prefs),
	}
	m.tables.SetMarker(func(t domain.Table) string { return deps.Star(tableBookmark(t.Name)) })
	return m
}

func tableBookmark(name string) domain.Bookmark {
	return domain.Bookmark{Kind: domain.KindTable, ID: name, Label: name}
}

func (m *Model) Title() string { return Title }
func (m *Model) Init() tea.Cmd { return m.loadTables() }

func (m *Model) Focus() {
	m.tables.Focus()
	m.items.Focus()
}

func (m *Model) Blur() {
	m.tables.Blur()
	m.items.Blur()
}

func (m *Model) SetSize(width, height int) {
	m.tables.SetSize(width, height)
	m.items.SetSize(width, height)
	m.viewer.SetWidth(width)
	m.viewer.SetHeight(max(1, height-2)) // heading + blank line
	m.details.SetSize(width, height)
	m.editor.SetWidth(width)
	m.editor.SetHeight(max(3, height-4)) // heading, blank and hint lines
}

// InputMode is normal only on the tables list without a filter.
func (m *Model) InputMode() section.InputMode {
	if m.view == viewList && m.level == levelTables && !m.tables.Capturing() {
		return section.ModeNormal
	}
	return section.ModeCapture
}

func (m *Model) loadTables() tea.Cmd {
	svc := m.svc
	return tea.Batch(m.tables.StartLoading(), func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), loadTimeout)
		defer cancel()
		items, err := svc.ListTables(ctx)
		return tablesLoadedMsg{items: items, err: err}
	})
}

// browse opens the items of table: a scan, or a query when q is set.
func (m *Model) browse(t domain.Table, q *domain.KeyQuery) tea.Cmd {
	m.table, m.query = t, q
	m.loaded, m.next = nil, nil
	m.seq++
	m.items.Reset()
	m.items.SetColumns(itemColumns(t, nil))
	m.items.SetTitle(m.itemsTitle())
	m.level, m.view = levelItems, viewList
	return tea.Batch(m.items.StartLoading(), m.loadItems(false))
}

// loadItems reads the next page; more appends it to the loaded items.
func (m *Model) loadItems(more bool) tea.Cmd {
	svc, seq, table, query, cursor := m.svc, m.seq, m.table, m.query, m.next
	m.loadingMore = more
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), loadTimeout)
		defer cancel()
		var page domain.ItemPage
		var err error
		if query != nil {
			page, err = svc.Query(ctx, table, *query, cursor)
		} else {
			page, err = svc.Scan(ctx, table, cursor)
		}
		return itemsLoadedMsg{seq: seq, page: page, more: more, err: err}
	}
}

func (m *Model) itemsTitle() string {
	title := m.table.Name + " › scan"
	if q := m.query; q != nil {
		title = fmt.Sprintf("%s › %s = %s", m.table.Name, m.table.PartitionKey.Name, q.PartitionValue)
		if q.SortKeyPrefix != "" {
			title += fmt.Sprintf(", %s begins with %s", sortKeyName(m.table), q.SortKeyPrefix)
		}
	}
	switch {
	case m.loadingMore:
		title += " · loading more…"
	case m.next != nil:
		title += " · more available (n)"
	}
	return title
}

func (m *Model) Update(msg tea.Msg) (section.Section, tea.Cmd) {
	switch msg := msg.(type) {
	case tablesLoadedMsg:
		if msg.err != nil {
			m.tables.SetError(msg.err)
			return m, section.Error(msg.err)
		}
		m.tables.SetItems(msg.items)
		return m, section.Status(fmt.Sprintf("Loaded %d tables", len(msg.items)))
	case itemsLoadedMsg:
		return m, m.handleItems(msg)
	case itemChangedMsg:
		return m, m.handleItemChanged(msg)
	case section.RevealMsg:
		if msg.Bookmark.Kind == domain.KindTable {
			m.details.Close()
			m.level, m.view = levelTables, viewList
			m.tables.Reveal(func(t domain.Table) bool { return t.Name == msg.Bookmark.ID })
		}
		return m, nil
	case tea.KeyPressMsg:
		return m, m.handleKey(msg)
	}
	cmds := []tea.Cmd{m.tables.Update(msg), m.items.Update(msg), m.details.Update(msg)}
	switch m.view {
	case viewQuery:
		cmds = append(cmds, m.form.update(msg))
	case viewEdit:
		var cmd tea.Cmd
		m.editor, cmd = m.editor.Update(msg)
		cmds = append(cmds, cmd)
	}
	return m, tea.Batch(cmds...)
}

func (m *Model) handleItems(msg itemsLoadedMsg) tea.Cmd {
	if msg.seq != m.seq {
		return nil // stale: the user browsed something else
	}
	m.loadingMore = false
	if msg.err != nil {
		if !msg.more {
			m.items.SetError(msg.err)
		}
		m.items.SetTitle(m.itemsTitle())
		return section.Error(msg.err)
	}
	if !msg.more {
		m.loaded = nil
	}
	m.loaded = append(m.loaded, msg.page.Items...)
	m.next = msg.page.Next
	m.items.SetColumns(itemColumns(m.table, m.loaded))
	m.items.SetItems(m.loaded)
	m.items.SetTitle(m.itemsTitle())
	status := fmt.Sprintf("Loaded %d items", len(m.loaded))
	if m.next != nil {
		status += "; press n for more"
	}
	return section.Status(status)
}

func (m *Model) handleKey(msg tea.KeyPressMsg) tea.Cmd {
	switch m.view {
	case viewItem:
		switch {
		case key.Matches(msg, m.keys.Back):
			m.view = viewList
			return nil
		case key.Matches(msg, m.keys.Edit):
			return m.openEditor()
		case key.Matches(msg, m.keys.Delete):
			return m.confirmDelete(m.item)
		}
		var cmd tea.Cmd
		m.viewer, cmd = m.viewer.Update(msg)
		return cmd
	case viewEdit:
		switch {
		case key.Matches(msg, m.keys.Back):
			m.view = viewItem
			return nil
		case key.Matches(msg, m.keys.Save):
			return m.confirmSave()
		}
		var cmd tea.Cmd
		m.editor, cmd = m.editor.Update(msg)
		return cmd
	case viewTableDetails:
		if cmd, ok := m.details.HandleKey(msg); ok {
			return cmd
		}
		switch {
		case key.Matches(msg, m.keys.Back):
			m.details.Close()
			m.view = viewList
		case key.Matches(msg, m.keys.Console):
			return m.openConsole(m.table)
		case key.Matches(msg, m.keys.Find):
			m.details.Close()
			return m.openQuery(m.table)
		}
		return nil
	case viewQuery:
		switch {
		case key.Matches(msg, m.keys.Back):
			m.view = viewList
			return nil
		case key.Matches(msg, m.keys.Run):
			q := m.form.query()
			if err := app.ValidateKeyQuery(m.table, q); err != nil {
				m.form.err = err
				return nil
			}
			return m.browse(m.table, &q)
		case key.Matches(msg, m.keys.Next):
			return m.form.toggle()
		}
		return m.form.update(msg)
	}
	if m.level == levelItems {
		return m.handleItemsKey(msg)
	}
	return m.handleTablesKey(msg)
}

func (m *Model) handleTablesKey(msg tea.KeyPressMsg) tea.Cmd {
	if m.tables.Filtering() {
		return m.tables.Update(msg)
	}
	if key.Matches(msg, m.keys.Refresh) {
		return m.loadTables()
	}
	if t, ok := m.tables.Selected(); ok {
		switch {
		case key.Matches(msg, m.keys.Favorite):
			cmd := m.deps.ToggleFavorite(tableBookmark(t.Name))
			m.tables.Refresh()
			return cmd
		case key.Matches(msg, m.keys.Browse):
			m.deps.Visit(tableBookmark(t.Name))
			return m.browse(t, nil)
		case key.Matches(msg, m.keys.Find):
			return m.openQuery(t)
		case key.Matches(msg, m.keys.Details):
			m.deps.Visit(tableBookmark(t.Name))
			m.table = t
			m.view = viewTableDetails
			svc := m.svc
			return m.details.Open(m.tableDetailsHeader(), func(ctx context.Context, w domain.MetricWindow) ([]domain.Series, error) {
				return svc.Metrics(ctx, t, w)
			}, "")
		case key.Matches(msg, m.keys.Console):
			return m.openConsole(t)
		}
	}
	return m.tables.Update(msg)
}

func (m *Model) handleItemsKey(msg tea.KeyPressMsg) tea.Cmd {
	if m.items.Filtering() {
		return m.items.Update(msg)
	}
	switch {
	case key.Matches(msg, m.keys.Back):
		if !m.items.Query().Empty() {
			return m.items.Update(msg) // clears the filter
		}
		m.level = levelTables
		return nil
	case key.Matches(msg, m.keys.Refresh):
		return m.browse(m.table, m.query)
	case key.Matches(msg, m.keys.More):
		if m.next == nil || m.loadingMore {
			return nil
		}
		cmd := m.loadItems(true)
		m.items.SetTitle(m.itemsTitle())
		return cmd
	case key.Matches(msg, m.keys.Find):
		return m.openQuery(m.table)
	case key.Matches(msg, m.keys.Console):
		return m.openConsole(m.table)
	case key.Matches(msg, m.keys.Inspect):
		if it, ok := m.items.Selected(); ok {
			m.showItem(it)
		}
		return nil
	case key.Matches(msg, m.keys.Edit):
		if it, ok := m.items.Selected(); ok {
			m.item = it
			return m.openEditor()
		}
		return nil
	case key.Matches(msg, m.keys.Delete):
		if it, ok := m.items.Selected(); ok {
			return m.confirmDelete(it)
		}
		return nil
	}
	return m.items.Update(msg)
}

func (m *Model) openQuery(t domain.Table) tea.Cmd {
	m.table = t
	m.view = viewQuery
	return m.form.reset(t)
}

func (m *Model) openConsole(t domain.Table) tea.Cmd {
	return func() tea.Msg {
		url, err := m.svc.OpenConsole(t)
		if err != nil {
			return section.StatusMsg{Text: fmt.Sprintf("%v (%s)", err, url), Error: true}
		}
		return section.StatusMsg{Text: "Opened console for " + t.Name}
	}
}

func (m *Model) Help() []key.Binding {
	k := m.keys
	switch m.view {
	case viewItem:
		return []key.Binding{k.Scroll, k.Edit, k.Delete, k.Back}
	case viewEdit:
		return []key.Binding{k.Save, k.Back}
	case viewTableDetails:
		return append(m.details.Help(), k.Find, k.Console, k.Back)
	case viewQuery:
		if m.form.hasSK {
			return []key.Binding{k.Run, k.Next, k.Back}
		}
		return []key.Binding{k.Run, k.Back}
	}
	if m.level == levelItems {
		if m.items.Filtering() {
			return m.items.Help()
		}
		b := []key.Binding{k.Inspect, k.Edit, k.Delete}
		if m.next != nil {
			b = append(b, k.More)
		}
		return append(append(b, k.Find, k.Console, k.Refresh, k.Back), m.items.Help()...)
	}
	if m.tables.Filtering() {
		return m.tables.Help()
	}
	return append([]key.Binding{k.Browse, k.Find, k.Details, k.Console, k.Favorite, k.Refresh}, m.tables.Help()...)
}
