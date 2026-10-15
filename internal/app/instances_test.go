package app_test

import (
	"context"
	"errors"
	"slices"
	"testing"

	"infra/internal/app"
	"infra/internal/domain"
)

type fakeRepo struct {
	instances []domain.Instance
	err       error
}

func (f fakeRepo) ListInstances(context.Context) ([]domain.Instance, error) {
	return f.instances, f.err
}

type fakeSessions struct{}

func (fakeSessions) ShellSession(id string) app.Command {
	return app.Command{Name: "shell", Args: []string{id}}
}

func (fakeSessions) PortForwardSession(pf domain.PortForward) app.Command {
	return app.Command{Name: "forward", Args: []string{pf.Target}}
}

type fakeConsole struct{}

func (fakeConsole) InstanceURL(id string) string { return "https://console/" + id }

type fakeOpener struct {
	opened []string
	err    error
}

func (f *fakeOpener) Open(url string) error {
	f.opened = append(f.opened, url)
	return f.err
}

// fakeMetrics implements every metrics source, returning one series named
// after the resource.
type fakeMetrics struct{ err error }

func (f fakeMetrics) series(name string) ([]domain.Series, error) {
	return []domain.Series{{Label: name}}, f.err
}

func (f fakeMetrics) InstanceMetrics(_ context.Context, i domain.Instance, _ domain.MetricWindow) ([]domain.Series, error) {
	return f.series(i.ID)
}

func (f fakeMetrics) ServiceMetrics(_ context.Context, s domain.Service, _ domain.MetricWindow) ([]domain.Series, error) {
	return f.series(s.Name)
}

func (f fakeMetrics) TaskMetrics(_ context.Context, t domain.Task, _ domain.MetricWindow) ([]domain.Series, error) {
	return f.series(t.ID)
}

func (f fakeMetrics) TableMetrics(_ context.Context, t domain.Table, _ domain.MetricWindow) ([]domain.Series, error) {
	return f.series(t.Name)
}

func (f fakeMetrics) CacheMetrics(_ context.Context, c domain.Cache, _ domain.MetricWindow) ([]domain.Series, error) {
	return f.series(c.ID)
}

func newService(repo fakeRepo, opener *fakeOpener) *app.InstanceService {
	return app.NewInstanceService(app.InstanceDeps{Repo: repo, Control: &fakeControl{}, Sessions: fakeSessions{},
		Console: fakeConsole{}, Opener: opener, Metrics: fakeMetrics{}})
}

var running = domain.Instance{ID: "i-1", Name: "web", State: domain.InstanceStateRunning}

func TestListSortsByDisplayName(t *testing.T) {
	svc := newService(fakeRepo{instances: []domain.Instance{
		{ID: "i-3", Name: "zeta"},
		{ID: "i-2"},
		{ID: "i-1", Name: "alpha"},
	}}, &fakeOpener{})

	got, err := svc.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, i := range got {
		ids = append(ids, i.ID)
	}
	if want := []string{"i-1", "i-2", "i-3"}; !slices.Equal(ids, want) {
		t.Errorf("List() order = %v, want %v", ids, want)
	}
}

func TestListWrapsRepositoryError(t *testing.T) {
	boom := errors.New("boom")
	_, err := newService(fakeRepo{err: boom}, &fakeOpener{}).List(context.Background())
	if !errors.Is(err, boom) {
		t.Errorf("List() error = %v, want wrapping %v", err, boom)
	}
}

func TestConnect(t *testing.T) {
	svc := newService(fakeRepo{}, &fakeOpener{})

	cmd, err := svc.Connect(running)
	if err != nil || cmd.Name != "shell" || cmd.Args[0] != "i-1" {
		t.Errorf("Connect(running) = %+v, %v", cmd, err)
	}

	stopped := running
	stopped.State = domain.InstanceStateStopped
	if _, err := svc.Connect(stopped); !errors.Is(err, domain.ErrInstanceNotRunning) {
		t.Errorf("Connect(stopped) error = %v, want ErrInstanceNotRunning", err)
	}
}

func TestPortForward(t *testing.T) {
	svc := newService(fakeRepo{}, &fakeOpener{})

	if cmd, err := svc.PortForward(running, 8080, 80); err != nil || cmd.Name != "forward" {
		t.Errorf("PortForward(valid) = %+v, %v", cmd, err)
	}
	if _, err := svc.PortForward(running, 8080, 0); !errors.Is(err, domain.ErrInvalidPort) {
		t.Errorf("PortForward(invalid port) error = %v, want ErrInvalidPort", err)
	}
}

func TestOpenConsole(t *testing.T) {
	opener := &fakeOpener{}
	url, err := newService(fakeRepo{}, opener).OpenConsole(running)
	if err != nil {
		t.Fatal(err)
	}
	if url != "https://console/i-1" || !slices.Equal(opener.opened, []string{url}) {
		t.Errorf("OpenConsole() url = %q, opened = %v", url, opener.opened)
	}
}
