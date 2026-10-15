package containers

import (
	"fmt"
	"strings"
	"time"

	"charm.land/lipgloss/v2"

	"infra/internal/domain"
	"infra/internal/tui/theme"

	"infra/internal/tui/components/details"
)

func (m *Model) View() string {
	switch m.view {
	case viewActions:
		return m.menu.View(m.theme, taskLabel(m.task), m.task.ID+" · "+m.task.LastStatus)
	case viewPicker:
		return m.picker.View(m.theme, "Choose a container", taskLabel(m.task))
	case viewTaskDetails, viewServiceDetails:
		return m.details.View()
	case viewScale:
		return m.viewScale()
	case viewPortForward:
		return m.form.View(m.theme, "Port forward", taskLabel(m.task)+" · "+m.container.Name)
	}
	return m.current().View()
}

func (m *Model) serviceDetailsHeader() string {
	s := m.svcDetail
	head := details.Render(m.theme, s.Name, "service details", []details.Field{
		{Label: "Cluster", Value: s.Cluster},
		{Label: "Status", Value: s.Status},
		{Label: "Tasks", Value: fmt.Sprintf("%d running, %d pending, %d desired", s.Running, s.Pending, s.Desired)},
		{Label: "Task definition", Value: s.TaskDefinition},
		{Label: "Launch type", Value: s.LaunchType},
		{Label: "ECS Exec", Value: yesNo(s.ExecEnabled)},
		{Label: "ARN", Value: s.ARN},
	})
	return head + "\n" + m.deploymentsView(s) + "\n" + m.eventsView(s)
}

// deploymentsView shows each deployment with its rollout progress.
func (m *Model) deploymentsView(s domain.Service) string {
	th := m.theme
	title := th.Title.Render("Deployments")
	if s.RollingOut() {
		title += " " + th.Warning.Render("rolling out · refreshing every 10s")
	}
	lines := []string{title}
	if len(s.Deployments) == 0 {
		return title + "\n" + th.Muted.Render("No deployment information yet.") + "\n"
	}
	for _, d := range s.Deployments {
		state := d.RolloutState
		style := th.Muted
		switch state {
		case "IN_PROGRESS":
			style = th.Warning
		case "FAILED":
			style = th.StatusError
		case "COMPLETED":
			style = th.StatusInfo
		}
		line := fmt.Sprintf("%-8s %-24s %s %s %d/%d running", d.Status, d.TaskDefinition,
			style.Render(fmt.Sprintf("%-11s", state)), progressBar(th, d.Running, d.Desired, 12), d.Running, d.Desired)
		if d.Pending > 0 {
			line += fmt.Sprintf(" · %d pending", d.Pending)
		}
		if d.Failed > 0 {
			line += th.StatusError.Render(fmt.Sprintf(" · %d failed", d.Failed))
		}
		if !d.CreatedAt.IsZero() {
			line += th.Muted.Render(" · started " + d.CreatedAt.Local().Format("01/02 15:04"))
		}
		lines = append(lines, line)
		if d.RolloutReason != "" && state != "COMPLETED" {
			lines = append(lines, th.Muted.Render("         "+d.RolloutReason))
		}
	}
	return strings.Join(lines, "\n") + "\n"
}

// eventsView shows the latest service events, wrapped to the panel.
func (m *Model) eventsView(s domain.Service) string {
	th := m.theme
	lines := []string{th.Title.Render("Events") + " " + th.Muted.Render("newest first")}
	if len(s.Events) == 0 {
		return lines[0] + "\n" + th.Muted.Render("No events.") + "\n"
	}
	wrap := lipgloss.NewStyle().Width(max(20, m.width-16))
	for _, e := range s.Events {
		msg := strings.Split(wrap.Render(e.Message), "\n")
		lines = append(lines, th.Muted.Render(e.Time.Local().Format("01/02 15:04:05"))+"  "+msg[0])
		for _, more := range msg[1:] {
			lines = append(lines, strings.Repeat(" ", 16)+more)
		}
	}
	return strings.Join(lines, "\n") + "\n"
}

func progressBar(th theme.Theme, done, total, width int) string {
	filled := 0
	if total > 0 {
		filled = min(width, done*width/total)
	}
	return th.Selected.Render(strings.Repeat("█", filled)) + th.Muted.Render(strings.Repeat("░", width-filled))
}

func (m *Model) taskDetailsHeader() string {
	t := m.task
	started := ""
	if !t.StartedAt.IsZero() {
		started = t.StartedAt.Local().Format(time.DateTime)
	}
	var containers []details.Field
	for _, c := range t.Containers {
		parts := []string{c.Status}
		if c.Health != "" && c.Health != "UNKNOWN" {
			parts = append(parts, c.Health)
		}
		if c.ExecAgent != "" {
			parts = append(parts, "exec agent "+c.ExecAgent)
		}
		parts = append(parts, c.Image)
		containers = append(containers, details.Field{Label: c.Name, Value: strings.Join(parts, " · ")})
	}
	return details.Render(m.theme, taskLabel(t), "task details", []details.Field{
		{Label: "ID", Value: t.ID},
		{Label: "Cluster", Value: t.Cluster},
		{Label: "Service", Value: t.Service},
		{Label: "Status", Value: t.LastStatus + " (desired " + t.DesiredStatus + ")"},
		{Label: "Health", Value: t.Health},
		{Label: "Task definition", Value: t.TaskDefinition},
		{Label: "Launch type", Value: t.LaunchType},
		{Label: "CPU / memory", Value: strings.Trim(t.CPU+" / "+t.Memory, " /")},
		{Label: "AZ", Value: t.AvailabilityZone},
		{Label: "Private IP", Value: t.PrivateIP},
		{Label: "Started", Value: started},
		{Label: "ECS Exec", Value: yesNo(t.ExecEnabled)},
	}, details.Section{Title: "Containers", Fields: containers})
}
