package caches

import (
	"context"
	"fmt"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"infra/internal/domain"
	"infra/internal/session"
	"infra/internal/tui/components/actionmenu"
	"infra/internal/tui/components/picker"
	"infra/internal/tui/section"
)

// action is an operation on a cache. The same list drives the actions menu,
// the hotkeys in the list and mouse clicks.
type action struct {
	label string
	key   key.Binding
	run   func(m *Model, c domain.Cache) tea.Cmd
}

func (m *Model) actions() []action {
	return []action{
		{label: "Open CLI via jump host (valkey-cli/redis-cli)", key: m.keys.Connect, run: func(m *Model, c domain.Cache) tea.Cmd {
			return m.askHost(c, pendingConnect)
		}},
		{label: "Port forward via jump host (SSM)", key: m.keys.PortForward, run: func(m *Model, c domain.Cache) tea.Cmd {
			return m.askHost(c, pendingPortForward)
		}},
		{label: "Open in AWS console", key: m.keys.Console, run: (*Model).openConsole},
		{label: "Details and monitoring", key: m.keys.Details, run: func(m *Model, c domain.Cache) tea.Cmd {
			m.deps.Visit(bookmark(c))
			m.cache = c
			m.view = viewDetails
			svc := m.svc
			hint := ""
			if len(c.Members) > 0 {
				hint = "No data points for node " + c.Members[0] + " in this time range."
			}
			return m.details.Open(m.detailsHeader(), func(ctx context.Context, w domain.MetricWindow) ([]domain.Series, error) {
				return svc.Metrics(ctx, c, w)
			}, hint)
		}},
	}
}

func (m *Model) menuItems() []actionmenu.Item {
	var items []actionmenu.Item
	for _, a := range m.actions() {
		items = append(items, actionmenu.Item{Label: a.label, Key: a.key})
	}
	return items
}

func (m *Model) actionForKey(msg tea.KeyPressMsg) (action, bool) {
	for _, a := range m.actions() {
		if key.Matches(msg, a.key) {
			return a, true
		}
	}
	return action{}, false
}

func (m *Model) runAction(a action, c domain.Cache) tea.Cmd {
	m.details.Close()
	m.view = viewList
	return a.run(m, c)
}

// askHost shows the jump host picker, loading the running instances.
func (m *Model) askHost(c domain.Cache, p pendingAction) tea.Cmd {
	m.cache, m.pending = c, p
	m.view = viewHostPicker
	m.picker = nil
	m.hostsLoading, m.hostsErr = true, nil
	m.hostsSeq++
	seq, svc := m.hostsSeq, m.svc
	return tea.Batch(m.spinner.Tick, func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), loadTimeout)
		defer cancel()
		hosts, err := svc.JumpHosts(ctx)
		return hostsLoadedMsg{seq: seq, hosts: hosts, err: err}
	})
}

func (m *Model) handleHosts(msg hostsLoadedMsg) tea.Cmd {
	if msg.seq != m.hostsSeq || m.view != viewHostPicker {
		return nil
	}
	m.hostsLoading = false
	if msg.err != nil {
		m.hostsErr = msg.err
		return section.Error(msg.err)
	}
	m.hosts = msg.hosts
	items := make([]picker.Item, len(msg.hosts))
	for i, h := range msg.hosts {
		items[i] = picker.Item{Value: h.ID, Label: h.DisplayName(), Detail: strings.Join(nonEmpty(h.ID, h.PrivateIP, h.AvailabilityZone), " · ")}
	}
	preselect := m.deps.Prefs.Get(jumpHostPref(m.cache.ID))
	if preselect == "" {
		preselect = m.lastHost
	}
	m.picker = picker.New("Jump host for "+m.cache.ID, items, preselect, max(3, m.height-6))
	return m.picker.Focus()
}

func (m *Model) handlePickerKey(msg tea.KeyPressMsg) tea.Cmd {
	if m.picker == nil {
		if key.Matches(msg, m.keys.Back) {
			m.view = viewList
		}
		return nil
	}
	result, cmd := m.picker.Update(msg)
	switch result {
	case picker.Cancelled:
		m.view = viewList
		return nil
	case picker.Chosen:
		return m.chooseHost()
	}
	return cmd
}

func (m *Model) chooseHost() tea.Cmd {
	item, ok := m.picker.Selected()
	if !ok {
		return nil
	}
	for _, h := range m.hosts {
		if h.ID == item.Value {
			m.host = h
		}
	}
	m.lastHost = m.host.ID
	_ = m.deps.Prefs.Set(jumpHostPref(m.cache.ID), m.host.ID) // best effort: only a preselection
	m.view = viewList
	if m.pending == pendingConnect {
		return m.connect()
	}
	hint := "The tunnel runs in the background; follow or close it in Sessions."
	if m.cache.TLS {
		hint = "TLS is on: connect with --tls and skip hostname verification (e.g. redis-cli --tls --insecure -h localhost), " +
			"since the certificate is for the AWS endpoint name."
	}
	m.view = viewPortForward
	return m.form.ResetFixedRemote(m.cache.Endpoint.Port, hint)
}

// connect opens the engine CLI on the jump host and shows it in Sessions.
func (m *Model) connect() tea.Cmd {
	cmd, err := m.svc.Connect(m.cache, m.host)
	if err != nil {
		return section.Error(err)
	}
	title := fmt.Sprintf("%s · cli via %s", m.cache.ID, m.host.DisplayName())
	s, err := m.sessions.Start(session.KindShell, title, cmd)
	if err != nil {
		return section.Error(fmt.Errorf("starting session: %w", err))
	}
	return func() tea.Msg { return section.OpenSessionMsg{ID: s.ID} }
}

func (m *Model) submitPortForward() tea.Cmd {
	local, _, err := m.form.Ports()
	if err != nil {
		m.form.SetError(err)
		return nil
	}
	cmd, err := m.svc.PortForward(m.cache, m.host, local)
	if err != nil {
		m.form.SetError(err)
		return nil
	}
	title := fmt.Sprintf("%s · localhost:%d → %d via %s", m.cache.ID, local, m.cache.Endpoint.Port, m.host.DisplayName())
	if _, err := m.sessions.Start(session.KindPortForward, title, cmd); err != nil {
		m.form.SetError(fmt.Errorf("starting session: %w", err))
		return nil
	}
	m.view = viewList
	return section.Status(fmt.Sprintf("Port forward started on localhost:%d (see Sessions)", local))
}

func (m *Model) openConsole(c domain.Cache) tea.Cmd {
	return func() tea.Msg {
		url, err := m.svc.OpenConsole(c)
		if err != nil {
			return section.StatusMsg{Text: fmt.Sprintf("%v (%s)", err, url), Error: true}
		}
		return section.StatusMsg{Text: "Opened console for " + c.ID}
	}
}

func nonEmpty(values ...string) []string {
	var out []string
	for _, v := range values {
		if v != "" {
			out = append(out, v)
		}
	}
	return out
}
