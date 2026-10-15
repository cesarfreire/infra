package caches

import (
	"strings"

	"infra/internal/tui/components/details"
)

func (m *Model) View() string {
	switch m.view {
	case viewActions:
		return m.menu.View(m.theme, m.cache.ID, engineLabel(m.cache)+" · "+m.cache.Status)
	case viewDetails:
		return m.details.View()
	case viewHostPicker:
		return m.viewHostPicker()
	case viewPortForward:
		return m.form.View(m.theme, "Port forward", m.cache.ID+" via "+m.host.DisplayName())
	}
	return m.list.View()
}

func (m *Model) viewHostPicker() string {
	switch {
	case m.hostsLoading:
		return m.theme.Title.Render("Jump host for "+m.cache.ID) + "\n\n" + m.spinner.View() + " Loading running instances…"
	case m.hostsErr != nil:
		return m.theme.Title.Render("Jump host for "+m.cache.ID) + "\n\n" + m.theme.StatusError.Render(m.hostsErr.Error())
	case m.picker == nil:
		return ""
	}
	note := m.theme.Muted.Render("The instance needs the SSM agent and network access to the cache (VPC and security group).")
	return m.picker.View(m.theme, 80) + "\n\n" + note
}

func (m *Model) detailsHeader() string {
	c := m.cache
	mode := "disabled"
	if c.ClusterMode {
		mode = "enabled"
	}
	return details.Render(m.theme, c.ID, "cache details", []details.Field{
		{Label: "Engine", Value: engineLabel(c)},
		{Label: "Status", Value: c.Status},
		{Label: "Node type", Value: nodeLabel(c)},
		{Label: "Cluster mode", Value: mode},
		{Label: "Endpoint", Value: endpointLabel(c)},
		{Label: "TLS", Value: yesNo(c.TLS)},
		{Label: "Description", Value: strings.TrimSpace(c.Description)},
		{Label: "ARN", Value: c.ARN},
	})
}
