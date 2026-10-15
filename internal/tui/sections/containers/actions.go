package containers

import (
	"context"
	"fmt"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"infra/internal/domain"
	"infra/internal/session"
	"infra/internal/tui/components/actionmenu"
	"infra/internal/tui/section"
)

// taskAction is an operation on a task. The same list drives the actions
// menu, the hotkeys in the tasks list and mouse clicks.
type taskAction struct {
	label string
	key   key.Binding
	run   func(m *Model, t domain.Task) tea.Cmd
}

func (m *Model) taskActions() []taskAction {
	return []taskAction{
		{label: "Connect (ECS Exec shell)", key: m.keys.Connect, run: func(m *Model, t domain.Task) tea.Cmd {
			return m.withContainer(t, pendingConnect)
		}},
		{label: "Port forward (SSM)", key: m.keys.PortForward, run: func(m *Model, t domain.Task) tea.Cmd {
			return m.withContainer(t, pendingPortForward)
		}},
		{label: "Open in AWS console", key: m.keys.Console, run: func(m *Model, t domain.Task) tea.Cmd {
			return m.openConsole(taskLabel(t), func() (string, error) { return m.svc.OpenTaskConsole(t) })
		}},
		{label: "Details and monitoring", key: m.keys.Details, run: func(m *Model, t domain.Task) tea.Cmd {
			m.task = t
			m.view = viewTaskDetails
			svc := m.svc
			return m.details.Open(m.taskDetailsHeader(), func(ctx context.Context, w domain.MetricWindow) ([]domain.Series, error) {
				return svc.TaskMetrics(ctx, t, w)
			}, "Task metrics need Container Insights with enhanced observability enabled on the cluster. "+
				"Service-level CPU and memory are in the service details.")
		}},
	}
}

func (m *Model) taskMenuItems() []actionmenu.Item {
	var items []actionmenu.Item
	for _, a := range m.taskActions() {
		items = append(items, actionmenu.Item{Label: a.label, Key: a.key})
	}
	return items
}

func (m *Model) taskActionForKey(msg tea.KeyPressMsg) (taskAction, bool) {
	for _, a := range m.taskActions() {
		if key.Matches(msg, a.key) {
			return a, true
		}
	}
	return taskAction{}, false
}

// runTaskAction leaves nested views before running, so actions that open
// another view override it and the rest return to the list.
func (m *Model) runTaskAction(a taskAction, t domain.Task) tea.Cmd {
	m.details.Close()
	m.view = viewList
	return a.run(m, t)
}

// withContainer runs p on the task's only running container, or asks the
// user to choose one first.
func (m *Model) withContainer(t domain.Task, p pendingAction) tea.Cmd {
	m.task = t
	m.pending = p
	running := t.RunningContainers()
	switch len(running) {
	case 0:
		return section.Error(fmt.Errorf("task %s: %w", t.ID, domain.ErrContainerNotRunning))
	case 1:
		return m.useContainer(running[0])
	}
	m.choices = running
	items := make([]actionmenu.Item, len(running))
	for i, c := range running {
		items[i] = actionmenu.Item{Label: c.Name + "  " + m.theme.Muted.Render(c.Image)}
	}
	m.picker.SetItems(items)
	m.view = viewPicker
	return nil
}

func (m *Model) useContainer(c domain.Container) tea.Cmd {
	m.container = c
	m.view = viewList
	if m.pending == pendingConnect {
		return m.connect(m.task, c)
	}
	if err := m.task.CanExec(c); err != nil {
		return section.Error(err)
	}
	m.view = viewPortForward
	return m.form.Reset()
}

// connect opens an ECS Exec shell and shows it in the Sessions section.
func (m *Model) connect(t domain.Task, c domain.Container) tea.Cmd {
	cmd, err := m.svc.Connect(t, c)
	if err != nil {
		return section.Error(err)
	}
	s, err := m.sessions.Start(session.KindShell, fmt.Sprintf("%s · %s · shell", taskLabel(t), c.Name), cmd)
	if err != nil {
		return section.Error(fmt.Errorf("starting session: %w", err))
	}
	return func() tea.Msg { return section.OpenSessionMsg{ID: s.ID} }
}

func (m *Model) submitPortForward() tea.Cmd {
	local, remote, err := m.form.Ports()
	if err != nil {
		m.form.SetError(err)
		return nil
	}
	cmd, err := m.svc.PortForward(m.task, m.container, local, remote)
	if err != nil {
		m.form.SetError(err)
		return nil
	}
	title := fmt.Sprintf("%s · %s · localhost:%d → %d", taskLabel(m.task), m.container.Name, local, remote)
	if _, err := m.sessions.Start(session.KindPortForward, title, cmd); err != nil {
		m.form.SetError(fmt.Errorf("starting session: %w", err))
		return nil
	}
	m.view = viewList
	return section.Status(fmt.Sprintf("Port forward started on localhost:%d (see Sessions)", local))
}

// taskLabel is a short human name for a task: its service (or task
// definition) and the start of its ID.
func taskLabel(t domain.Task) string {
	name := t.Service
	if name == "" {
		name = t.TaskDefinition
	}
	return name + " " + t.ID[:min(8, len(t.ID))]
}
