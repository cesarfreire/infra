// Package instances is the menu section that lists compute instances and
// runs actions on them (SSM shell, port forwarding, console, details).
package instances

import (
	"context"
	"fmt"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"infra/internal/app"
	"infra/internal/domain"
	"infra/internal/session"
	"infra/internal/tui/components/actionmenu"
	"infra/internal/tui/components/detailpanel"
	"infra/internal/tui/components/listview"
	"infra/internal/tui/components/portform"
	"infra/internal/tui/section"
	"infra/internal/tui/theme"
)

const loadTimeout = 30 * time.Second

// Service is the subset of use cases this section needs. It is satisfied by
// *app.InstanceService and by fakes in tests.
type Service interface {
	List(ctx context.Context) ([]domain.Instance, error)
	Connect(inst domain.Instance) (app.Command, error)
	PortForward(inst domain.Instance, localPort, remotePort int) (app.Command, error)
	OpenConsole(inst domain.Instance) (string, error)
	Metrics(ctx context.Context, inst domain.Instance, w domain.MetricWindow) ([]domain.Series, error)
	Start(ctx context.Context, inst domain.Instance) error
	Stop(ctx context.Context, inst domain.Instance) error
	Reboot(ctx context.Context, inst domain.Instance) error
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
	viewPortForward
)

type instancesLoadedMsg struct {
	instances []domain.Instance
	err       error
}

// changedMsg reports a change (start, stop...) made to an instance.
type changedMsg struct {
	text string
	err  error
}

// Model implements section.Section.
type Model struct {
	svc      Service
	sessions SessionStarter
	deps     section.Deps
	theme    theme.Theme
	keys     keyMap

	list    *listview.Model[domain.Instance]
	menu    actionmenu.Model
	form    portform.Model
	details *detailpanel.Model

	selected domain.Instance
	view     viewState
}

// New creates the Instances section.
func New(svc Service, sessions SessionStarter, deps section.Deps) *Model {
	th := deps.Theme
	m := &Model{
		svc:      svc,
		sessions: sessions,
		deps:     deps,
		theme:    th,
		keys:     defaultKeyMap(),
		list:     listview.New("Instances", "instances", columns, filterFields, filterPlaceholder, th),
		menu:     actionmenu.New(),
		form:     portform.New(),
		details:  detailpanel.New(th).WithPrefs(deps.Prefs),
	}
	m.menu.SetItems(m.menuItems())
	m.list.SetMarker(func(i domain.Instance) string { return deps.Star(bookmark(i)) })
	return m
}

func bookmark(i domain.Instance) domain.Bookmark {
	return domain.Bookmark{Kind: domain.KindInstance, ID: i.ID, Label: i.DisplayName()}
}

func (m *Model) Title() string { return "Instances" }
func (m *Model) Init() tea.Cmd { return m.load() }
func (m *Model) Focus()        { m.list.Focus() }
func (m *Model) Blur()         { m.list.Blur() }
func (m *Model) SetSize(width, height int) {
	m.list.SetSize(width, height)
	m.details.SetSize(width, height)
}

// InputMode captures input in nested views and while the list filter is
// being edited or applied (esc then clears the filter instead of leaving).
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
		instances, err := svc.List(ctx)
		return instancesLoadedMsg{instances: instances, err: err}
	})
}

func (m *Model) Update(msg tea.Msg) (section.Section, tea.Cmd) {
	switch msg := msg.(type) {
	case instancesLoadedMsg:
		if msg.err != nil {
			m.list.SetError(msg.err)
			return m, section.Error(msg.err)
		}
		m.list.SetItems(msg.instances)
		return m, section.Status(fmt.Sprintf("Loaded %d instances", len(msg.instances)))
	case changedMsg:
		if msg.err != nil {
			return m, section.Error(msg.err)
		}
		return m, tea.Batch(section.Status(msg.text), m.load())
	case section.RevealMsg:
		if msg.Bookmark.Kind == domain.KindInstance {
			m.details.Close()
			m.view = viewList
			m.list.Reveal(func(i domain.Instance) bool { return i.ID == msg.Bookmark.ID })
		}
		return m, nil
	case tea.KeyPressMsg:
		return m, m.handleKey(msg)
	case tea.MouseClickMsg:
		if m.view == viewActions {
			if idx, ok := m.menu.Click(msg.Y); ok {
				return m, m.runAction(m.actions()[idx], m.selected)
			}
		}
		return m, nil
	}
	// Spinner ticks, cursor blinks, metrics...
	cmd := tea.Batch(m.list.Update(msg), m.details.Update(msg))
	if m.view == viewPortForward {
		cmd = tea.Batch(cmd, m.form.Update(msg))
	}
	return m, cmd
}

func (m *Model) handleKey(msg tea.KeyPressMsg) tea.Cmd {
	switch m.view {
	case viewActions:
		if key.Matches(msg, m.keys.Back) {
			m.view = viewList
			return nil
		}
		if idx, ok := m.menu.Update(msg); ok {
			return m.runAction(m.actions()[idx], m.selected)
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
			m.details.Close()
			return m.runAction(a, m.selected)
		}
		return nil
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
	return m.handleListKey(msg)
}

func (m *Model) handleListKey(msg tea.KeyPressMsg) tea.Cmd {
	if m.list.Filtering() {
		return m.list.Update(msg)
	}
	if key.Matches(msg, m.keys.Refresh) {
		return m.load()
	}
	if inst, ok := m.list.Selected(); ok {
		switch {
		case key.Matches(msg, m.keys.Favorite):
			cmd := m.deps.ToggleFavorite(bookmark(inst))
			m.list.Refresh()
			return cmd
		case key.Matches(msg, m.keys.Select):
			m.selected = inst
			m.view = viewActions
			m.menu.SetItems(m.menuItems())
			m.deps.Visit(bookmark(inst))
			return nil
		}
		if a, ok := m.actionForKey(msg); ok {
			return a.run(m, inst)
		}
	}
	return m.list.Update(msg)
}

func (m *Model) Help() []key.Binding {
	k := m.keys
	switch m.view {
	case viewActions:
		return append(m.menu.Help(), k.Back)
	case viewDetails:
		return append(m.details.Help(), k.Connect, k.PortForward, k.Console, k.Back)
	case viewPortForward:
		return []key.Binding{m.form.NextField(), k.Start, k.Back}
	}
	if m.list.Filtering() {
		return m.list.Help()
	}
	return append([]key.Binding{k.Select, k.Connect, k.PortForward, k.Console, k.Details, k.Favorite, k.Refresh}, m.list.Help()...)
}
