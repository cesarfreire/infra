package app

import (
	"cmp"
	"context"
	"fmt"
	"slices"

	"infra/internal/domain"
)

// InstanceDeps are the adapters the instance use cases need.
type InstanceDeps struct {
	Repo     InstanceRepository
	Control  InstanceController
	Sessions SessionCommandBuilder
	Console  ConsoleURLBuilder
	Opener   URLOpener
	Metrics  InstanceMetricsSource
	Policy   Policy
}

// InstanceService implements the use cases available for instances.
type InstanceService struct {
	repo     InstanceRepository
	control  InstanceController
	sessions SessionCommandBuilder
	console  ConsoleURLBuilder
	opener   URLOpener
	metrics  InstanceMetricsSource
	policy   Policy
}

// NewInstanceService wires the instance use cases with their adapters.
func NewInstanceService(d InstanceDeps) *InstanceService {
	return &InstanceService{repo: d.Repo, control: d.Control, sessions: d.Sessions, console: d.Console,
		opener: d.Opener, metrics: d.Metrics, policy: d.Policy}
}

// Start starts a stopped instance.
func (s *InstanceService) Start(ctx context.Context, inst domain.Instance) error {
	return s.change(ctx, "start", inst, domain.InstanceStateStopped, s.control.StartInstance)
}

// Stop stops a running instance.
func (s *InstanceService) Stop(ctx context.Context, inst domain.Instance) error {
	return s.change(ctx, "stop", inst, domain.InstanceStateRunning, s.control.StopInstance)
}

// Reboot reboots a running instance.
func (s *InstanceService) Reboot(ctx context.Context, inst domain.Instance) error {
	return s.change(ctx, "reboot", inst, domain.InstanceStateRunning, s.control.RebootInstance)
}

func (s *InstanceService) change(ctx context.Context, action string, inst domain.Instance, want domain.InstanceState, do func(context.Context, string) error) error {
	if err := s.policy.CheckWrite(action + " " + inst.DisplayName()); err != nil {
		return err
	}
	if inst.State != want {
		return fmt.Errorf("%s %s: it is %s: %w", action, inst.DisplayName(), inst.State, domain.ErrInvalidState)
	}
	if err := do(ctx, inst.ID); err != nil {
		return fmt.Errorf("%s %s: %w", action, inst.DisplayName(), err)
	}
	return nil
}

// Metrics returns the monitoring series of inst over w.
func (s *InstanceService) Metrics(ctx context.Context, inst domain.Instance, w domain.MetricWindow) ([]domain.Series, error) {
	series, err := s.metrics.InstanceMetrics(ctx, inst, w)
	if err != nil {
		return nil, fmt.Errorf("reading metrics of %s: %w", inst.DisplayName(), err)
	}
	return series, nil
}

// List returns all instances sorted by display name, then ID.
func (s *InstanceService) List(ctx context.Context) ([]domain.Instance, error) {
	instances, err := s.repo.ListInstances(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing instances: %w", err)
	}
	slices.SortStableFunc(instances, func(a, b domain.Instance) int {
		return cmp.Or(cmp.Compare(a.DisplayName(), b.DisplayName()), cmp.Compare(a.ID, b.ID))
	})
	return instances, nil
}

// Connect returns the command that opens an interactive shell on the instance.
func (s *InstanceService) Connect(inst domain.Instance) (Command, error) {
	if !inst.IsRunning() {
		return Command{}, fmt.Errorf("connect to %s: %w", inst.DisplayName(), domain.ErrInstanceNotRunning)
	}
	return s.sessions.ShellSession(inst.ID), nil
}

// PortForward returns the command that tunnels localPort to remotePort on the instance.
func (s *InstanceService) PortForward(inst domain.Instance, localPort, remotePort int) (Command, error) {
	if !inst.IsRunning() {
		return Command{}, fmt.Errorf("port forward to %s: %w", inst.DisplayName(), domain.ErrInstanceNotRunning)
	}
	pf := domain.PortForward{Target: inst.ID, LocalPort: localPort, RemotePort: remotePort}
	if err := pf.Validate(); err != nil {
		return Command{}, err
	}
	return s.sessions.PortForwardSession(pf), nil
}

// OpenConsole opens the instance page in the provider's web console and
// returns the URL that was opened.
func (s *InstanceService) OpenConsole(inst domain.Instance) (string, error) {
	url := s.console.InstanceURL(inst.ID)
	if err := s.opener.Open(url); err != nil {
		return url, fmt.Errorf("opening console: %w", err)
	}
	return url, nil
}
