package containers

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"infra/internal/domain"
	"infra/internal/tui/section"
)

// confirmDeploy asks before replacing every task of s.
func (m *Model) confirmDeploy(s domain.Service) tea.Cmd {
	if cmd := m.deps.BlockWrite("deploy " + s.Name); cmd != nil {
		return cmd
	}
	svc := m.svc
	body := fmt.Sprintf("Force a new deployment of %s in %s? Every task is replaced by a new one "+
		"(rolling update with the service's deployment settings), pulling the image again.", s.Name, s.Cluster)
	return section.Confirm("Force new deployment", body, func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), loadTimeout)
		defer cancel()
		if err := svc.ForceDeployment(ctx, s); err != nil {
			return changedMsg{err: err}
		}
		return changedMsg{text: "Deployment of " + s.Name + " started", deployed: &s}
	})
}

// openScale asks for the new desired count of s.
func (m *Model) openScale(s domain.Service) tea.Cmd {
	if cmd := m.deps.BlockWrite("scale " + s.Name); cmd != nil {
		return cmd
	}
	m.svcDetail = s
	m.details.Close()
	m.view = viewScale
	m.scale.SetValue(strconv.Itoa(s.Desired))
	m.scale.CursorEnd()
	return m.scale.Focus()
}

func (m *Model) submitScale() tea.Cmd {
	s := m.svcDetail
	count, err := strconv.Atoi(strings.TrimSpace(m.scale.Value()))
	if err != nil || count < 0 {
		return section.Error(fmt.Errorf("the desired count must be a number ≥ 0"))
	}
	m.view = viewList
	if count == s.Desired {
		return section.Status("Desired count unchanged")
	}
	svc := m.svc
	body := fmt.Sprintf("Change the desired count of %s from %d to %d tasks?", s.Name, s.Desired, count)
	if count == 0 {
		body += " The service stops serving traffic."
	}
	return section.Confirm("Scale service", body, func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), loadTimeout)
		defer cancel()
		if err := svc.SetDesiredCount(ctx, s, count); err != nil {
			return changedMsg{err: err}
		}
		return changedMsg{text: fmt.Sprintf("%s scaled to %d tasks", s.Name, count)}
	})
}

func (m *Model) viewScale() string {
	s := m.svcDetail
	return m.theme.Title.Render("Scale "+s.Name) + " " +
		m.theme.Muted.Render(fmt.Sprintf("%d running, %d desired", s.Running, s.Desired)) + "\n\n" +
		m.scale.View() + "\n\n" + m.theme.Muted.Render("enter to review the change, esc to cancel")
}
