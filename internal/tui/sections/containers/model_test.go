package containers

import (
	"context"
	"errors"
	"fmt"
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

var (
	app1 = domain.Container{Name: "app", RuntimeID: "r1", Status: "RUNNING", Image: "api:1"}
	side = domain.Container{Name: "envoy", RuntimeID: "r2", Status: "RUNNING", Image: "envoy:1"}

	single = domain.Task{ID: "aaaaaaaa1111", Cluster: "prod", Service: "api", LastStatus: "RUNNING", ExecEnabled: true,
		Containers: []domain.Container{app1}}
	multi = domain.Task{ID: "bbbbbbbb2222", Cluster: "prod", Service: "web", LastStatus: "RUNNING", ExecEnabled: true,
		Containers: []domain.Container{app1, side}}
)

type fakeService struct {
	tasksFor  [2]string // last ListTasks cluster, service
	connected string    // container name
	forwarded [2]int
	consoled  string
	reads     int
	changes   []string
	// rollouts are the snapshots Rollout returns, one per call.
	rollouts     []domain.Rollout
	rolloutCalls int
}

func (f *fakeService) ListClusters(context.Context) ([]domain.Cluster, error) {
	return []domain.Cluster{{Name: "prod", Status: "ACTIVE"}, {Name: "staging", Status: "ACTIVE"}}, nil
}

func (f *fakeService) ListServices(_ context.Context, cluster string) ([]domain.Service, error) {
	return []domain.Service{{Name: "api", Cluster: cluster}, {Name: "web", Cluster: cluster}}, nil
}

func (f *fakeService) ListTasks(_ context.Context, cluster, service string) ([]domain.Task, error) {
	f.tasksFor = [2]string{cluster, service}
	return []domain.Task{single, multi}, nil
}

func (f *fakeService) Connect(_ domain.Task, c domain.Container) (app.Command, error) {
	f.connected = c.Name
	return app.Command{Name: "true"}, nil
}

func (f *fakeService) PortForward(_ domain.Task, _ domain.Container, local, remote int) (app.Command, error) {
	f.forwarded = [2]int{local, remote}
	return app.Command{Name: "true"}, nil
}

func (f *fakeService) OpenClusterConsole(c domain.Cluster) (string, error) {
	f.consoled = "cluster/" + c.Name
	return "", nil
}

func (f *fakeService) OpenServiceConsole(s domain.Service) (string, error) {
	f.consoled = "service/" + s.Name
	return "", nil
}

func (f *fakeService) ServiceMetrics(context.Context, domain.Service, domain.MetricWindow) ([]domain.Series, error) {
	return []domain.Series{{Label: "CPU", Unit: domain.UnitPercent, Points: []domain.Point{{Time: time.Now().Add(-time.Minute), Value: 42}}}}, nil
}

func (f *fakeService) TaskMetrics(context.Context, domain.Task, domain.MetricWindow) ([]domain.Series, error) {
	return []domain.Series{{Label: "CPU", Unit: domain.UnitPercent}}, nil // no Container Insights
}

func (f *fakeService) GetService(_ context.Context, cluster, name string) (domain.Service, error) {
	f.reads++
	return domain.Service{Name: name, Cluster: cluster, Desired: 3, Running: 1,
		Deployments: []domain.Deployment{{Status: "PRIMARY", RolloutState: "IN_PROGRESS", TaskDefinition: "api:8", Desired: 3, Running: 1}},
		Events:      []domain.ServiceEvent{{Time: time.Now(), Message: "(service api) has started 2 tasks"}},
	}, nil
}

// Rollout returns the next snapshot of rollouts, repeating the last one.
func (f *fakeService) Rollout(_ context.Context, s domain.Service) (domain.Rollout, error) {
	f.rolloutCalls++
	if len(f.rollouts) == 0 {
		return domain.Rollout{Service: s}, nil
	}
	r := f.rollouts[min(f.rolloutCalls, len(f.rollouts))-1]
	return r, nil
}

func (f *fakeService) PreviousRevision(context.Context, domain.Service) (app.RevisionChange, error) {
	from := domain.TaskDefinition{Family: "api", Revision: 7, Containers: []domain.ContainerDefinition{{Name: "app", Image: "repo/api:v41"}}}
	to := domain.TaskDefinition{Family: "api", Revision: 8, Containers: []domain.ContainerDefinition{{Name: "app", Image: "repo/api:v42"}}}
	return app.RevisionChange{From: from, To: to, Changes: domain.DiffTaskDefinitions(from, to)}, nil
}

func (f *fakeService) Rollback(_ context.Context, s domain.Service, td domain.TaskDefinition) error {
	f.changes = append(f.changes, "rollback "+s.Name+" to "+td.Name())
	return nil
}

func (f *fakeService) ForceDeployment(_ context.Context, s domain.Service) error {
	f.changes = append(f.changes, "deploy "+s.Name)
	return nil
}

func (f *fakeService) SetDesiredCount(_ context.Context, s domain.Service, n int) error {
	f.changes = append(f.changes, fmt.Sprintf("scale %s %d", s.Name, n))
	return nil
}

func (f *fakeService) OpenTaskConsole(t domain.Task) (string, error) {
	f.consoled = "task/" + t.ID
	return "", nil
}

type recorder struct{ kinds []session.Kind }

func (r *recorder) Start(kind session.Kind, title string, _ app.Command) (*session.Session, error) {
	r.kinds = append(r.kinds, kind)
	return &session.Session{ID: len(r.kinds), Kind: kind, Title: title}, nil
}

func press(k string) tea.KeyPressMsg {
	switch k {
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "down":
		return tea.KeyPressMsg{Code: tea.KeyDown}
	case "backspace":
		return tea.KeyPressMsg{Code: tea.KeyBackspace}
	}
	return tea.KeyPressMsg{Code: rune(k[0]), Text: k}
}

// send updates the model and runs the resulting commands, feeding their
// messages back, like the Bubble Tea runtime (spinner ticks are dropped).
func send(m *Model, msgs ...tea.Msg) []tea.Msg {
	var out []tea.Msg
	for len(msgs) > 0 {
		msg := msgs[0]
		msgs = msgs[1:]
		_, cmd := m.Update(msg)
		for _, r := range run(cmd) {
			switch r.(type) {
			case section.StatusMsg, section.AlertMsg, section.OpenSessionMsg, section.NavigateMsg, section.ConfirmMsg, section.NotifyMsg:
				out = append(out, r) // for the root model
			default:
				msgs = append(msgs, r) // load results, metrics...
			}
		}
	}
	return out
}

// run executes cmd; commands that take long (timers) are dropped.
func run(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	done := make(chan tea.Msg, 1)
	go func() { done <- cmd() }()
	var result tea.Msg
	select {
	case result = <-done:
	case <-time.After(100 * time.Millisecond):
		return nil
	}
	switch msg := result.(type) {
	case nil:
		return nil
	case tea.BatchMsg:
		var out []tea.Msg
		for _, c := range msg {
			out = append(out, run(c)...)
		}
		return out
	default:
		if strings.Contains(fmt.Sprintf("%T", msg), "Tick") {
			return nil
		}
		return []tea.Msg{msg}
	}
}

func newModel(t *testing.T) (*Model, *fakeService, *recorder) {
	t.Helper()
	svc, rec := &fakeService{}, &recorder{}
	m := New(svc, rec, section.MemoryDeps(theme.Default()))
	m.SetSize(120, 20)
	m.Focus()
	for _, msg := range run(m.Init()) {
		send(m, msg)
	}
	return m, svc, rec
}

func findStatus(msgs []tea.Msg) (section.StatusMsg, bool) {
	for _, msg := range msgs {
		if st, ok := msg.(section.StatusMsg); ok {
			return st, true
		}
	}
	return section.StatusMsg{}, false
}

func TestDrillDownAndBack(t *testing.T) {
	m, svc, _ := newModel(t)
	if m.InputMode() != section.ModeNormal || !strings.Contains(m.View(), "staging") {
		t.Fatal("should start on the clusters list")
	}

	send(m, press("enter"))
	if m.level != levelServices || !strings.Contains(m.View(), "prod › Services") || m.InputMode() != section.ModeCapture {
		t.Fatalf("enter should open services of prod; view:\n%s", m.View())
	}

	send(m, press("down"), press("enter"))
	if m.level != levelTasks || svc.tasksFor != [2]string{"prod", "web"} || !strings.Contains(m.View(), "prod › web › Tasks") {
		t.Fatalf("enter should open tasks of web; got %v", svc.tasksFor)
	}

	send(m, press("esc"))
	if m.level != levelServices {
		t.Error("esc should go back to services")
	}
	send(m, press("esc"))
	if m.level != levelClusters || m.InputMode() != section.ModeNormal {
		t.Error("esc should go back to clusters")
	}
}

func TestAllTasksFromClusters(t *testing.T) {
	m, svc, _ := newModel(t)
	send(m, press("t"))
	if m.level != levelTasks || svc.tasksFor != [2]string{"prod", ""} || !strings.Contains(m.View(), "All tasks") {
		t.Fatalf("t should list all tasks of the cluster; got %v", svc.tasksFor)
	}
	send(m, press("esc"))
	if m.level != levelClusters {
		t.Error("esc should return to where tasks were opened from")
	}
}

func openTasks(t *testing.T) (*Model, *fakeService, *recorder) {
	t.Helper()
	m, svc, rec := newModel(t)
	send(m, press("t"))
	return m, svc, rec
}

func TestConnectSingleContainer(t *testing.T) {
	m, svc, rec := openTasks(t)
	out := send(m, press("c"))
	if svc.connected != "app" || len(rec.kinds) != 1 || rec.kinds[0] != session.KindShell {
		t.Fatalf("connected = %q, sessions = %v", svc.connected, rec.kinds)
	}
	if len(out) != 1 {
		t.Fatalf("expected one message, got %v", out)
	}
	if _, ok := out[0].(section.OpenSessionMsg); !ok {
		t.Errorf("expected OpenSessionMsg, got %#v", out[0])
	}
}

func TestConnectAsksForContainer(t *testing.T) {
	m, svc, _ := openTasks(t)
	send(m, press("down"), press("c"))
	if m.view != viewPicker || !strings.Contains(m.View(), "envoy") {
		t.Fatal("a task with two containers should show the picker")
	}
	send(m, press("down"), press("enter"))
	if svc.connected != "envoy" || m.view != viewList {
		t.Errorf("connected = %q, view = %v", svc.connected, m.view)
	}
}

func TestPickerMouseClick(t *testing.T) {
	m, svc, _ := openTasks(t)
	send(m, press("down"), press("c"))
	send(m, tea.MouseClickMsg{X: 2, Y: actionmenu.Top + 1, Button: tea.MouseLeft})
	if svc.connected != "envoy" {
		t.Errorf("clicking the second container should connect to it; got %q", svc.connected)
	}
}

func TestPortForward(t *testing.T) {
	m, svc, rec := openTasks(t)
	send(m, press("p"))
	if m.view != viewPortForward {
		t.Fatal("p should open the port forward form")
	}
	for _, r := range "8080" {
		send(m, tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	out := send(m, press("enter"))
	st, ok := findStatus(out)
	if svc.forwarded != [2]int{8080, 8080} || len(rec.kinds) != 1 || rec.kinds[0] != session.KindPortForward || !ok || st.Error {
		t.Errorf("forwarded = %v, sessions = %v, status = %+v", svc.forwarded, rec.kinds, st)
	}
}

func TestConnectWithoutExecFails(t *testing.T) {
	m, _, rec := openTasks(t)
	noExec := single
	noExec.ExecEnabled = false
	m.tasks.SetItems([]domain.Task{noExec})
	out := send(m, press("p"))
	if st, ok := findStatus(out); !ok || !st.Error {
		t.Errorf("expected an error status, got %v", out)
	}
	if m.view == viewPortForward || len(rec.kinds) != 0 {
		t.Error("port forward must not open when ECS Exec is disabled")
	}
}

func TestTaskActionsMenuAndDetails(t *testing.T) {
	m, svc, _ := openTasks(t)
	send(m, press("enter"))
	if m.view != viewActions {
		t.Fatal("enter on a task should open its actions")
	}
	send(m, press("o"))
	if svc.consoled != "task/aaaaaaaa1111" {
		t.Errorf("consoled = %q", svc.consoled)
	}
	send(m, press("d"))
	if m.view != viewTaskDetails || !strings.Contains(m.View(), "api:1") {
		t.Errorf("d should show task details with containers:\n%s", m.View())
	}
	send(m, press("esc"))
	if m.view != viewList || m.level != levelTasks {
		t.Error("esc should return to the tasks list")
	}
}

func TestServiceDetailsAndConsole(t *testing.T) {
	m, svc, _ := newModel(t)
	send(m, press("enter"))
	send(m, press("o"))
	if svc.consoled != "service/api" {
		t.Errorf("consoled = %q", svc.consoled)
	}
	send(m, press("d"))
	if m.view != viewServiceDetails || !strings.Contains(m.View(), "service details") {
		t.Error("d should show service details")
	}
}

func TestStaleResultsAreIgnored(t *testing.T) {
	m, _, _ := newModel(t)
	send(m, press("enter")) // services of prod
	send(m, servicesLoadedMsg{cluster: "staging", items: []domain.Service{{Name: "ghost"}}})
	if strings.Contains(m.View(), "ghost") {
		t.Error("results for another cluster must be ignored")
	}
}

func TestLoadError(t *testing.T) {
	m, _, _ := newModel(t)
	out := send(m, clustersLoadedMsg{err: errors.New("AccessDenied")})
	if st, ok := findStatus(out); !ok || !st.Error || !strings.Contains(m.View(), "AccessDenied") {
		t.Error("load errors should be shown")
	}
}

func TestTaskDetailsExplainMissingContainerInsights(t *testing.T) {
	m, _, _ := openTasks(t)
	m.SetSize(120, 40) // task details are long; the hint is below a 20-row panel
	send(m, press("d"))
	if m.view != viewTaskDetails || !strings.Contains(m.View(), "Container Insights") {
		t.Errorf("empty task metrics should explain Container Insights:\n%s", m.View())
	}
	send(m, press("esc"))
	if m.view != viewList {
		t.Error("esc should leave the details")
	}
}

func TestServiceDetailsShowCharts(t *testing.T) {
	m, _, _ := newModel(t)
	send(m, press("enter"))
	send(m, press("d"))
	if view := m.View(); !strings.Contains(view, "service details") || !strings.Contains(view, "now 42%") {
		t.Errorf("service details should chart CPU:\n%s", view)
	}
}

func typeRune(r rune) tea.KeyPressMsg { return tea.KeyPressMsg{Code: r, Text: string(r)} }
