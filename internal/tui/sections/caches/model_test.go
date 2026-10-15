package caches

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"infra/internal/app"
	"infra/internal/domain"
	"infra/internal/session"
	"infra/internal/tui/components/picker"
	"infra/internal/tui/section"
	"infra/internal/tui/theme"
)

var valkey = domain.Cache{ID: "sessions", Engine: "valkey", EngineVersion: "8.0", Status: "available", TLS: true,
	NodeType: "cache.r7g.large", Nodes: 2, Endpoint: domain.Endpoint{Host: "master.sessions.cache.amazonaws.com", Port: 6379}}

type fakeService struct {
	connected [2]string // cache, host
	forwarded int
}

func (f *fakeService) ListCaches(context.Context) ([]domain.Cache, error) {
	return []domain.Cache{valkey, {ID: "memo", Engine: "memcached", Status: "available"}}, nil
}

func (f *fakeService) JumpHosts(context.Context) ([]domain.Instance, error) {
	return []domain.Instance{
		{ID: "i-1", Name: "bastion", State: domain.InstanceStateRunning},
		{ID: "i-2", Name: "worker", State: domain.InstanceStateRunning},
	}, nil
}

func (f *fakeService) PortForward(_ domain.Cache, _ domain.Instance, local int) (app.Command, error) {
	f.forwarded = local
	return app.Command{Name: "true"}, nil
}

func (f *fakeService) Connect(c domain.Cache, h domain.Instance) (app.Command, error) {
	f.connected = [2]string{c.ID, h.ID}
	return app.Command{Name: "true"}, nil
}

func (f *fakeService) OpenConsole(domain.Cache) (string, error) { return "", nil }

func (f *fakeService) Metrics(context.Context, domain.Cache, domain.MetricWindow) ([]domain.Series, error) {
	return []domain.Series{{Label: "Engine CPU", Unit: domain.UnitPercent, Points: []domain.Point{{Time: time.Now().Add(-time.Minute), Value: 12}}}}, nil
}

type recorder struct{ titles []string }

func (r *recorder) Start(kind session.Kind, title string, _ app.Command) (*session.Session, error) {
	r.titles = append(r.titles, title)
	return &session.Session{ID: len(r.titles), Kind: kind, Title: title}, nil
}

func press(k string) tea.KeyPressMsg {
	switch k {
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "down":
		return tea.KeyPressMsg{Code: tea.KeyDown}
	}
	return tea.KeyPressMsg{Code: rune(k[0]), Text: k}
}

func send(m *Model, msgs ...tea.Msg) []tea.Msg {
	var out []tea.Msg
	for len(msgs) > 0 {
		msg := msgs[0]
		msgs = msgs[1:]
		_, cmd := m.Update(msg)
		for _, r := range run(cmd) {
			switch r.(type) {
			case cachesLoadedMsg, hostsLoadedMsg:
				msgs = append(msgs, r)
			default:
				out = append(out, r)
			}
		}
	}
	return out
}

func run(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	ch := make(chan tea.Msg, 1)
	go func() { ch <- cmd() }()
	select {
	case msg := <-ch:
		if b, ok := msg.(tea.BatchMsg); ok {
			var out []tea.Msg
			for _, c := range b {
				out = append(out, run(c)...)
			}
			return out
		}
		if msg == nil || strings.Contains(fmt.Sprintf("%T", msg), "Tick") {
			return nil
		}
		return []tea.Msg{msg}
	case <-time.After(100 * time.Millisecond):
		return nil
	}
}

func newModel(t *testing.T) (*Model, *fakeService, *recorder) {
	t.Helper()
	svc, rec := &fakeService{}, &recorder{}
	m := New(svc, rec, section.MemoryDeps(theme.Default()))
	m.SetSize(140, 20)
	m.Focus()
	send(m, run(m.Init())...)
	return m, svc, rec
}

func TestListShowsCaches(t *testing.T) {
	m, _, _ := newModel(t)
	view := m.View()
	for _, want := range []string{"sessions", "valkey 8.0", "cache.r7g.large ×2", "memcached"} {
		if !strings.Contains(view, want) {
			t.Errorf("list missing %q:\n%s", want, view)
		}
	}
}

func TestConnectThroughJumpHost(t *testing.T) {
	m, svc, rec := newModel(t)
	send(m, press("c"))
	if m.view != viewHostPicker || m.picker == nil || !strings.Contains(m.View(), "bastion") {
		t.Fatalf("c should ask for a jump host:\n%s", m.View())
	}
	out := send(m, press("down"), press("enter"))
	if svc.connected != [2]string{"sessions", "i-2"} || len(rec.titles) != 1 {
		t.Fatalf("connected = %v, sessions = %v", svc.connected, rec.titles)
	}
	if len(out) != 1 {
		t.Fatalf("out = %v", out)
	}
	if _, ok := out[0].(section.OpenSessionMsg); !ok {
		t.Errorf("expected OpenSessionMsg, got %#v", out[0])
	}

	// The last host is preselected next time.
	send(m, press("c"))
	if sel, _ := m.picker.Selected(); sel.Value != "i-2" {
		t.Errorf("preselected = %v, want i-2", sel)
	}
}

func TestPortForwardThroughJumpHost(t *testing.T) {
	m, svc, rec := newModel(t)
	send(m, press("p"))
	send(m, press("enter"))
	if m.view != viewPortForward || !strings.Contains(m.View(), "Remote port: 6379") || !strings.Contains(m.View(), "--tls") {
		t.Fatalf("expected the port form with a TLS hint:\n%s", m.View())
	}
	out := send(m, press("enter"))
	if svc.forwarded != 6379 || len(rec.titles) != 1 || !strings.Contains(rec.titles[0], "via bastion") {
		t.Errorf("forwarded = %d, sessions = %v", svc.forwarded, rec.titles)
	}
	if len(out) != 1 {
		t.Fatalf("out = %v", out)
	}
	if st, ok := out[0].(section.StatusMsg); !ok || st.Error {
		t.Errorf("expected a status, got %#v", out[0])
	}
}

func TestPickerCancelAndClick(t *testing.T) {
	m, svc, _ := newModel(t)
	send(m, press("c"), press("esc"))
	if m.view != viewList {
		t.Fatal("esc should cancel the picker")
	}
	send(m, press("c"))
	send(m, tea.MouseClickMsg{X: 2, Y: picker.Top + 1, Button: tea.MouseLeft})
	if svc.connected[1] != "i-2" {
		t.Errorf("clicking the second host should use it; got %v", svc.connected)
	}
}

func TestActionsMenuAndDetails(t *testing.T) {
	m, _, _ := newModel(t)
	send(m, press("enter"))
	if m.view != viewActions || !strings.Contains(m.View(), "Open CLI via jump host") {
		t.Fatal("enter should open the actions")
	}
	send(m, press("d"))
	if m.view != viewDetails || !strings.Contains(m.View(), "master.sessions.cache.amazonaws.com:6379") {
		t.Errorf("details:\n%s", m.View())
	}
}
