package app

import (
	"cmp"
	"context"
	"fmt"
	"regexp"
	"slices"

	"infra/internal/domain"
)

// CacheService implements the use cases available for in-memory caches.
// Caches live inside a VPC, so sessions go through a jump host: a running
// instance with the SSM agent that can reach the cache endpoint.
type CacheService struct {
	repo      CacheRepository
	instances InstanceRepository
	sessions  JumpHostSessionBuilder
	console   CacheConsoleURLBuilder
	opener    URLOpener
	metrics   CacheMetricsSource
}

// CacheDeps are the adapters the cache use cases need.
type CacheDeps struct {
	Repo      CacheRepository
	Instances InstanceRepository
	Sessions  JumpHostSessionBuilder
	Console   CacheConsoleURLBuilder
	Opener    URLOpener
	Metrics   CacheMetricsSource
}

// NewCacheService wires the cache use cases with their adapters.
func NewCacheService(d CacheDeps) *CacheService {
	return &CacheService{repo: d.Repo, instances: d.Instances, sessions: d.Sessions, console: d.Console,
		opener: d.Opener, metrics: d.Metrics}
}

// Metrics returns the monitoring series of a cache over w.
func (s *CacheService) Metrics(ctx context.Context, c domain.Cache, w domain.MetricWindow) ([]domain.Series, error) {
	series, err := s.metrics.CacheMetrics(ctx, c, w)
	if err != nil {
		return nil, fmt.Errorf("reading metrics of %s: %w", c.ID, err)
	}
	return series, nil
}

// ListCaches returns caches sorted by ID.
func (s *CacheService) ListCaches(ctx context.Context) ([]domain.Cache, error) {
	caches, err := s.repo.ListCaches(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing caches: %w", err)
	}
	slices.SortFunc(caches, func(a, b domain.Cache) int { return cmp.Compare(a.ID, b.ID) })
	return caches, nil
}

// JumpHosts returns the running instances, sorted by name, that can be
// used to reach a cache.
func (s *CacheService) JumpHosts(ctx context.Context) ([]domain.Instance, error) {
	all, err := s.instances.ListInstances(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing instances: %w", err)
	}
	running := slices.DeleteFunc(all, func(i domain.Instance) bool { return !i.IsRunning() })
	slices.SortFunc(running, func(a, b domain.Instance) int {
		return cmp.Or(cmp.Compare(a.DisplayName(), b.DisplayName()), cmp.Compare(a.ID, b.ID))
	})
	return running, nil
}

// PortForward returns the command that tunnels localPort to the cache
// endpoint through host.
func (s *CacheService) PortForward(c domain.Cache, host domain.Instance, localPort int) (Command, error) {
	if err := checkReachable(c, host); err != nil {
		return Command{}, err
	}
	pf := domain.PortForward{Target: host.ID, RemoteHost: c.Endpoint.Host, RemotePort: c.Endpoint.Port, LocalPort: localPort}
	if err := pf.Validate(); err != nil {
		return Command{}, err
	}
	return s.sessions.PortForwardSession(pf), nil
}

// Connect returns the command that opens the engine's CLI on host,
// connected to the cache. The CLI must be installed on the host.
func (s *CacheService) Connect(c domain.Cache, host domain.Instance) (Command, error) {
	if err := checkReachable(c, host); err != nil {
		return Command{}, err
	}
	cli, err := CLICommand(c)
	if err != nil {
		return Command{}, err
	}
	return s.sessions.RemoteCommandSession(host.ID, cli), nil
}

// OpenConsole opens the cache in the web console and returns the URL.
func (s *CacheService) OpenConsole(c domain.Cache) (string, error) {
	url := s.console.CacheURL(c)
	if err := s.opener.Open(url); err != nil {
		return url, fmt.Errorf("opening console: %w", err)
	}
	return url, nil
}

func checkReachable(c domain.Cache, host domain.Instance) error {
	if !c.IsAvailable() {
		return fmt.Errorf("cache %s is %s: %w", c.ID, c.Status, domain.ErrNotAvailable)
	}
	if c.Endpoint.Host == "" {
		return fmt.Errorf("cache %s has no endpoint: %w", c.ID, domain.ErrNotAvailable)
	}
	if !host.IsRunning() {
		return fmt.Errorf("jump host %s: %w", host.DisplayName(), domain.ErrInstanceNotRunning)
	}
	return nil
}

// hostname guards the shell command against unexpected endpoint values.
var hostname = regexp.MustCompile(`^[A-Za-z0-9.-]+$`)

// CLICommand is the shell command that connects to c with valkey-cli,
// falling back to redis-cli (they are compatible).
func CLICommand(c domain.Cache) (string, error) {
	if c.Engine == domain.EngineMemcached {
		return "", fmt.Errorf("CLI for memcached: %w; use port forward", domain.ErrNotSupported)
	}
	if !hostname.MatchString(c.Endpoint.Host) {
		return "", fmt.Errorf("invalid endpoint %q", c.Endpoint.Host)
	}
	args := fmt.Sprintf("-h %s -p %d", c.Endpoint.Host, c.Endpoint.Port)
	if c.TLS {
		args += " --tls"
	}
	if c.ClusterMode {
		args += " -c"
	}
	return fmt.Sprintf("command -v valkey-cli >/dev/null && exec valkey-cli %[1]s || exec redis-cli %[1]s", args), nil
}
