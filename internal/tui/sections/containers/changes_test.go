package containers

import (
	"strings"
	"testing"

	"infra/internal/domain"
	"infra/internal/tui/section"
)

func confirmIn(t *testing.T, out []any) section.ConfirmMsg {
	t.Helper()
	for _, msg := range out {
		if c, ok := msg.(section.ConfirmMsg); ok {
			return c
		}
	}
	t.Fatalf("expected a confirmation in %v", out)
	return section.ConfirmMsg{}
}

func asAny[T any](xs []T) []any {
	out := make([]any, len(xs))
	for i, x := range xs {
		out[i] = x
	}
	return out
}

func TestForceDeployFromServices(t *testing.T) {
	m, svc, _ := newModel(t)
	send(m, press("enter")) // services of prod
	c := confirmIn(t, asAny(send(m, press("D"))))
	if !strings.Contains(c.Body, "api") || len(svc.changes) != 0 {
		t.Fatalf("confirm body %q, changes %v", c.Body, svc.changes)
	}
	send(m, c.Yes())
	if len(svc.changes) != 1 || svc.changes[0] != "deploy api" {
		t.Errorf("changes = %v", svc.changes)
	}
}

func TestScaleService(t *testing.T) {
	m, svc, _ := newModel(t)
	send(m, press("enter"))
	send(m, press("N"))
	if m.view != viewScale || !strings.Contains(m.View(), "Desired tasks") {
		t.Fatalf("N should ask for the desired count:\n%s", m.View())
	}
	for range 3 { // clear the prefilled "0"
		send(m, press("backspace"))
	}
	for _, r := range "4" {
		send(m, typeRune(r))
	}
	c := confirmIn(t, asAny(send(m, press("enter"))))
	if !strings.Contains(c.Body, "to 4 tasks") {
		t.Errorf("body = %q", c.Body)
	}
	send(m, c.Yes())
	if len(svc.changes) != 1 || svc.changes[0] != "scale api 4" {
		t.Errorf("changes = %v", svc.changes)
	}
}

func TestServiceDetailsFollowDeployments(t *testing.T) {
	m, svc, _ := newModel(t)
	m.SetSize(120, 60)
	send(m, press("enter"))
	send(m, press("d"))
	view := m.View()
	for _, want := range []string{"Deployments", "rolling out", "api:8", "1/3 running", "has started 2 tasks"} {
		if !strings.Contains(view, want) {
			t.Errorf("service details missing %q:\n%s", want, view)
		}
	}
	if svc.reads != 1 {
		t.Errorf("the service should be re-read once on open, got %d", svc.reads)
	}
	send(m, serviceTickMsg{seq: m.svcSeq})
	if svc.reads != 2 {
		t.Error("a tick should re-read the service")
	}
	seq := m.svcSeq
	send(m, press("esc"))
	send(m, serviceTickMsg{seq: seq})
	if svc.reads != 2 {
		t.Error("ticks after leaving must not read again")
	}
}

func TestRevealService(t *testing.T) {
	m, _, _ := newModel(t)
	send(m, section.RevealMsg{Bookmark: domain.Bookmark{Kind: domain.KindECSService, ID: "web", Parent: "staging"}})
	if m.level != levelServices || m.cluster != "staging" {
		t.Fatalf("level %v cluster %q", m.level, m.cluster)
	}
	if s, _ := m.services.Selected(); s.Name != "web" {
		t.Errorf("selected %q, want web", s.Name)
	}
}

func TestReadOnlyBlocksDeploy(t *testing.T) {
	m, svc, _ := newModel(t)
	m.deps.ReadOnly = true
	send(m, press("enter"))
	out := send(m, press("D"))
	for _, msg := range out {
		if _, ok := msg.(section.ConfirmMsg); ok {
			t.Fatal("no confirmation when read-only")
		}
	}
	if len(svc.changes) != 0 {
		t.Error("nothing should change")
	}
}
