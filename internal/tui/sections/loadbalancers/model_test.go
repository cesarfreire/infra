package loadbalancers

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"infra/internal/domain"
	"infra/internal/tui/section"
	"infra/internal/tui/theme"
)

type fakeService struct{}

var web = domain.LoadBalancer{Name: "web", ARN: "arn:lb/web", Type: "application", Scheme: "internet-facing", State: "active"}

func (fakeService) List(context.Context) ([]domain.LoadBalancer, error) {
	return []domain.LoadBalancer{web, {Name: "internal-api", ARN: "arn:lb/api", Type: "network"}}, nil
}

func (fakeService) Listeners(context.Context, domain.LoadBalancer) ([]domain.Listener, error) {
	return []domain.Listener{{Protocol: "HTTPS", Port: 443, Action: "forward to api-tg"}}, nil
}

func (fakeService) TargetGroups(context.Context, domain.LoadBalancer) ([]domain.TargetGroup, error) {
	return []domain.TargetGroup{{Name: "api-tg", Protocol: "HTTP", Port: 8080, Targets: []domain.Target{
		{ID: "10.0.0.5", Port: 8080, State: "unhealthy", Reason: "Target.FailedHealthChecks"},
		{ID: "10.0.0.6", Port: 8080, State: "healthy"},
	}}}, nil
}

func (fakeService) Metrics(context.Context, domain.LoadBalancer, domain.MetricWindow) ([]domain.Series, error) {
	return nil, nil
}

func (fakeService) OpenConsole(domain.LoadBalancer) (string, error) { return "", nil }

func press(k string) tea.KeyPressMsg {
	switch k {
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	}
	return tea.KeyPressMsg{Code: rune(k[0]), Text: k}
}

func send(m *Model, msgs ...tea.Msg) {
	for len(msgs) > 0 {
		msg := msgs[0]
		msgs = msgs[1:]
		_, cmd := m.Update(msg)
		for _, r := range run(cmd) {
			switch r.(type) {
			case section.StatusMsg, section.AlertMsg:
			default:
				msgs = append(msgs, r)
			}
		}
	}
}

func run(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	done := make(chan tea.Msg, 1)
	go func() { done <- cmd() }()
	select {
	case msg := <-done:
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

func newModel(t *testing.T) *Model {
	t.Helper()
	m := New(fakeService{}, section.MemoryDeps(theme.Default()))
	m.SetSize(120, 40)
	m.Focus()
	send(m, run(m.Init())...)
	return m
}

func TestDrillDownToTargets(t *testing.T) {
	m := newModel(t)
	send(m, press("enter"))
	if m.level != levelGroups || !strings.Contains(m.View(), "1/2") {
		t.Fatalf("target groups should show healthy counts:\n%s", m.View())
	}
	send(m, press("enter"))
	if view := m.View(); m.level != levelTargets || !strings.Contains(view, "unhealthy") || !strings.Contains(view, "FailedHealthChecks") {
		t.Fatalf("targets:\n%s", view)
	}
	send(m, press("esc"))
	send(m, press("esc"))
	if m.level != levelBalancers || m.InputMode() != section.ModeNormal {
		t.Error("esc should go back up")
	}
}

func TestDetailsShowListeners(t *testing.T) {
	m := newModel(t)
	send(m, press("d"))
	if view := m.View(); !m.showDetails || !strings.Contains(view, "HTTPS") || !strings.Contains(view, "forward to api-tg") {
		t.Errorf("details:\n%s", view)
	}
	send(m, press("esc"))
	if m.showDetails {
		t.Error("esc should close the details")
	}
}

func TestFavoriteAndReveal(t *testing.T) {
	m := newModel(t)
	send(m, press("*"))
	if !strings.Contains(m.View(), "★ ") {
		t.Errorf("expected a star:\n%s", m.View())
	}
	send(m, section.RevealMsg{Bookmark: domain.Bookmark{Kind: domain.KindLoadBalancer, ID: "web"}})
	if lb, _ := m.balancers.Selected(); lb.Name != "web" {
		t.Errorf("selected %q", lb.Name)
	}
}
