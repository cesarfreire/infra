// Package containers is the menu section for container workloads (ECS):
// drill down from clusters to services to tasks, and open shells or port
// forwards into containers.
package containers

import (
	"context"
	"fmt"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"

	"infra/internal/app"
	"infra/internal/domain"
	"infra/internal/filter"
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
// *app.ContainerService and by fakes in tests.
type Service interface {
	ListClusters(ctx context.Context) ([]domain.Cluster, error)
	ListServices(ctx context.Context, cluster string) ([]domain.Service, error)
	ListTasks(ctx context.Context, cluster, service string) ([]domain.Task, error)
	Connect(task domain.Task, container domain.Container) (app.Command, error)
	PortForward(task domain.Task, container domain.Container, localPort, remotePort int) (app.Command, error)
	OpenClusterConsole(c domain.Cluster) (string, error)
	OpenServiceConsole(s domain.Service) (string, error)
	OpenTaskConsole(t domain.Task) (string, error)
	ServiceMetrics(ctx context.Context, s domain.Service, w domain.MetricWindow) ([]domain.Series, error)
	TaskMetrics(ctx context.Context, t domain.Task, w domain.MetricWindow) ([]domain.Series, error)
	GetService(ctx context.Context, cluster, name string) (domain.Service, error)
	ForceDeployment(ctx context.Context, s domain.Service) error
	SetDesiredCount(ctx context.Context, s domain.Service, count int) error
	Rollout(ctx context.Context, s domain.Service) (domain.Rollout, error)
	PreviousRevision(ctx context.Context, s domain.Service) (app.RevisionChange, error)
	Rollback(ctx context.Context, s domain.Service, revision domain.TaskDefinition) error
}

// SessionStarter runs commands as sessions shown inside the TUI. It is
// satisfied by *session.Manager.
type SessionStarter interface {
	Start(kind session.Kind, title string, cmd app.Command) (*session.Session, error)
}

// level is the depth of the drill-down.
type level int

const (
	levelClusters level = iota
	levelServices
	levelTasks
)

type viewState int

const (
	viewList viewState = iota
	viewActions
	viewPicker
	viewServiceDetails
	viewTaskDetails
	viewPortForward
	viewScale
	viewWatch
)

// serviceRefresh is how often open service details are re-read, to follow
// deployments.
const serviceRefresh = 10 * time.Second

// changedMsg reports a change (deploy, scale) made to a service.
type changedMsg struct {
	text     string
	err      error
	deployed *domain.Service // set after a deployment, to follow it
}

type serviceTickMsg struct{ seq int }

type serviceReadMsg struct {
	seq int
	svc domain.Service
	err error
}

// pendingAction is what to do once a container is chosen in the picker.
type pendingAction int

const (
	pendingConnect pendingAction = iota
	pendingPortForward
)

type clustersLoadedMsg struct {
	items []domain.Cluster
	err   error
}

type servicesLoadedMsg struct {
	cluster string
	items   []domain.Service
	err     error
}

type tasksLoadedMsg struct {
	cluster, service string
	items            []domain.Task
	err              error
}

// list is what the section needs from any of its listview.Model[T].
type list interface {
	Filtering() bool
	Capturing() bool
	Query() filter.Query
	Update(tea.Msg) tea.Cmd
	View() string
	Help() []key.Binding
	Focus()
	Blur()
	SetSize(width, height int)
}

// Model implements section.Section.
type Model struct {
	svc      Service
	sessions SessionStarter
	deps     section.Deps
	theme    theme.Theme
	keys     keyMap
	scale    textinput.Model
	width    int
	svcSeq   int // identifies the open service details; bumped when leaving
	height   int

	watch     watchState
	watchVP   viewport.Model
	watchKeys watchKeys

	clusters *listview.Model[domain.Cluster]
	services *listview.Model[domain.Service]
	tasks    *listview.Model[domain.Task]
	menu     actionmenu.Model
	picker   actionmenu.Model
	form     portform.Model
	details  *detailpanel.Model

	level       level
	tasksParent level  // where esc goes from the tasks level
	cluster     string // selected cluster name
	service     string // selected service name; empty lists all tasks
	view        viewState

	task      domain.Task        // target of task views
	svcDetail domain.Service     // target of the service details view
	choices   []domain.Container // picker entries
	container domain.Container   // chosen container
	pending   pendingAction
}

// New creates the Containers section.
func New(svc Service, sessions SessionStarter, deps section.Deps) *Model {
	th := deps.Theme
	scale := textinput.New()
	scale.Prompt = "Desired tasks: "
	scale.CharLimit = 4
	m := &Model{
		svc:       svc,
		sessions:  sessions,
		deps:      deps,
		scale:     scale,
		watchVP:   viewport.New(),
		watchKeys: defaultWatchKeys(),
		theme:     th,
		keys:      defaultKeyMap(),
		clusters:  listview.New("ECS clusters", "clusters", clusterColumns, clusterFields, clusterPlaceholder, th),
		services:  listview.New("Services", "services", serviceColumns, serviceFields, servicePlaceholder, th),
		tasks:     listview.New("Tasks", "running tasks", taskColumns, taskFields, taskPlaceholder, th),
		menu:      actionmenu.New(),
		picker:    actionmenu.New(),
		form:      portform.New(),
		details:   detailpanel.New(th).WithPrefs(deps.Prefs),
	}
	m.menu.SetItems(m.taskMenuItems())
	m.clusters.SetMarker(func(c domain.Cluster) string { return deps.Star(clusterBookmark(c.Name)) })
	m.services.SetMarker(func(s domain.Service) string { return deps.Star(serviceBookmark(s)) })
	return m
}

func clusterBookmark(name string) domain.Bookmark {
	return domain.Bookmark{Kind: domain.KindECSCluster, ID: name, Label: name}
}

func serviceBookmark(s domain.Service) domain.Bookmark {
	return domain.Bookmark{Kind: domain.KindECSService, ID: s.Name, Parent: s.Cluster, Label: s.Cluster + "/" + s.Name}
}

func (m *Model) Title() string { return "Containers" }
func (m *Model) Init() tea.Cmd { return m.loadClusters() }

func (m *Model) lists() []list { return []list{m.clusters, m.services, m.tasks} }

func (m *Model) current() list { return m.lists()[m.level] }

func (m *Model) Focus() {
	for _, l := range m.lists() {
		l.Focus()
	}
}

func (m *Model) Blur() {
	for _, l := range m.lists() {
		l.Blur()
	}
}

func (m *Model) SetSize(width, height int) {
	for _, l := range m.lists() {
		l.SetSize(width, height)
	}
	m.width, m.height = width, height
	m.details.SetSize(width, height)
	m.watchVP.SetWidth(width)
	m.watchVP.SetHeight(max(1, height))
	if m.view == viewWatch {
		m.renderWatch()
	}
	if m.view == viewServiceDetails {
		m.details.SetHeader(m.serviceDetailsHeader())
	}
}

// InputMode is normal only on the clusters list without a filter; deeper
// levels capture esc to go up.
func (m *Model) InputMode() section.InputMode {
	if m.view == viewList && m.level == levelClusters && !m.clusters.Capturing() {
		return section.ModeNormal
	}
	return section.ModeCapture
}

func (m *Model) loadClusters() tea.Cmd {
	svc := m.svc
	return tea.Batch(m.clusters.StartLoading(), func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), loadTimeout)
		defer cancel()
		items, err := svc.ListClusters(ctx)
		return clustersLoadedMsg{items: items, err: err}
	})
}

func (m *Model) loadServices() tea.Cmd {
	svc, cluster := m.svc, m.cluster
	return tea.Batch(m.services.StartLoading(), func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), loadTimeout)
		defer cancel()
		items, err := svc.ListServices(ctx, cluster)
		return servicesLoadedMsg{cluster: cluster, items: items, err: err}
	})
}

func (m *Model) loadTasks() tea.Cmd {
	svc, cluster, service := m.svc, m.cluster, m.service
	return tea.Batch(m.tasks.StartLoading(), func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), loadTimeout)
		defer cancel()
		items, err := svc.ListTasks(ctx, cluster, service)
		return tasksLoadedMsg{cluster: cluster, service: service, items: items, err: err}
	})
}

func (m *Model) reload() tea.Cmd {
	switch m.level {
	case levelServices:
		return m.loadServices()
	case levelTasks:
		return m.loadTasks()
	}
	return m.loadClusters()
}

func (m *Model) Update(msg tea.Msg) (section.Section, tea.Cmd) {
	switch msg := msg.(type) {
	case clustersLoadedMsg:
		return m, loaded(m.clusters, msg.items, msg.err, "clusters")
	case servicesLoadedMsg:
		if msg.cluster != m.cluster {
			return m, nil // stale: the user moved to another cluster
		}
		return m, loaded(m.services, msg.items, msg.err, "services")
	case tasksLoadedMsg:
		if msg.cluster != m.cluster || msg.service != m.service {
			return m, nil
		}
		return m, loaded(m.tasks, msg.items, msg.err, "tasks")
	case changedMsg:
		if msg.err != nil {
			return m, section.Error(msg.err)
		}
		cmds := []tea.Cmd{section.Status(msg.text)}
		if msg.deployed != nil {
			cmds = append(cmds, m.openWatch(*msg.deployed))
			return m, tea.Batch(cmds...)
		}
		if m.level == levelServices {
			cmds = append(cmds, m.loadServices())
		}
		if m.view == viewServiceDetails {
			cmds = append(cmds, m.readService())
		}
		return m, tea.Batch(cmds...)
	case rolloutMsg:
		return m, m.handleRollout(msg)
	case revisionMsg:
		return m, m.handleRevision(msg)
	case watchTickMsg:
		if msg.seq == m.watch.seq && m.view == viewWatch && !m.watch.paused {
			return m, m.readRollout()
		}
		return m, nil
	case serviceTickMsg:
		if msg.seq == m.svcSeq && m.view == viewServiceDetails {
			return m, m.readService()
		}
		return m, nil
	case serviceReadMsg:
		if msg.seq != m.svcSeq || m.view != viewServiceDetails {
			return m, nil
		}
		seq := m.svcSeq
		next := tea.Tick(serviceRefresh, func(time.Time) tea.Msg { return serviceTickMsg{seq: seq} })
		if msg.err != nil {
			return m, next // keep the last view; the metrics panel reports access problems
		}
		m.svcDetail = msg.svc
		m.details.SetHeader(m.serviceDetailsHeader())
		return m, next
	case section.RevealMsg:
		return m, m.reveal(msg.Bookmark)
	case tea.KeyPressMsg:
		return m, m.handleKey(msg)
	case tea.MouseClickMsg:
		return m, m.handleClick(msg)
	}
	// Spinner ticks, cursor blinks...
	cmds := []tea.Cmd{m.clusters.Update(msg), m.services.Update(msg), m.tasks.Update(msg), m.details.Update(msg)}
	switch m.view {
	case viewPortForward:
		cmds = append(cmds, m.form.Update(msg))
	case viewScale:
		var cmd tea.Cmd
		m.scale, cmd = m.scale.Update(msg)
		cmds = append(cmds, cmd)
	}
	return m, tea.Batch(cmds...)
}

// reveal shows a bookmarked cluster or service.
func (m *Model) reveal(b domain.Bookmark) tea.Cmd {
	switch b.Kind {
	case domain.KindECSCluster:
		m.leaveView()
		m.level = levelClusters
		m.clusters.Reveal(func(c domain.Cluster) bool { return c.Name == b.ID })
	case domain.KindECSService:
		m.leaveView()
		cmd := m.openServices(b.Parent)
		m.services.Reveal(func(s domain.Service) bool { return s.Name == b.ID })
		return cmd
	}
	return nil
}

// leaveView returns to the current list, stopping detail and deployment
// refreshes.
func (m *Model) leaveView() {
	m.details.Close()
	m.svcSeq++
	m.watch.seq++
	m.view = viewList
}

// readService re-reads the service shown in the details.
func (m *Model) readService() tea.Cmd {
	svc, seq, s := m.svc, m.svcSeq, m.svcDetail
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), loadTimeout)
		defer cancel()
		fresh, err := svc.GetService(ctx, s.Cluster, s.Name)
		return serviceReadMsg{seq: seq, svc: fresh, err: err}
	}
}

func loaded[T any](l *listview.Model[T], items []T, err error, noun string) tea.Cmd {
	if err != nil {
		l.SetError(err)
		return section.Error(err)
	}
	l.SetItems(items)
	return section.Status(fmt.Sprintf("Loaded %d %s", len(items), noun))
}

func (m *Model) handleClick(msg tea.MouseClickMsg) tea.Cmd {
	switch m.view {
	case viewActions:
		if idx, ok := m.menu.Click(msg.Y); ok {
			return m.runTaskAction(m.taskActions()[idx], m.task)
		}
	case viewPicker:
		if idx, ok := m.picker.Click(msg.Y); ok {
			return m.useContainer(m.choices[idx])
		}
	}
	return nil
}

func (m *Model) handleKey(msg tea.KeyPressMsg) tea.Cmd {
	// esc first closes the diff shown over the deployment view.
	if m.view == viewWatch && m.watch.showDiff && key.Matches(msg, m.keys.Back) {
		m.watch.showDiff = false
		m.renderWatch()
		return nil
	}
	if m.view == viewTaskDetails || m.view == viewServiceDetails {
		if cmd, ok := m.details.HandleKey(msg); ok {
			return cmd
		}
	}
	if m.view != viewList && key.Matches(msg, m.keys.Back) {
		m.leaveView()
		return nil
	}
	switch m.view {
	case viewActions:
		if idx, ok := m.menu.Update(msg); ok {
			return m.runTaskAction(m.taskActions()[idx], m.task)
		}
		return nil
	case viewPicker:
		if idx, ok := m.picker.Update(msg); ok {
			return m.useContainer(m.choices[idx])
		}
		return nil
	case viewTaskDetails:
		if a, ok := m.taskActionForKey(msg); ok {
			return m.runTaskAction(a, m.task)
		}
		return nil
	case viewWatch:
		return m.handleWatchKey(msg)
	case viewServiceDetails:
		switch {
		case key.Matches(msg, m.keys.Watch):
			return m.openWatch(m.svcDetail)
		case key.Matches(msg, m.keys.Deploy):
			return m.confirmDeploy(m.svcDetail)
		case key.Matches(msg, m.keys.Scale):
			return m.openScale(m.svcDetail)
		}
		return nil
	case viewScale:
		if key.Matches(msg, m.keys.Start) {
			return m.submitScale()
		}
		var cmd tea.Cmd
		m.scale, cmd = m.scale.Update(msg)
		return cmd
	case viewPortForward:
		if key.Matches(msg, m.keys.Start) {
			return m.submitPortForward()
		}
		return m.form.Update(msg)
	}
	return m.handleListKey(msg)
}

func (m *Model) handleListKey(msg tea.KeyPressMsg) tea.Cmd {
	l := m.current()
	if l.Filtering() {
		return l.Update(msg)
	}
	switch {
	case key.Matches(msg, m.keys.Back):
		if !l.Query().Empty() {
			return l.Update(msg) // clears the filter
		}
		m.up()
		return nil
	case key.Matches(msg, m.keys.Refresh):
		return m.reload()
	case key.Matches(msg, m.keys.AllTasks) && m.level == levelServices:
		return m.openTasks("")
	}

	switch m.level {
	case levelClusters:
		if c, ok := m.clusters.Selected(); ok {
			switch {
			case key.Matches(msg, m.keys.Favorite):
				cmd := m.deps.ToggleFavorite(clusterBookmark(c.Name))
				m.clusters.Refresh()
				return cmd
			case key.Matches(msg, m.keys.OpenServices):
				m.deps.Visit(clusterBookmark(c.Name))
				return m.openServices(c.Name)
			case key.Matches(msg, m.keys.AllTasks):
				m.cluster = c.Name
				return m.openTasks("")
			case key.Matches(msg, m.keys.Console):
				return m.openConsole(c.Name, func() (string, error) { return m.svc.OpenClusterConsole(c) })
			}
		}
	case levelServices:
		if s, ok := m.services.Selected(); ok {
			switch {
			case key.Matches(msg, m.keys.Favorite):
				cmd := m.deps.ToggleFavorite(serviceBookmark(s))
				m.services.Refresh()
				return cmd
			case key.Matches(msg, m.keys.OpenTasks):
				m.deps.Visit(serviceBookmark(s))
				return m.openTasks(s.Name)
			case key.Matches(msg, m.keys.Deploy):
				return m.confirmDeploy(s)
			case key.Matches(msg, m.keys.Watch):
				return m.openWatch(s)
			case key.Matches(msg, m.keys.Scale):
				return m.openScale(s)
			case key.Matches(msg, m.keys.Details):
				m.deps.Visit(serviceBookmark(s))
				m.svcDetail = s
				m.view = viewServiceDetails
				m.svcSeq++
				svc := m.svc
				return tea.Batch(m.details.Open(m.serviceDetailsHeader(), func(ctx context.Context, w domain.MetricWindow) ([]domain.Series, error) {
					return svc.ServiceMetrics(ctx, s, w)
				}, ""), m.readService())
			case key.Matches(msg, m.keys.Console):
				return m.openConsole(s.Name, func() (string, error) { return m.svc.OpenServiceConsole(s) })
			}
		}
	case levelTasks:
		if t, ok := m.tasks.Selected(); ok {
			if key.Matches(msg, m.keys.Actions) {
				m.task = t
				m.menu.SetItems(m.taskMenuItems())
				m.view = viewActions
				return nil
			}
			if a, ok := m.taskActionForKey(msg); ok {
				return m.runTaskAction(a, t)
			}
		}
	}
	return l.Update(msg)
}

func (m *Model) openServices(cluster string) tea.Cmd {
	m.cluster = cluster
	m.services.Reset()
	m.services.SetTitle(cluster + " › Services")
	m.level = levelServices
	return m.loadServices()
}

// openTasks lists the tasks of service, or of the whole cluster when empty.
func (m *Model) openTasks(service string) tea.Cmd {
	m.service = service
	m.tasksParent = m.level
	m.tasks.Reset()
	title := m.cluster + " › All tasks"
	if service != "" {
		title = m.cluster + " › " + service + " › Tasks"
	}
	m.tasks.SetTitle(title)
	m.level = levelTasks
	return m.loadTasks()
}

// up goes back one level, keeping the parent list as it was.
func (m *Model) up() {
	switch m.level {
	case levelTasks:
		m.level = m.tasksParent
	case levelServices:
		m.level = levelClusters
	}
}

func (m *Model) openConsole(name string, open func() (string, error)) tea.Cmd {
	return func() tea.Msg {
		url, err := open()
		if err != nil {
			return section.StatusMsg{Text: fmt.Sprintf("%v (%s)", err, url), Error: true}
		}
		return section.StatusMsg{Text: "Opened console for " + name}
	}
}

func (m *Model) Help() []key.Binding {
	k := m.keys
	switch m.view {
	case viewActions, viewPicker:
		return append(m.menu.Help(), k.Back)
	case viewTaskDetails:
		return append(m.details.Help(), k.Connect, k.PortForward, k.Console, k.Back)
	case viewServiceDetails:
		return append(m.details.Help(), k.Watch, k.Deploy, k.Scale, k.Back)
	case viewWatch:
		return m.watchHelp()
	case viewScale:
		return []key.Binding{k.Start, k.Back}
	case viewPortForward:
		return []key.Binding{m.form.NextField(), k.Start, k.Back}
	}
	l := m.current()
	if l.Filtering() {
		return l.Help()
	}
	var b []key.Binding
	switch m.level {
	case levelClusters:
		b = []key.Binding{k.OpenServices, k.AllTasks, k.Console, k.Favorite, k.Refresh}
	case levelServices:
		b = []key.Binding{k.OpenTasks, k.AllTasks, k.Details, k.Watch, k.Deploy, k.Scale, k.Console, k.Favorite, k.Refresh, k.Back}
	case levelTasks:
		b = []key.Binding{k.Actions, k.Connect, k.PortForward, k.Console, k.Details, k.Refresh, k.Back}
	}
	return append(b, l.Help()...)
}
