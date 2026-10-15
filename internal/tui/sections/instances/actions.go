package instances

import (
	"context"
	"fmt"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"infra/internal/domain"
	"infra/internal/session"
	"infra/internal/tui/components/actionmenu"
	"infra/internal/tui/section"
)

// action is an operation available for a single instance. The same list
// drives the actions menu, the hotkeys in the list and mouse clicks.
type action struct {
	label string
	key   key.Binding
	run   func(m *Model, inst domain.Instance) tea.Cmd
}

func (m *Model) actions() []action {
	return []action{
		{label: "Connect (SSM shell)", key: m.keys.Connect, run: (*Model).connect},
		{label: "Port forward (SSM)", key: m.keys.PortForward, run: (*Model).openPortForward},
		{label: "Open in AWS console", key: m.keys.Console, run: (*Model).openConsole},
		{label: "Details and monitoring", key: m.keys.Details, run: (*Model).openDetails},
		{label: "Start instance", key: m.keys.PowerOn, run: power("start", domain.InstanceStateStopped, Service.Start, "")},
		{label: "Stop instance", key: m.keys.PowerOff, run: power("stop", domain.InstanceStateRunning, Service.Stop,
			" Data on instance store volumes is lost, and the public IP changes unless it is an Elastic IP.")},
		{label: "Reboot instance", key: m.keys.Reboot, run: power("reboot", domain.InstanceStateRunning, Service.Reboot, "")},
	}
}

// power returns an action that changes the power state after confirmation.
// The state check here only fails fast; the use case enforces it.
func power(verb string, want domain.InstanceState, do func(Service, context.Context, domain.Instance) error, warning string) func(*Model, domain.Instance) tea.Cmd {
	return func(m *Model, inst domain.Instance) tea.Cmd {
		name := inst.DisplayName()
		if cmd := m.deps.BlockWrite(verb + " " + name); cmd != nil {
			return cmd
		}
		if inst.State != want {
			return section.Error(fmt.Errorf("cannot %s %s: it is %s", verb, name, inst.State))
		}
		svc := m.svc
		title := strings.ToUpper(verb[:1]) + verb[1:] + " instance"
		body := fmt.Sprintf("%s %s (%s)?%s", strings.ToUpper(verb[:1])+verb[1:], name, inst.ID, warning)
		return section.Confirm(title, body, func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), loadTimeout)
			defer cancel()
			if err := do(svc, ctx, inst); err != nil {
				return changedMsg{err: err}
			}
			return changedMsg{text: fmt.Sprintf("Requested %s of %s", verb, name)}
		})
	}
}

func (m *Model) menuItems() []actionmenu.Item {
	var items []actionmenu.Item
	for _, a := range m.actions() {
		items = append(items, actionmenu.Item{Label: a.label, Key: a.key})
	}
	return items
}

// actionForKey returns the action bound to msg, if any.
func (m *Model) actionForKey(msg tea.KeyPressMsg) (action, bool) {
	for _, a := range m.actions() {
		if key.Matches(msg, a.key) {
			return a, true
		}
	}
	return action{}, false
}

// runAction leaves nested views before running, so actions that open
// another view (details, form) override it and the rest return to the list.
func (m *Model) runAction(a action, inst domain.Instance) tea.Cmd {
	m.view = viewList
	return a.run(m, inst)
}

// connect opens a shell session and shows it in the Sessions section.
func (m *Model) connect(inst domain.Instance) tea.Cmd {
	cmd, err := m.svc.Connect(inst)
	if err != nil {
		return section.Error(err)
	}
	s, err := m.sessions.Start(session.KindShell, inst.DisplayName()+" · shell", cmd)
	if err != nil {
		return section.Error(fmt.Errorf("starting session: %w", err))
	}
	return func() tea.Msg { return section.OpenSessionMsg{ID: s.ID} }
}

func (m *Model) openConsole(inst domain.Instance) tea.Cmd {
	return func() tea.Msg {
		url, err := m.svc.OpenConsole(inst)
		if err != nil {
			return section.StatusMsg{Text: fmt.Sprintf("%v (%s)", err, url), Error: true}
		}
		return section.StatusMsg{Text: "Opened console for " + inst.DisplayName()}
	}
}

func (m *Model) openDetails(inst domain.Instance) tea.Cmd {
	m.deps.Visit(bookmark(inst))
	m.selected = inst
	m.view = viewDetails
	svc := m.svc
	return m.details.Open(m.detailsHeader(inst), func(ctx context.Context, w domain.MetricWindow) ([]domain.Series, error) {
		return svc.Metrics(ctx, inst, w)
	}, "")
}

func (m *Model) openPortForward(inst domain.Instance) tea.Cmd {
	if !inst.IsRunning() {
		return section.Error(fmt.Errorf("port forward to %s: %w", inst.DisplayName(), domain.ErrInstanceNotRunning))
	}
	m.selected = inst
	m.view = viewPortForward
	return m.form.Reset()
}

func (m *Model) submitPortForward() tea.Cmd {
	local, remote, err := m.form.Ports()
	if err != nil {
		m.form.SetError(err)
		return nil
	}
	cmd, err := m.svc.PortForward(m.selected, local, remote)
	if err != nil {
		m.form.SetError(err)
		return nil
	}
	title := fmt.Sprintf("%s · localhost:%d → %d", m.selected.DisplayName(), local, remote)
	if _, err := m.sessions.Start(session.KindPortForward, title, cmd); err != nil {
		m.form.SetError(fmt.Errorf("starting session: %w", err))
		return nil
	}
	// Tunnels run in the background; their output is in the Sessions section.
	m.view = viewList
	return section.Status(fmt.Sprintf("Port forward started on localhost:%d (see Sessions)", local))
}
