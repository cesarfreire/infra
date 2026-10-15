// Package loadbalancers is the menu section for load balancers (ELB v2):
// drill down from load balancers to target groups to targets and their
// health, with listeners and charts in the details.
package loadbalancers

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"infra/internal/domain"
	"infra/internal/filter"
	"infra/internal/tui/components/detailpanel"
	"infra/internal/tui/components/details"
	"infra/internal/tui/components/listview"
	"infra/internal/tui/section"
	"infra/internal/tui/theme"
)

// Title is the menu label of this section.
const Title = "Load balancers"

const loadTimeout = 30 * time.Second

// Service is the subset of use cases this section needs. It is satisfied by
// *app.LoadBalancerService and by fakes in tests.
type Service interface {
	List(ctx context.Context) ([]domain.LoadBalancer, error)
	Listeners(ctx context.Context, lb domain.LoadBalancer) ([]domain.Listener, error)
	TargetGroups(ctx context.Context, lb domain.LoadBalancer) ([]domain.TargetGroup, error)
	Metrics(ctx context.Context, lb domain.LoadBalancer, w domain.MetricWindow) ([]domain.Series, error)
	OpenConsole(lb domain.LoadBalancer) (string, error)
}

type level int

const (
	levelBalancers level = iota
	levelGroups
	levelTargets
)

type lbsLoadedMsg struct {
	items []domain.LoadBalancer
	err   error
}

type groupsLoadedMsg struct {
	lb    string
	items []domain.TargetGroup
	err   error
}

type listenersLoadedMsg struct {
	lb    string
	items []domain.Listener
	err   error
}

type keyMap struct {
	Open     key.Binding
	Details  key.Binding
	Console  key.Binding
	Favorite key.Binding
	Refresh  key.Binding
	Back     key.Binding
}

// Model implements section.Section.
type Model struct {
	svc   Service
	deps  section.Deps
	theme theme.Theme
	keys  keyMap

	balancers *listview.Model[domain.LoadBalancer]
	groups    *listview.Model[domain.TargetGroup]
	targets   *listview.Model[domain.Target]
	details   *detailpanel.Model

	level       level
	showDetails bool
	lb          domain.LoadBalancer // selected load balancer
	listeners   []domain.Listener
	listenerErr error
	group       domain.TargetGroup
}

// New creates the Load balancers section.
func New(svc Service, deps section.Deps) *Model {
	th := deps.Theme
	m := &Model{
		svc:       svc,
		deps:      deps,
		theme:     th,
		balancers: listview.New("Load balancers", "load balancers", lbColumns, lbFields, "name, type:application, scheme:internal", th),
		groups:    listview.New("Target groups", "target groups", groupColumns, groupFields, "name, protocol:https", th),
		targets:   listview.New("Targets", "targets", targetColumns, targetFields, "id, state:unhealthy, zone:1a", th),
		details:   detailpanel.New(th).WithPrefs(deps.Prefs),
		keys: keyMap{
			Open:     key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "open")),
			Details:  key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "details")),
			Console:  key.NewBinding(key.WithKeys("o"), key.WithHelp("o", "console")),
			Favorite: key.NewBinding(key.WithKeys("*"), key.WithHelp("*", "favorite")),
			Refresh:  key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "refresh")),
			Back:     key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
		},
	}
	m.balancers.SetMarker(func(lb domain.LoadBalancer) string { return deps.Star(bookmark(lb)) })
	return m
}

func bookmark(lb domain.LoadBalancer) domain.Bookmark {
	return domain.Bookmark{Kind: domain.KindLoadBalancer, ID: lb.Name, Label: lb.Name}
}

func (m *Model) Title() string { return Title }
func (m *Model) Init() tea.Cmd { return m.loadBalancers() }

func (m *Model) lists() []interface {
	Focus()
	Blur()
	SetSize(int, int)
} {
	return []interface {
		Focus()
		Blur()
		SetSize(int, int)
	}{m.balancers, m.groups, m.targets}
}

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
	m.details.SetSize(width, height)
}

// InputMode is normal only on the load balancers list without a filter.
func (m *Model) InputMode() section.InputMode {
	if !m.showDetails && m.level == levelBalancers && !m.balancers.Capturing() {
		return section.ModeNormal
	}
	return section.ModeCapture
}

func (m *Model) loadBalancers() tea.Cmd {
	svc := m.svc
	return tea.Batch(m.balancers.StartLoading(), func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), loadTimeout)
		defer cancel()
		items, err := svc.List(ctx)
		return lbsLoadedMsg{items: items, err: err}
	})
}

func (m *Model) loadGroups() tea.Cmd {
	svc, lb := m.svc, m.lb
	return tea.Batch(m.groups.StartLoading(), func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), loadTimeout)
		defer cancel()
		items, err := svc.TargetGroups(ctx, lb)
		return groupsLoadedMsg{lb: lb.ARN, items: items, err: err}
	})
}

func (m *Model) Update(msg tea.Msg) (section.Section, tea.Cmd) {
	switch msg := msg.(type) {
	case lbsLoadedMsg:
		if msg.err != nil {
			m.balancers.SetError(msg.err)
			return m, section.Error(msg.err)
		}
		m.balancers.SetItems(msg.items)
		return m, section.Status(fmt.Sprintf("Loaded %d load balancers", len(msg.items)))
	case groupsLoadedMsg:
		if msg.lb != m.lb.ARN {
			return m, nil
		}
		if msg.err != nil {
			m.groups.SetError(msg.err)
			return m, section.Error(msg.err)
		}
		m.groups.SetItems(msg.items)
		return m, section.Status(fmt.Sprintf("Loaded %d target groups", len(msg.items)))
	case listenersLoadedMsg:
		if msg.lb != m.lb.ARN || !m.showDetails {
			return m, nil
		}
		m.listeners, m.listenerErr = msg.items, msg.err
		m.details.SetHeader(m.detailsHeader())
		return m, nil
	case section.RevealMsg:
		if msg.Bookmark.Kind == domain.KindLoadBalancer {
			m.closeDetails()
			m.level = levelBalancers
			m.balancers.Reveal(func(lb domain.LoadBalancer) bool { return lb.Name == msg.Bookmark.ID })
		}
		return m, nil
	case tea.KeyPressMsg:
		return m, m.handleKey(msg)
	}
	return m, tea.Batch(m.balancers.Update(msg), m.groups.Update(msg), m.targets.Update(msg), m.details.Update(msg))
}

func (m *Model) closeDetails() {
	m.details.Close()
	m.showDetails = false
}

func (m *Model) handleKey(msg tea.KeyPressMsg) tea.Cmd {
	if m.showDetails {
		if cmd, ok := m.details.HandleKey(msg); ok {
			return cmd
		}
		switch {
		case key.Matches(msg, m.keys.Back):
			m.closeDetails()
		case key.Matches(msg, m.keys.Console):
			return m.openConsole(m.lb)
		}
		return nil
	}

	switch m.level {
	case levelGroups:
		return m.handleGroupsKey(msg)
	case levelTargets:
		if m.targets.Filtering() {
			return m.targets.Update(msg)
		}
		if key.Matches(msg, m.keys.Back) {
			if !m.targets.Query().Empty() {
				return m.targets.Update(msg)
			}
			m.level = levelGroups
			return nil
		}
		if key.Matches(msg, m.keys.Refresh) {
			m.level = levelGroups
			return m.loadGroups()
		}
		return m.targets.Update(msg)
	}

	if m.balancers.Filtering() {
		return m.balancers.Update(msg)
	}
	if key.Matches(msg, m.keys.Refresh) {
		return m.loadBalancers()
	}
	if lb, ok := m.balancers.Selected(); ok {
		switch {
		case key.Matches(msg, m.keys.Favorite):
			cmd := m.deps.ToggleFavorite(bookmark(lb))
			m.balancers.Refresh()
			return cmd
		case key.Matches(msg, m.keys.Open):
			m.deps.Visit(bookmark(lb))
			m.lb = lb
			m.level = levelGroups
			m.groups.Reset()
			m.groups.SetTitle(lb.Name + " › Target groups")
			return m.loadGroups()
		case key.Matches(msg, m.keys.Details):
			return m.openDetails(lb)
		case key.Matches(msg, m.keys.Console):
			return m.openConsole(lb)
		}
	}
	return m.balancers.Update(msg)
}

func (m *Model) handleGroupsKey(msg tea.KeyPressMsg) tea.Cmd {
	if m.groups.Filtering() {
		return m.groups.Update(msg)
	}
	switch {
	case key.Matches(msg, m.keys.Back):
		if !m.groups.Query().Empty() {
			return m.groups.Update(msg)
		}
		m.level = levelBalancers
		return nil
	case key.Matches(msg, m.keys.Refresh):
		return m.loadGroups()
	case key.Matches(msg, m.keys.Details):
		return m.openDetails(m.lb)
	case key.Matches(msg, m.keys.Open):
		if g, ok := m.groups.Selected(); ok {
			m.group = g
			m.level = levelTargets
			m.targets.Reset()
			m.targets.SetTitle(m.lb.Name + " › " + g.Name + " › Targets")
			m.targets.SetItems(g.Targets)
		}
		return nil
	}
	return m.groups.Update(msg)
}

func (m *Model) openDetails(lb domain.LoadBalancer) tea.Cmd {
	m.deps.Visit(bookmark(lb))
	m.lb = lb
	m.showDetails = true
	m.listeners, m.listenerErr = nil, nil
	svc := m.svc
	hint := ""
	if lb.Type == "gateway" {
		hint = "Gateway load balancers have no traffic charts here."
	}
	return tea.Batch(
		m.details.Open(m.detailsHeader(), func(ctx context.Context, w domain.MetricWindow) ([]domain.Series, error) {
			return svc.Metrics(ctx, lb, w)
		}, hint),
		func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), loadTimeout)
			defer cancel()
			items, err := svc.Listeners(ctx, lb)
			return listenersLoadedMsg{lb: lb.ARN, items: items, err: err}
		},
	)
}

func (m *Model) openConsole(lb domain.LoadBalancer) tea.Cmd {
	svc := m.svc
	return func() tea.Msg {
		url, err := svc.OpenConsole(lb)
		if err != nil {
			return section.StatusMsg{Text: fmt.Sprintf("%v (%s)", err, url), Error: true}
		}
		return section.StatusMsg{Text: "Opened console for " + lb.Name}
	}
}

func (m *Model) Help() []key.Binding {
	k := m.keys
	if m.showDetails {
		return append(m.details.Help(), k.Console, k.Back)
	}
	switch m.level {
	case levelGroups:
		if m.groups.Filtering() {
			return m.groups.Help()
		}
		return append([]key.Binding{key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "targets")), k.Details, k.Refresh, k.Back}, m.groups.Help()...)
	case levelTargets:
		if m.targets.Filtering() {
			return m.targets.Help()
		}
		return append([]key.Binding{k.Refresh, k.Back}, m.targets.Help()...)
	}
	if m.balancers.Filtering() {
		return m.balancers.Help()
	}
	return append([]key.Binding{key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "target groups")),
		k.Details, k.Console, k.Favorite, k.Refresh}, m.balancers.Help()...)
}

func (m *Model) View() string {
	if m.showDetails {
		return m.details.View()
	}
	switch m.level {
	case levelGroups:
		return m.groups.View()
	case levelTargets:
		return m.targets.View()
	}
	return m.balancers.View()
}

func (m *Model) detailsHeader() string {
	lb := m.lb
	head := details.Render(m.theme, lb.Name, lb.Type+" load balancer", []details.Field{
		{Label: "State", Value: lb.State},
		{Label: "Scheme", Value: lb.Scheme},
		{Label: "DNS name", Value: lb.DNSName},
		{Label: "VPC", Value: lb.VPC},
		{Label: "Zones", Value: strings.Join(lb.Zones, ", ")},
		{Label: "Created", Value: lb.CreatedAt.Local().Format(time.DateTime)},
		{Label: "ARN", Value: lb.ARN},
	})
	lines := []string{m.theme.Title.Render("Listeners")}
	switch {
	case m.listenerErr != nil:
		lines = append(lines, m.theme.StatusError.Render(m.listenerErr.Error()))
	case m.listeners == nil:
		lines = append(lines, m.theme.Muted.Render("Loading…"))
	case len(m.listeners) == 0:
		lines = append(lines, m.theme.Muted.Render("No listeners."))
	}
	for _, l := range m.listeners {
		lines = append(lines, fmt.Sprintf("%s %s", m.theme.Label.Render(fmt.Sprintf("%-6s %-5d", l.Protocol, l.Port)), l.Action))
	}
	return head + "\n" + strings.Join(lines, "\n") + "\n"
}

var lbColumns = []listview.Column[domain.LoadBalancer]{
	{Title: "Load balancer", Value: func(lb domain.LoadBalancer) string { return lb.Name }},
	{Title: "Type", Width: 11, Value: func(lb domain.LoadBalancer) string { return lb.Type }},
	{Title: "Scheme", Width: 15, Value: func(lb domain.LoadBalancer) string { return lb.Scheme }},
	{Title: "State", Width: 12, Value: func(lb domain.LoadBalancer) string { return lb.State }},
	{Title: "DNS name", Width: 50, Value: func(lb domain.LoadBalancer) string { return lb.DNSName }},
}

func lbFields(lb domain.LoadBalancer) []filter.Field {
	return []filter.Field{
		{Name: "name", Value: lb.Name},
		{Name: "type", Value: lb.Type},
		{Name: "scheme", Value: lb.Scheme},
		{Name: "state", Value: lb.State},
		{Name: "dns", Value: lb.DNSName},
	}
}

var groupColumns = []listview.Column[domain.TargetGroup]{
	{Title: "Target group", Value: func(g domain.TargetGroup) string { return g.Name }},
	{Title: "Protocol", Width: 12, Value: func(g domain.TargetGroup) string { return fmt.Sprintf("%s:%d", g.Protocol, g.Port) }},
	{Title: "Healthy", Width: 9, Value: func(g domain.TargetGroup) string { return fmt.Sprintf("%d/%d", g.Healthy(), len(g.Targets)) }},
	{Title: "Type", Width: 9, Value: func(g domain.TargetGroup) string { return g.TargetType }},
	{Title: "Health check", Width: 24, Value: func(g domain.TargetGroup) string { return g.HealthCheck }},
}

func groupFields(g domain.TargetGroup) []filter.Field {
	return []filter.Field{{Name: "name", Value: g.Name}, {Name: "protocol", Value: g.Protocol}, {Name: "type", Value: g.TargetType}}
}

var targetColumns = []listview.Column[domain.Target]{
	{Title: "Target", Value: func(t domain.Target) string { return t.ID }},
	{Title: "Port", Width: 6, Value: func(t domain.Target) string { return strconv.Itoa(t.Port) }},
	{Title: "Zone", Width: 12, Value: func(t domain.Target) string { return t.Zone }},
	{Title: "State", Width: 11, Value: func(t domain.Target) string { return t.State }},
	{Title: "Reason", Width: 32, Value: func(t domain.Target) string { return strings.TrimPrefix(t.Reason, "Target.") }},
}

func targetFields(t domain.Target) []filter.Field {
	return []filter.Field{
		{Name: "id", Value: t.ID},
		{Name: "state", Value: t.State},
		{Name: "zone", Value: t.Zone},
		{Name: "reason", Value: t.Reason + " " + t.Description},
	}
}
