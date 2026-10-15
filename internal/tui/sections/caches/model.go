// Package caches is the menu section for in-memory caches (ElastiCache).
// Caches are only reachable from inside their VPC, so the CLI and port
// forwarding go through a jump host: a running instance with the SSM agent.
package caches

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"

	"infra/internal/app"
	"infra/internal/domain"
	"infra/internal/filter"
	"infra/internal/session"
	"infra/internal/tui/components/actionmenu"
	"infra/internal/tui/components/detailpanel"
	"infra/internal/tui/components/listview"
	"infra/internal/tui/components/picker"
	"infra/internal/tui/components/portform"
	"infra/internal/tui/section"
	"infra/internal/tui/theme"
)

// Title is the menu label of this section.
const Title = "Caches"

const loadTimeout = 30 * time.Second

// Service is the subset of use cases this section needs. It is satisfied by
// *app.CacheService and by fakes in tests.
type Service interface {
	ListCaches(ctx context.Context) ([]domain.Cache, error)
	JumpHosts(ctx context.Context) ([]domain.Instance, error)
	PortForward(c domain.Cache, host domain.Instance, localPort int) (app.Command, error)
	Connect(c domain.Cache, host domain.Instance) (app.Command, error)
	OpenConsole(c domain.Cache) (string, error)
	Metrics(ctx context.Context, c domain.Cache, w domain.MetricWindow) ([]domain.Series, error)
}

// SessionStarter runs commands as sessions shown inside the TUI. It is
// satisfied by *session.Manager.
type SessionStarter interface {
	Start(kind session.Kind, title string, cmd app.Command) (*session.Session, error)
}

type viewState int

const (
	viewList viewState = iota
	viewActions
	viewDetails
	viewHostPicker
	viewPortForward
)

// pendingAction is what to do once the jump host is chosen.
type pendingAction int

const (
	pendingConnect pendingAction = iota
	pendingPortForward
)

type cachesLoadedMsg struct {
	items []domain.Cache
	err   error
}

type hostsLoadedMsg struct {
	seq   int
	hosts []domain.Instance
	err   error
}

type keyMap struct {
	Actions     key.Binding
	Connect     key.Binding
	PortForward key.Binding
	Console     key.Binding
	Details     key.Binding
	Refresh     key.Binding
	Back        key.Binding
	Start       key.Binding
	Favorite    key.Binding
}

// Model implements section.Section.
type Model struct {
	svc      Service
	sessions SessionStarter
	deps     section.Deps
	theme    theme.Theme
	keys     keyMap

	list    *listview.Model[domain.Cache]
	menu    actionmenu.Model
	form    portform.Model
	picker  *picker.Model
	spinner spinner.Model
	details *detailpanel.Model

	view         viewState
	cache        domain.Cache
	pending      pendingAction
	hosts        []domain.Instance
	host         domain.Instance
	lastHost     string // preselected in the picker
	hostsLoading bool
	hostsErr     error
	hostsSeq     int
	height       int
}

// New creates the Caches section.
func New(svc Service, sessions SessionStarter, deps section.Deps) *Model {
	th := deps.Theme
	m := &Model{
		svc:      svc,
		sessions: sessions,
		deps:     deps,
		theme:    th,
		list:     listview.New("ElastiCache", "caches", columns, fields, placeholder, th),
		menu:     actionmenu.New(),
		form:     portform.New(),
		spinner:  spinner.New(spinner.WithSpinner(spinner.Dot), spinner.WithStyle(th.Selected)),
		details:  detailpanel.New(th).WithPrefs(deps.Prefs),
		keys: keyMap{
			Actions:     key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "actions")),
			Connect:     key.NewBinding(key.WithKeys("c"), key.WithHelp("c", "cli")),
			PortForward: key.NewBinding(key.WithKeys("p"), key.WithHelp("p", "port forward")),
			Console:     key.NewBinding(key.WithKeys("o"), key.WithHelp("o", "console")),
			Details:     key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "details")),
			Refresh:     key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "refresh")),
			Back:        key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
			Favorite:    key.NewBinding(key.WithKeys("*"), key.WithHelp("*", "favorite")),
			Start:       key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "start")),
		},
	}
	m.menu.SetItems(m.menuItems())
	m.list.SetMarker(func(c domain.Cache) string { return deps.Star(bookmark(c)) })
	return m
}

func bookmark(c domain.Cache) domain.Bookmark {
	return domain.Bookmark{Kind: domain.KindCache, ID: c.ID, Label: c.ID}
}

// jumpHostPref keys the jump host remembered for a cache.
func jumpHostPref(cacheID string) string { return "jumphost." + cacheID }

func (m *Model) Title() string { return Title }
func (m *Model) Init() tea.Cmd { return m.load() }
func (m *Model) Focus()        { m.list.Focus() }
func (m *Model) Blur()         { m.list.Blur() }

func (m *Model) SetSize(width, height int) {
	m.list.SetSize(width, height)
	m.details.SetSize(width, height)
	m.height = height
}

func (m *Model) InputMode() section.InputMode {
	if m.view != viewList || m.list.Capturing() {
		return section.ModeCapture
	}
	return section.ModeNormal
}

func (m *Model) load() tea.Cmd {
	svc := m.svc
	return tea.Batch(m.list.StartLoading(), func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), loadTimeout)
		defer cancel()
		items, err := svc.ListCaches(ctx)
		return cachesLoadedMsg{items: items, err: err}
	})
}

func (m *Model) Update(msg tea.Msg) (section.Section, tea.Cmd) {
	switch msg := msg.(type) {
	case cachesLoadedMsg:
		if msg.err != nil {
			m.list.SetError(msg.err)
			return m, section.Error(msg.err)
		}
		m.list.SetItems(msg.items)
		return m, section.Status(fmt.Sprintf("Loaded %d caches", len(msg.items)))
	case hostsLoadedMsg:
		return m, m.handleHosts(msg)
	case section.RevealMsg:
		if msg.Bookmark.Kind == domain.KindCache {
			m.details.Close()
			m.view = viewList
			m.list.Reveal(func(c domain.Cache) bool { return c.ID == msg.Bookmark.ID })
		}
		return m, nil
	case tea.KeyPressMsg:
		return m, m.handleKey(msg)
	case tea.MouseClickMsg:
		return m, m.handleClick(msg)
	case spinner.TickMsg:
		var cmd tea.Cmd
		if m.hostsLoading {
			m.spinner, cmd = m.spinner.Update(msg)
		}
		return m, tea.Batch(cmd, m.list.Update(msg))
	}
	cmds := []tea.Cmd{m.list.Update(msg), m.details.Update(msg)}
	switch {
	case m.view == viewPortForward:
		cmds = append(cmds, m.form.Update(msg))
	case m.view == viewHostPicker && m.picker != nil:
		_, cmd := m.picker.Update(msg)
		cmds = append(cmds, cmd)
	}
	return m, tea.Batch(cmds...)
}

func (m *Model) handleKey(msg tea.KeyPressMsg) tea.Cmd {
	switch m.view {
	case viewActions:
		if key.Matches(msg, m.keys.Back) {
			m.view = viewList
			return nil
		}
		if idx, ok := m.menu.Update(msg); ok {
			return m.runAction(m.actions()[idx], m.cache)
		}
		return nil
	case viewDetails:
		if cmd, ok := m.details.HandleKey(msg); ok {
			return cmd
		}
		if key.Matches(msg, m.keys.Back) {
			m.details.Close()
			m.view = viewList
			return nil
		}
		if a, ok := m.actionForKey(msg); ok {
			return m.runAction(a, m.cache)
		}
		return nil
	case viewHostPicker:
		return m.handlePickerKey(msg)
	case viewPortForward:
		switch {
		case key.Matches(msg, m.keys.Back):
			m.view = viewList
			return nil
		case key.Matches(msg, m.keys.Start):
			return m.submitPortForward()
		}
		return m.form.Update(msg)
	}

	if m.list.Filtering() {
		return m.list.Update(msg)
	}
	if key.Matches(msg, m.keys.Refresh) {
		return m.load()
	}
	if c, ok := m.list.Selected(); ok {
		if key.Matches(msg, m.keys.Favorite) {
			cmd := m.deps.ToggleFavorite(bookmark(c))
			m.list.Refresh()
			return cmd
		}
		if key.Matches(msg, m.keys.Actions) {
			m.deps.Visit(bookmark(c))
			m.cache = c
			m.menu.SetItems(m.menuItems())
			m.view = viewActions
			return nil
		}
		if a, ok := m.actionForKey(msg); ok {
			return m.runAction(a, c)
		}
	}
	return m.list.Update(msg)
}

func (m *Model) handleClick(msg tea.MouseClickMsg) tea.Cmd {
	switch m.view {
	case viewActions:
		if idx, ok := m.menu.Click(msg.Y); ok {
			return m.runAction(m.actions()[idx], m.cache)
		}
	case viewHostPicker:
		if m.picker != nil && m.picker.Click(msg.Y) {
			return m.chooseHost()
		}
	}
	return nil
}

func (m *Model) Help() []key.Binding {
	k := m.keys
	switch m.view {
	case viewActions:
		return append(m.menu.Help(), k.Back)
	case viewDetails:
		return append(m.details.Help(), k.Connect, k.PortForward, k.Console, k.Back)
	case viewHostPicker:
		if m.picker != nil {
			return m.picker.Help()
		}
		return []key.Binding{k.Back}
	case viewPortForward:
		return []key.Binding{k.Start, k.Back}
	}
	if m.list.Filtering() {
		return m.list.Help()
	}
	return append([]key.Binding{k.Actions, k.Connect, k.PortForward, k.Console, k.Details, k.Favorite, k.Refresh}, m.list.Help()...)
}

var columns = []listview.Column[domain.Cache]{
	{Title: "Cache", Value: func(c domain.Cache) string { return c.ID }},
	{Title: "Engine", Width: 16, Value: engineLabel},
	{Title: "Status", Width: 10, Value: func(c domain.Cache) string { return c.Status }},
	{Title: "Type", Width: 18, Value: nodeLabel},
	{Title: "TLS", Width: 4, Value: func(c domain.Cache) string { return yesNo(c.TLS) }},
	{Title: "Endpoint", Width: 48, Value: endpointLabel},
}

const placeholder = "id, engine:valkey, status:available, type:r7g, tls:yes, mode:cluster"

func fields(c domain.Cache) []filter.Field {
	mode := "single"
	switch {
	case c.Serverless:
		mode = "serverless"
	case c.ClusterMode:
		mode = "cluster"
	}
	return []filter.Field{
		{Name: "id", Value: c.ID},
		{Name: "engine", Value: engineLabel(c)},
		{Name: "status", Value: c.Status},
		{Name: "type", Value: nodeLabel(c)},
		{Name: "tls", Value: yesNo(c.TLS)},
		{Name: "mode", Value: mode},
		{Name: "endpoint", Value: endpointLabel(c)},
	}
}

func engineLabel(c domain.Cache) string {
	if c.EngineVersion == "" {
		return c.Engine
	}
	return c.Engine + " " + c.EngineVersion
}

func nodeLabel(c domain.Cache) string {
	if c.Serverless {
		return "serverless"
	}
	if c.Nodes > 1 {
		return fmt.Sprintf("%s ×%d", c.NodeType, c.Nodes)
	}
	return c.NodeType
}

func endpointLabel(c domain.Cache) string {
	if c.Endpoint.Host == "" {
		return ""
	}
	return c.Endpoint.Host + ":" + strconv.Itoa(c.Endpoint.Port)
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}
