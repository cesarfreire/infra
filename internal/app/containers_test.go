package app_test

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"infra/internal/app"
	"infra/internal/domain"
)

type fakeContainerRepo struct {
	clusters []domain.Cluster
	services []domain.Service
	tasks    []domain.Task
	err      error
	defs     map[string]domain.TaskDefinition
	defReads *int
	service  *domain.Service // returned by GetService when set
}

func (f fakeContainerRepo) GetTaskDefinition(_ context.Context, name string) (domain.TaskDefinition, error) {
	if f.defReads != nil {
		*f.defReads++
	}
	def, ok := f.defs[name]
	if !ok {
		return domain.TaskDefinition{}, errors.New("not found: " + name)
	}
	return def, nil
}

func (f fakeContainerRepo) ListClusters(context.Context) ([]domain.Cluster, error) {
	return f.clusters, f.err
}

func (f fakeContainerRepo) ListServices(context.Context, string) ([]domain.Service, error) {
	return f.services, f.err
}

func (f fakeContainerRepo) GetService(_ context.Context, cluster, name string) (domain.Service, error) {
	if f.service != nil {
		return *f.service, f.err
	}
	return domain.Service{Cluster: cluster, Name: name}, f.err
}

func (f fakeContainerRepo) ListDeploymentTasks(context.Context, string, string) ([]domain.Task, error) {
	return f.tasks, f.err
}

func (f fakeContainerRepo) ListTasks(context.Context, string, string) ([]domain.Task, error) {
	return f.tasks, f.err
}

type fakeContainerSessions struct{}

func (fakeContainerSessions) ContainerShellSession(t domain.Task, c domain.Container) app.Command {
	return app.Command{Name: "exec", Args: []string{t.ID, c.Name}}
}

func (fakeContainerSessions) ContainerPortForwardSession(t domain.Task, c domain.Container, _ domain.PortForward) app.Command {
	return app.Command{Name: "forward", Args: []string{t.ID, c.Name}}
}

type fakeContainerConsole struct{}

func (fakeContainerConsole) ClusterURL(c string) string    { return "cluster/" + c }
func (fakeContainerConsole) ServiceURL(c, s string) string { return "service/" + c + "/" + s }
func (fakeContainerConsole) TaskURL(c, t string) string    { return "task/" + c + "/" + t }

func newContainerService(repo fakeContainerRepo, opener *fakeOpener) *app.ContainerService {
	return app.NewContainerService(app.ContainerDeps{Repo: repo, Control: &fakeControl{}, Sessions: fakeContainerSessions{},
		Console: fakeContainerConsole{}, Opener: opener, Metrics: fakeMetrics{}})
}

var (
	appContainer = domain.Container{Name: "app", Status: "RUNNING"}
	runningTask  = domain.Task{ID: "t1", Cluster: "prod", LastStatus: "RUNNING", ExecEnabled: true, Containers: []domain.Container{appContainer}}
)

func TestListTasksSortsByServiceThenNewest(t *testing.T) {
	now := time.Now()
	svc := newContainerService(fakeContainerRepo{tasks: []domain.Task{
		{ID: "old", Service: "api", StartedAt: now.Add(-time.Hour)},
		{ID: "web", Service: "web", StartedAt: now},
		{ID: "new", Service: "api", StartedAt: now},
	}}, &fakeOpener{})
	got, err := svc.ListTasks(context.Background(), "prod", "")
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, task := range got {
		ids = append(ids, task.ID)
	}
	if want := []string{"new", "old", "web"}; !slices.Equal(ids, want) {
		t.Errorf("order = %v, want %v", ids, want)
	}
}

func TestContainerListErrorsAreWrapped(t *testing.T) {
	boom := errors.New("boom")
	svc := newContainerService(fakeContainerRepo{err: boom}, &fakeOpener{})
	if _, err := svc.ListClusters(context.Background()); !errors.Is(err, boom) {
		t.Errorf("ListClusters error = %v", err)
	}
	if _, err := svc.ListServices(context.Background(), "prod"); !errors.Is(err, boom) {
		t.Errorf("ListServices error = %v", err)
	}
}

func TestContainerConnect(t *testing.T) {
	svc := newContainerService(fakeContainerRepo{}, &fakeOpener{})
	if cmd, err := svc.Connect(runningTask, appContainer); err != nil || cmd.Name != "exec" {
		t.Errorf("Connect() = %+v, %v", cmd, err)
	}
	noExec := runningTask
	noExec.ExecEnabled = false
	if _, err := svc.Connect(noExec, appContainer); !errors.Is(err, domain.ErrExecNotEnabled) {
		t.Errorf("Connect(no exec) error = %v", err)
	}
}

func TestContainerPortForward(t *testing.T) {
	svc := newContainerService(fakeContainerRepo{}, &fakeOpener{})
	if cmd, err := svc.PortForward(runningTask, appContainer, 8080, 80); err != nil || cmd.Name != "forward" {
		t.Errorf("PortForward() = %+v, %v", cmd, err)
	}
	if _, err := svc.PortForward(runningTask, appContainer, 8080, 0); !errors.Is(err, domain.ErrInvalidPort) {
		t.Errorf("PortForward(bad port) error = %v", err)
	}
}

func TestContainerConsoles(t *testing.T) {
	opener := &fakeOpener{}
	svc := newContainerService(fakeContainerRepo{}, opener)
	svc.OpenClusterConsole(domain.Cluster{Name: "prod"})
	svc.OpenServiceConsole(domain.Service{Cluster: "prod", Name: "api"})
	svc.OpenTaskConsole(runningTask)
	want := []string{"cluster/prod", "service/prod/api", "task/prod/t1"}
	if !slices.Equal(opener.opened, want) {
		t.Errorf("opened = %v, want %v", opener.opened, want)
	}
}

func TestRolloutFiltersAndOrdersTasks(t *testing.T) {
	now := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	repo := fakeContainerRepo{tasks: []domain.Task{
		{ID: "stopped-long-ago", LastStatus: "STOPPED", DesiredStatus: "STOPPED", StoppedAt: now.Add(-2 * time.Hour)},
		{ID: "stopped", LastStatus: "STOPPED", DesiredStatus: "STOPPED", StoppedAt: now.Add(-5 * time.Minute)},
		{ID: "draining", LastStatus: "RUNNING", DesiredStatus: "STOPPED"},
		{ID: "pending", LastStatus: "PENDING", DesiredStatus: "RUNNING"},
		{ID: "old-running", LastStatus: "RUNNING", DesiredStatus: "RUNNING", StartedAt: now.Add(-time.Hour)},
		{ID: "new-running", LastStatus: "RUNNING", DesiredStatus: "RUNNING", StartedAt: now.Add(-time.Minute)},
	}}
	svc := app.NewContainerService(app.ContainerDeps{Repo: repo, Now: func() time.Time { return now }})
	r, err := svc.Rollout(context.Background(), domain.Service{Cluster: "prod", Name: "api"})
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, task := range r.Tasks {
		ids = append(ids, task.ID)
	}
	want := []string{"new-running", "old-running", "pending", "draining", "stopped"}
	if !slices.Equal(ids, want) {
		t.Errorf("tasks = %v, want %v", ids, want)
	}
	if r.Service.Name != "api" {
		t.Errorf("service = %+v", r.Service)
	}
}
