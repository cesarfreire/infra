package app

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"

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
	Now      func() time.Time   // defaults to time.Now
	Targets  TargetHealthReader // optional: load balancer health in rollouts
}

// ContainerService implements the use cases available for containers.
type ContainerService struct {
	now     func() time.Time
	targets TargetHealthReader

	defsMu   sync.Mutex
	defs     map[string]domain.TaskDefinition // revisions are immutable
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
	now := d.Now
	if now == nil {
		now = time.Now
	}
	return &ContainerService{repo: d.Repo, control: d.Control, sessions: d.Sessions, console: d.Console,
		opener: d.Opener, metrics: d.Metrics, policy: d.Policy, now: now, targets: d.Targets,
		defs: map[string]domain.TaskDefinition{}}
}

// GetService reads a service again, e.g. to follow a rollout.
func (s *ContainerService) GetService(ctx context.Context, cluster, name string) (domain.Service, error) {
	svc, err := s.repo.GetService(ctx, cluster, name)
	if err != nil {
		return domain.Service{}, fmt.Errorf("reading service %s: %w", name, err)
	}
	return svc, nil
}

// RolloutStoppedWindow is how far back stopped tasks are shown when
// following a deployment.
const RolloutStoppedWindow = 30 * time.Minute

// Rollout reads a service and its tasks to follow a deployment. Stopped
// tasks are kept only if they stopped within RolloutStoppedWindow; the
// rest are ordered by deployment, then state, then start time.
func (s *ContainerService) Rollout(ctx context.Context, svc domain.Service) (domain.Rollout, error) {
	var r domain.Rollout
	g, gctx := errgroup.WithContext(ctx)
	g.Go(func() error {
		var err error
		r.Service, err = s.repo.GetService(gctx, svc.Cluster, svc.Name)
		return err
	})
	g.Go(func() error {
		var err error
		r.Tasks, err = s.repo.ListDeploymentTasks(gctx, svc.Cluster, svc.Name)
		return err
	})
	if err := g.Wait(); err != nil {
		return domain.Rollout{}, fmt.Errorf("reading the rollout of %s: %w", svc.Name, err)
	}
	s.addExtras(ctx, &r)
	cutoff := s.now().Add(-RolloutStoppedWindow)
	r.Tasks = slices.DeleteFunc(r.Tasks, func(t domain.Task) bool {
		return t.LastStatus == "STOPPED" && !t.StoppedAt.IsZero() && t.StoppedAt.Before(cutoff)
	})
	slices.SortStableFunc(r.Tasks, func(a, b domain.Task) int {
		return cmp.Or(cmp.Compare(taskRank(a), taskRank(b)), b.StartedAt.Compare(a.StartedAt), cmp.Compare(a.ID, b.ID))
	})
	return r, nil
}

// addExtras reads the target health and task definitions of a rollout.
// They are extras: a failure becomes a problem shown with the snapshot.
func (s *ContainerService) addExtras(ctx context.Context, r *domain.Rollout) {
	var mu sync.Mutex
	problem := func(what string, err error) {
		mu.Lock()
		defer mu.Unlock()
		r.Problems = append(r.Problems, fmt.Sprintf("%s: %v", what, err))
	}
	var wg sync.WaitGroup
	if s.targets != nil {
		r.TargetGroups = make([]domain.TargetGroup, len(r.Service.TargetGroups))
		for i, arn := range r.Service.TargetGroups {
			wg.Go(func() {
				targets, err := s.targets.TargetHealth(ctx, arn)
				if err != nil {
					problem("target health", err)
				}
				r.TargetGroups[i] = domain.TargetGroup{Name: targetGroupName(arn), ARN: arn, Targets: targets}
			})
		}
	}
	r.Definitions = map[string]domain.TaskDefinition{}
	for _, d := range r.Service.Deployments {
		wg.Go(func() {
			def, err := s.TaskDefinition(ctx, d.TaskDefinition)
			if err != nil {
				problem("task definition "+d.TaskDefinition, err)
				return
			}
			mu.Lock()
			r.Definitions[d.TaskDefinition] = def
			mu.Unlock()
		})
	}
	wg.Wait()
	slices.Sort(r.Problems)
	r.Problems = slices.Compact(r.Problems)
}

// targetGroupName extracts the name from ".../targetgroup/<name>/<id>".
func targetGroupName(arn string) string {
	parts := strings.Split(arn, "/")
	if len(parts) >= 3 {
		return parts[len(parts)-2]
	}
	return arn
}

// TaskDefinition reads a revision ("family:revision"), cached because
// revisions never change.
func (s *ContainerService) TaskDefinition(ctx context.Context, name string) (domain.TaskDefinition, error) {
	s.defsMu.Lock()
	def, ok := s.defs[name]
	s.defsMu.Unlock()
	if ok {
		return def, nil
	}
	def, err := s.repo.GetTaskDefinition(ctx, name)
	if err != nil {
		return domain.TaskDefinition{}, err
	}
	s.defsMu.Lock()
	s.defs[name] = def
	s.defsMu.Unlock()
	return def, nil
}

// RevisionChange is the task definition running now, the one before it,
// and what changed between them.
type RevisionChange struct {
	From    domain.TaskDefinition
	To      domain.TaskDefinition
	Changes []domain.Change
}

// PreviousRevision tells what a rollback would deploy: the task definition
// of an older deployment still in the service, if it differs from the
// primary one, else the primary's previous revision.
func (s *ContainerService) PreviousRevision(ctx context.Context, svc domain.Service) (RevisionChange, error) {
	primary := ""
	for _, d := range svc.Deployments {
		if d.Status == "PRIMARY" {
			primary = d.TaskDefinition
		}
	}
	if primary == "" {
		primary = svc.TaskDefinition
	}
	current, err := s.TaskDefinition(ctx, primary)
	if err != nil {
		return RevisionChange{}, fmt.Errorf("reading %s: %w", primary, err)
	}
	previous := ""
	for _, d := range svc.Deployments {
		if d.Status != "PRIMARY" && d.TaskDefinition != primary {
			previous = d.TaskDefinition
			break
		}
	}
	if previous == "" {
		previous = current.PreviousRevision()
	}
	if previous == "" {
		return RevisionChange{}, fmt.Errorf("%s is the first revision: %w", primary, domain.ErrInvalidState)
	}
	old, err := s.TaskDefinition(ctx, previous)
	if err != nil {
		return RevisionChange{}, fmt.Errorf("reading %s: %w", previous, err)
	}
	return RevisionChange{From: old, To: current, Changes: domain.DiffTaskDefinitions(old, current)}, nil
}

// Rollback deploys revision to the service.
func (s *ContainerService) Rollback(ctx context.Context, svc domain.Service, revision domain.TaskDefinition) error {
	if err := s.policy.CheckWrite("roll back " + svc.Name); err != nil {
		return err
	}
	if err := s.control.UpdateTaskDefinition(ctx, svc.Cluster, svc.Name, revision.Name()); err != nil {
		return fmt.Errorf("rolling back %s to %s: %w", svc.Name, revision.Name(), err)
	}
	return nil
}

// taskRank orders tasks: running first, then starting, then stopping, then stopped.
func taskRank(t domain.Task) int {
	switch {
	case t.LastStatus == "STOPPED":
		return 3
	case t.Stopping():
		return 2
	case t.LastStatus == "RUNNING":
		return 0
	}
	return 1
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
