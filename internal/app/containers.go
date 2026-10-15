package app

import (
	"cmp"
	"context"
	"fmt"
	"slices"

	"infra/internal/domain"
)

// ContainerDeps are the adapters the container use cases need.
type ContainerDeps struct {
	Repo     ContainerRepository
	Control  ServiceController
	Sessions ContainerSessionBuilder
	Console  ContainerConsoleURLBuilder
	Opener   URLOpener
	Metrics  ContainerMetricsSource
	Policy   Policy
}

// ContainerService implements the use cases available for containers.
type ContainerService struct {
	repo     ContainerRepository
	control  ServiceController
	sessions ContainerSessionBuilder
	console  ContainerConsoleURLBuilder
	opener   URLOpener
	metrics  ContainerMetricsSource
	policy   Policy
}

// NewContainerService wires the container use cases with their adapters.
func NewContainerService(d ContainerDeps) *ContainerService {
	return &ContainerService{repo: d.Repo, control: d.Control, sessions: d.Sessions, console: d.Console,
		opener: d.Opener, metrics: d.Metrics, policy: d.Policy}
}

// GetService reads a service again, e.g. to follow a rollout.
func (s *ContainerService) GetService(ctx context.Context, cluster, name string) (domain.Service, error) {
	svc, err := s.repo.GetService(ctx, cluster, name)
	if err != nil {
		return domain.Service{}, fmt.Errorf("reading service %s: %w", name, err)
	}
	return svc, nil
}

// ForceDeployment starts a new deployment that replaces every task.
func (s *ContainerService) ForceDeployment(ctx context.Context, svc domain.Service) error {
	if err := s.policy.CheckWrite("deploy " + svc.Name); err != nil {
		return err
	}
	if err := s.control.ForceNewDeployment(ctx, svc.Cluster, svc.Name); err != nil {
		return fmt.Errorf("deploying %s: %w", svc.Name, err)
	}
	return nil
}

// MaxDesiredCount guards against typos when scaling.
const MaxDesiredCount = 1000

// SetDesiredCount scales a service.
func (s *ContainerService) SetDesiredCount(ctx context.Context, svc domain.Service, count int) error {
	if err := s.policy.CheckWrite("scale " + svc.Name); err != nil {
		return err
	}
	if count < 0 || count > MaxDesiredCount {
		return fmt.Errorf("desired count must be between 0 and %d", MaxDesiredCount)
	}
	if err := s.control.UpdateDesiredCount(ctx, svc.Cluster, svc.Name, count); err != nil {
		return fmt.Errorf("scaling %s: %w", svc.Name, err)
	}
	return nil
}

// ServiceMetrics returns the monitoring series of a service over w.
func (s *ContainerService) ServiceMetrics(ctx context.Context, svc domain.Service, w domain.MetricWindow) ([]domain.Series, error) {
	series, err := s.metrics.ServiceMetrics(ctx, svc, w)
	if err != nil {
		return nil, fmt.Errorf("reading metrics of %s: %w", svc.Name, err)
	}
	return series, nil
}

// TaskMetrics returns the monitoring series of a task over w.
func (s *ContainerService) TaskMetrics(ctx context.Context, t domain.Task, w domain.MetricWindow) ([]domain.Series, error) {
	series, err := s.metrics.TaskMetrics(ctx, t, w)
	if err != nil {
		return nil, fmt.Errorf("reading metrics of task %s: %w", t.ID, err)
	}
	return series, nil
}

// ListClusters returns clusters sorted by name.
func (s *ContainerService) ListClusters(ctx context.Context) ([]domain.Cluster, error) {
	clusters, err := s.repo.ListClusters(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing clusters: %w", err)
	}
	slices.SortFunc(clusters, func(a, b domain.Cluster) int { return cmp.Compare(a.Name, b.Name) })
	return clusters, nil
}

// ListServices returns the services of cluster sorted by name.
func (s *ContainerService) ListServices(ctx context.Context, cluster string) ([]domain.Service, error) {
	services, err := s.repo.ListServices(ctx, cluster)
	if err != nil {
		return nil, fmt.Errorf("listing services of %s: %w", cluster, err)
	}
	slices.SortFunc(services, func(a, b domain.Service) int { return cmp.Compare(a.Name, b.Name) })
	return services, nil
}

// ListTasks returns the running tasks of a service (or of the whole cluster
// when service is empty), sorted by service, then most recent first.
func (s *ContainerService) ListTasks(ctx context.Context, cluster, service string) ([]domain.Task, error) {
	tasks, err := s.repo.ListTasks(ctx, cluster, service)
	if err != nil {
		return nil, fmt.Errorf("listing tasks of %s: %w", cluster, err)
	}
	slices.SortFunc(tasks, func(a, b domain.Task) int {
		return cmp.Or(cmp.Compare(a.Service, b.Service), b.StartedAt.Compare(a.StartedAt), cmp.Compare(a.ID, b.ID))
	})
	return tasks, nil
}

// Connect returns the command that opens an interactive shell in container.
func (s *ContainerService) Connect(task domain.Task, container domain.Container) (Command, error) {
	if err := task.CanExec(container); err != nil {
		return Command{}, err
	}
	return s.sessions.ContainerShellSession(task, container), nil
}

// PortForward returns the command that tunnels localPort to remotePort in container.
func (s *ContainerService) PortForward(task domain.Task, container domain.Container, localPort, remotePort int) (Command, error) {
	if err := task.CanExec(container); err != nil {
		return Command{}, err
	}
	pf := domain.PortForward{LocalPort: localPort, RemotePort: remotePort}
	if err := pf.Validate(); err != nil {
		return Command{}, err
	}
	return s.sessions.ContainerPortForwardSession(task, container, pf), nil
}

// OpenClusterConsole opens the cluster page in the web console.
func (s *ContainerService) OpenClusterConsole(c domain.Cluster) (string, error) {
	return s.open(s.console.ClusterURL(c.Name))
}

// OpenServiceConsole opens the service page in the web console.
func (s *ContainerService) OpenServiceConsole(svc domain.Service) (string, error) {
	return s.open(s.console.ServiceURL(svc.Cluster, svc.Name))
}

// OpenTaskConsole opens the task page in the web console.
func (s *ContainerService) OpenTaskConsole(t domain.Task) (string, error) {
	return s.open(s.console.TaskURL(t.Cluster, t.ID))
}

func (s *ContainerService) open(url string) (string, error) {
	if err := s.opener.Open(url); err != nil {
		return url, fmt.Errorf("opening console: %w", err)
	}
	return url, nil
}
