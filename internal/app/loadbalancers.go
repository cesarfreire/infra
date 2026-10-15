package app

import (
	"cmp"
	"context"
	"fmt"
	"slices"

	"infra/internal/domain"
)

// LoadBalancerDeps are the adapters the load balancer use cases need.
type LoadBalancerDeps struct {
	Repo    LoadBalancerRepository
	Console LoadBalancerConsoleURLBuilder
	Opener  URLOpener
	Metrics LoadBalancerMetricsSource
}

// LoadBalancerService implements the use cases available for load balancers.
type LoadBalancerService struct {
	d LoadBalancerDeps
}

// NewLoadBalancerService wires the load balancer use cases with their adapters.
func NewLoadBalancerService(d LoadBalancerDeps) *LoadBalancerService {
	return &LoadBalancerService{d: d}
}

// List returns the load balancers sorted by name.
func (s *LoadBalancerService) List(ctx context.Context) ([]domain.LoadBalancer, error) {
	lbs, err := s.d.Repo.ListLoadBalancers(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing load balancers: %w", err)
	}
	slices.SortFunc(lbs, func(a, b domain.LoadBalancer) int { return cmp.Compare(a.Name, b.Name) })
	return lbs, nil
}

// Listeners returns the listeners of lb sorted by port.
func (s *LoadBalancerService) Listeners(ctx context.Context, lb domain.LoadBalancer) ([]domain.Listener, error) {
	ls, err := s.d.Repo.ListListeners(ctx, lb)
	if err != nil {
		return nil, fmt.Errorf("listing listeners of %s: %w", lb.Name, err)
	}
	slices.SortFunc(ls, func(a, b domain.Listener) int { return cmp.Compare(a.Port, b.Port) })
	return ls, nil
}

// TargetGroups returns the target groups of lb sorted by name, with their
// targets ordered unhealthy first.
func (s *LoadBalancerService) TargetGroups(ctx context.Context, lb domain.LoadBalancer) ([]domain.TargetGroup, error) {
	groups, err := s.d.Repo.ListTargetGroups(ctx, lb)
	if err != nil {
		return nil, fmt.Errorf("listing target groups of %s: %w", lb.Name, err)
	}
	slices.SortFunc(groups, func(a, b domain.TargetGroup) int { return cmp.Compare(a.Name, b.Name) })
	for _, g := range groups {
		slices.SortStableFunc(g.Targets, func(a, b domain.Target) int {
			return cmp.Or(cmp.Compare(healthRank(a.State), healthRank(b.State)), cmp.Compare(a.ID, b.ID))
		})
	}
	return groups, nil
}

// healthRank puts the targets needing attention first.
func healthRank(state string) int {
	switch state {
	case "unhealthy":
		return 0
	case "healthy":
		return 3
	case "unused":
		return 2
	}
	return 1 // initial, draining, unavailable...
}

// Metrics returns the monitoring series of lb over w.
func (s *LoadBalancerService) Metrics(ctx context.Context, lb domain.LoadBalancer, w domain.MetricWindow) ([]domain.Series, error) {
	series, err := s.d.Metrics.LoadBalancerMetrics(ctx, lb, w)
	if err != nil {
		return nil, fmt.Errorf("reading metrics of %s: %w", lb.Name, err)
	}
	return series, nil
}

// OpenConsole opens the load balancer in the web console and returns the URL.
func (s *LoadBalancerService) OpenConsole(lb domain.LoadBalancer) (string, error) {
	return open(s.d.Opener, s.d.Console.LoadBalancerURL(lb))
}
