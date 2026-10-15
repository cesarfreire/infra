package instances

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"infra/internal/app"
	"infra/internal/domain"
	"infra/internal/session"
	"infra/internal/tui/components/actionmenu"
	"infra/internal/tui/section"
	"infra/internal/tui/theme"
)

type fakeService struct {
	instances    []domain.Instance
	forwarded    [2]int
	consoled     string
	metricsCalls int
	powered      []string
}

func (f *fakeService) List(context.Context) ([]domain.Instance, error) { return f.instances, nil }

func (f *fakeService) Connect(inst domain.Instance) (app.Command, error) {
	if !inst.IsRunning() {
		return app.Command{}, domain.ErrInstanceNotRunning
	}
	return app.Command{Name: "true"}, nil
}

func (f *fakeService) PortForward(_ domain.Instance, local, remote int) (app.Command, error) {
	f.forwarded = [2]int{local, remote}
	return app.Command{Name: "true"}, nil
}

func (f *fakeService) Metrics(context.Context, domain.Instance, domain.MetricWindow) ([]domain.Series, error) {
	f.metricsCalls++
	return []domain.Series{{Label: "CPU", Unit: domain.UnitPercent, Points: []domain.Point{{Time: time.Now().Add(-time.Minute), Value: 42}}}}, nil
}

func (f *fakeService) Start(_ context.Context, i domain.Instance) error  { return f.power("start", i) }
func (f *fakeService) Stop(_ context.Context, i domain.Instance) error   { return f.power("stop", i) }
func (f *fakeService) Reboot(_ context.Context, i domain.Instance) error { return f.power("reboot", i) }

func (f *fakeService) power(verb string, i domain.Instance) error {
	f.powered = append(f.powered, verb+" "+i.ID)
	return nil
}

func (f *fakeService) OpenConsole(inst domain.Instance) (string, error) {
	f.consoled = inst.ID
	return "https://console", nil
}

type started struct {
	kind  session.Kind
	title string
}

type recorder struct{ started []started }

func (r *recorder) Start(kind session.Kind, title string, _ app.Command) (*session.Session, error) {
	r.started = append(r.started, started{kind, title})
	return &session.Session{ID: len(r.started), Kind: kind, Title: title}, nil
}

func press(k string) tea.KeyPressMsg {
	switch k {
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "down":
		return tea.KeyPressMsg{Code: tea.KeyDown}
	case "tab":
		return tea.KeyPressMsg{Code: tea.KeyTab}
	}
	return tea.KeyPressMsg{Code: rune(k[0]), Text: k}
}

func typeText(m *Model, s string) {
	for _, r := range s {
		m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
}

func newLoaded(t *testing.T) (*Model, *fakeService, *recorder) {
	t.Helper()
	svc := &fakeService{instances: []domain.Instance{
		{ID: "i-1", Name: "web", State: domain.InstanceStateRunning},
		{ID: "i-2", Name: "db", State: domain.InstanceStateStopped},
	}}
	rec := &recorder{}
	m := New(svc, rec, section.MemoryDeps(theme.Default()))
	m.SetSize(120, 20)
	m.Focus()
	m.Init()
	m.Update(instancesLoadedMsg{instances: svc.instances})
	return m, svc, rec
}

func statusOf(t *testing.T, cmd tea.Cmd) section.StatusMsg {
	t.Helper()
	if cmd == nil {
		t.Fatal("expected a command")
	}
	msg, ok := cmd().(section.StatusMsg)
	if !ok {
		t.Fatalf("expected StatusMsg, got %T", cmd())
	}
	return msg
}

func TestListRendersInstances(t *testing.T) {
	m, _, _ := newLoaded(t)
	out := m.View()
	for _, want := range []string{"web", "i-2", "2 found"} {
		if !strings.Contains(out, want) {
			t.Errorf("view missing %q", want)
		}
	}
}

func TestConnectOpensShellSession(t *testing.T) {
	m, _, rec := newLoaded(t)
	_, cmd := m.Update(press("c"))
	if len(rec.started) != 1 || rec.started[0].kind != session.KindShell {
		t.Fatalf("expected one shell session, got %+v", rec.started)
	}
	if msg, ok := cmd().(section.OpenSessionMsg); !ok || msg.ID != 1 {
		t.Errorf("expected OpenSessionMsg{1}, got %#v", cmd())
	}
}

func TestConnectStoppedInstanceShowsError(t *testing.T) {
	m, _, rec := newLoaded(t)
	m.Update(press("down"))
	_, cmd := m.Update(press("c"))
	if st := statusOf(t, cmd); !st.Error || len(rec.started) != 0 {
		t.Errorf("expected error status and no session; got %+v, %d sessions", st, len(rec.started))
	}
}

func TestActionsMenuAndDetails(t *testing.T) {
	m, _, _ := newLoaded(t)
	m.Update(press("enter"))
	if m.view != viewActions || m.InputMode() != section.ModeCapture {
		t.Fatal("enter should open the actions menu")
	}
	// Details is the 4th action.
	for range 3 {
		m.Update(press("down"))
	}
	m.Update(press("enter"))
	if m.view != viewDetails || !strings.Contains(m.View(), "i-1") {
		t.Fatal("selecting Details should show the details view")
	}
	m.Update(press("esc"))
	if m.view != viewList || m.InputMode() != section.ModeNormal {
		t.Error("esc should go back to the list")
	}
}

func TestActionsMenuMouseClick(t *testing.T) {
	m, svc, _ := newLoaded(t)
	m.Update(press("enter"))
	_, cmd := m.Update(tea.MouseClickMsg{X: 2, Y: actionmenu.Top + 2, Button: tea.MouseLeft})
	statusOf(t, cmd)
	if svc.consoled != "i-1" {
		t.Error("clicking the console action should open the console")
	}
}

func TestPortForwardForm(t *testing.T) {
	m, svc, rec := newLoaded(t)
	m.Update(press("p"))
	if m.view != viewPortForward {
		t.Fatal("p should open the port forward form")
	}
	m.Update(press("enter"))
	if m.form.Err() == nil {
		t.Error("submitting an empty form should fail")
	}
	typeText(m, "5432")
	m.Update(press("tab"))
	typeText(m, "15432")
	_, cmd := m.Update(press("enter"))
	if svc.forwarded != [2]int{15432, 5432} || len(rec.started) != 1 || m.view != viewList {
		t.Fatalf("forwarded = %v, sessions = %d, view = %v", svc.forwarded, len(rec.started), m.view)
	}
	if rec.started[0].kind != session.KindPortForward || statusOf(t, cmd).Error {
		t.Errorf("expected a background port-forward session, got %+v", rec.started[0])
	}
}

func TestPortForwardLocalDefaultsToRemote(t *testing.T) {
	m, svc, _ := newLoaded(t)
	m.Update(press("p"))
	typeText(m, "22")
	m.Update(press("enter"))
	if svc.forwarded != [2]int{22, 22} {
		t.Errorf("forwarded = %v, want [22 22]", svc.forwarded)
	}
}

func TestLoadErrorIsShown(t *testing.T) {
	m := New(&fakeService{}, &recorder{}, section.MemoryDeps(theme.Default()))
	m.Init()
	_, cmd := m.Update(instancesLoadedMsg{err: errors.New("access denied")})
	if st := statusOf(t, cmd); !st.Error || !strings.Contains(m.View(), "access denied") {
		t.Errorf("load error should be shown; status %+v", st)
	}
}

func TestFilter(t *testing.T) {
	m, _, _ := newLoaded(t)
	m.Update(press("/"))
	if !m.list.Filtering() || m.InputMode() != section.ModeCapture {
		t.Fatal("/ should start editing the filter")
	}
	typeText(m, "state:stop")
	if len(m.list.Visible()) != 1 || m.list.Visible()[0].ID != "i-2" {
		t.Fatalf("visible = %+v, want only i-2", m.list.Visible())
	}
	if !strings.Contains(m.View(), "1 of 2") {
		t.Error("heading should show the filtered count")
	}

	// enter keeps the filter and returns to the table: actions use the
	// filtered row.
	m.Update(press("enter"))
	if m.list.Filtering() {
		t.Fatal("enter should stop editing")
	}
	if inst, _ := m.list.Selected(); inst.ID != "i-2" {
		t.Errorf("current() = %s, want i-2", inst.ID)
	}

	// esc on an applied filter clears it instead of leaving the section.
	m.Update(press("esc"))
	if !m.list.Query().Empty() || len(m.list.Visible()) != 2 || m.InputMode() != section.ModeNormal {
		t.Errorf("esc should clear the filter; visible = %d", len(m.list.Visible()))
	}
}

func TestFilterNoMatch(t *testing.T) {
	m, _, _ := newLoaded(t)
	m.Update(press("/"))
	typeText(m, "nothing-like-this")
	if !strings.Contains(m.View(), "No instances match") {
		t.Error("expected the no-match message")
	}
	if _, ok := m.list.Selected(); ok {
		t.Error("current() should be empty when nothing matches")
	}
}

func TestDetailsShowMetricsAndStopRefreshing(t *testing.T) {
	m, svc, _ := newLoaded(t)
	_, cmd := m.Update(press("d"))
	if m.view != viewDetails || !strings.Contains(m.View(), "Loading metrics") {
		t.Fatalf("d should open details and load metrics:\n%s", m.View())
	}
	_, next := m.Update(cmd()) // metrics loaded; returns the refresh timer
	view := m.View()
	if svc.metricsCalls != 1 || !strings.Contains(view, "Monitoring") || !strings.Contains(view, "now 42%") || !strings.Contains(view, "█") {
		t.Fatalf("expected a CPU chart:\n%s", view)
	}
	if next == nil {
		t.Fatal("a refresh should be scheduled")
	}

	m.Update(press("esc"))
	// The refresh timer fires after the user left: no new request.
	for _, msg := range drain(next) {
		if _, more := m.Update(msg); more != nil {
			t.Error("a closed panel must not reload")
		}
	}
	if svc.metricsCalls != 1 {
		t.Errorf("metrics calls = %d, want 1", svc.metricsCalls)
	}
}

// drain runs cmd without waiting for timers: tea.Tick commands are
// replaced by their message.
func drain(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	done := make(chan tea.Msg, 1)
	go func() { done <- cmd() }()
	select {
	case msg := <-done:
		return []tea.Msg{msg}
	case <-time.After(50 * time.Millisecond):
		return nil
	}
}
