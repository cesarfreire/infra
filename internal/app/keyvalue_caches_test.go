package app_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"infra/internal/app"
	"infra/internal/domain"
)

type fakeKV struct {
	queried domain.KeyQuery
	limit   int
}

func (f *fakeKV) ListTables(context.Context) ([]domain.Table, error) {
	return []domain.Table{{Name: "users"}, {Name: "orders"}}, nil
}

func (f *fakeKV) Scan(_ context.Context, _ domain.Table, _ domain.Cursor, limit int) (domain.ItemPage, error) {
	f.limit = limit
	return domain.ItemPage{Items: []domain.Item{{"id": "1"}}}, nil
}

func (f *fakeKV) Query(_ context.Context, _ domain.Table, q domain.KeyQuery, _ domain.Cursor, _ int) (domain.ItemPage, error) {
	f.queried = q
	return domain.ItemPage{}, nil
}

type fakeTableConsole struct{}

func (fakeTableConsole) TableURL(t string) string { return "table/" + t }

var (
	pkOnly = domain.Table{Name: "users", PartitionKey: domain.KeyAttribute{Name: "id", Type: "S"}}
	numPK  = domain.Table{Name: "events", PartitionKey: domain.KeyAttribute{Name: "ts", Type: "N"},
		SortKey: &domain.KeyAttribute{Name: "seq", Type: "N"}}
	strSK = domain.Table{Name: "orders", PartitionKey: domain.KeyAttribute{Name: "customer", Type: "S"},
		SortKey: &domain.KeyAttribute{Name: "date", Type: "S"}}
)

func TestKeyValueListAndScan(t *testing.T) {
	repo := &fakeKV{}
	svc := app.NewKeyValueService(app.KeyValueDeps{Repo: repo, Console: fakeTableConsole{}, Opener: &fakeOpener{}, Metrics: fakeMetrics{}})
	tables, _ := svc.ListTables(context.Background())
	if tables[0].Name != "orders" {
		t.Errorf("tables not sorted: %v", tables)
	}
	if _, err := svc.Scan(context.Background(), pkOnly, nil); err != nil || repo.limit != app.ItemPageSize {
		t.Errorf("Scan() err = %v, limit = %d", err, repo.limit)
	}
}

func TestValidateKeyQuery(t *testing.T) {
	tests := []struct {
		name    string
		table   domain.Table
		q       domain.KeyQuery
		wantErr string
	}{
		{"ok", strSK, domain.KeyQuery{PartitionValue: "c1", SortKeyPrefix: "2026-"}, ""},
		{"missing pk", pkOnly, domain.KeyQuery{}, "required"},
		{"numeric pk", numPK, domain.KeyQuery{PartitionValue: "abc"}, "number"},
		{"numeric pk ok", numPK, domain.KeyQuery{PartitionValue: "12.5"}, ""},
		{"prefix without sk", pkOnly, domain.KeyQuery{PartitionValue: "x", SortKeyPrefix: "a"}, "no sort key"},
		{"prefix on numeric sk", numPK, domain.KeyQuery{PartitionValue: "1", SortKeyPrefix: "a"}, "string sort key"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := app.ValidateKeyQuery(tt.table, tt.q)
			if (err == nil) != (tt.wantErr == "") || (err != nil && !strings.Contains(err.Error(), tt.wantErr)) {
				t.Errorf("ValidateKeyQuery() = %v, want error containing %q", err, tt.wantErr)
			}
		})
	}
}

type fakeCacheRepo struct{}

func (fakeCacheRepo) ListCaches(context.Context) ([]domain.Cache, error) {
	return []domain.Cache{{ID: "b"}, {ID: "a"}}, nil
}

type fakeJumpSessions struct{ pf domain.PortForward }

func (f *fakeJumpSessions) PortForwardSession(pf domain.PortForward) app.Command {
	f.pf = pf
	return app.Command{Name: "forward"}
}

func (f *fakeJumpSessions) RemoteCommandSession(id, command string) app.Command {
	return app.Command{Name: "remote", Args: []string{id, command}}
}

type fakeCacheConsole struct{}

func (fakeCacheConsole) CacheURL(c domain.Cache) string { return "cache/" + c.ID }

var redis = domain.Cache{ID: "sessions", Engine: domain.EngineRedis, Status: "available", TLS: true, ClusterMode: true,
	Endpoint: domain.Endpoint{Host: "clustercfg.sessions.abc.use1.cache.amazonaws.com", Port: 6379}}

func newCacheService(sessions *fakeJumpSessions) *app.CacheService {
	instances := fakeRepo{instances: []domain.Instance{
		{ID: "i-2", Name: "zeta", State: domain.InstanceStateRunning},
		{ID: "i-1", Name: "bastion", State: domain.InstanceStateRunning},
		{ID: "i-3", Name: "off", State: domain.InstanceStateStopped},
	}}
	return app.NewCacheService(app.CacheDeps{Repo: fakeCacheRepo{}, Instances: instances, Sessions: sessions,
		Console: fakeCacheConsole{}, Opener: &fakeOpener{}, Metrics: fakeMetrics{}})
}

func TestJumpHostsAreRunningAndSorted(t *testing.T) {
	hosts, err := newCacheService(&fakeJumpSessions{}).JumpHosts(context.Background())
	if err != nil || len(hosts) != 2 || hosts[0].Name != "bastion" {
		t.Errorf("JumpHosts() = %v, %v", hosts, err)
	}
}

func TestCachePortForwardGoesThroughJumpHost(t *testing.T) {
	sessions := &fakeJumpSessions{}
	host := domain.Instance{ID: "i-1", State: domain.InstanceStateRunning}
	if _, err := newCacheService(sessions).PortForward(redis, host, 16379); err != nil {
		t.Fatal(err)
	}
	want := domain.PortForward{Target: "i-1", RemoteHost: redis.Endpoint.Host, RemotePort: 6379, LocalPort: 16379}
	if sessions.pf != want {
		t.Errorf("pf = %+v, want %+v", sessions.pf, want)
	}
}

func TestCacheConnect(t *testing.T) {
	svc := newCacheService(&fakeJumpSessions{})
	host := domain.Instance{ID: "i-1", State: domain.InstanceStateRunning}

	cmd, err := svc.Connect(redis, host)
	if err != nil {
		t.Fatal(err)
	}
	if cli := cmd.Args[1]; !strings.Contains(cli, "valkey-cli -h "+redis.Endpoint.Host+" -p 6379 --tls -c") || !strings.Contains(cli, "redis-cli") {
		t.Errorf("unexpected CLI command %q", cli)
	}

	memcached := redis
	memcached.Engine = domain.EngineMemcached
	if _, err := svc.Connect(memcached, host); !errors.Is(err, domain.ErrNotSupported) {
		t.Errorf("memcached CLI error = %v", err)
	}
	creating := redis
	creating.Status = "creating"
	if _, err := svc.Connect(creating, host); !errors.Is(err, domain.ErrNotAvailable) {
		t.Errorf("unavailable cache error = %v", err)
	}
	stopped := host
	stopped.State = domain.InstanceStateStopped
	if _, err := svc.Connect(redis, stopped); !errors.Is(err, domain.ErrInstanceNotRunning) {
		t.Errorf("stopped jump host error = %v", err)
	}
	evil := redis
	evil.Endpoint.Host = "x; rm -rf /"
	if _, err := svc.Connect(evil, host); err == nil {
		t.Error("an endpoint with shell characters must be rejected")
	}
}

func TestMetricsAreDelegatedAndErrorsWrapped(t *testing.T) {
	ctx, w := context.Background(), app.MetricWindows[app.DefaultMetricWindow]
	boom := errors.New("boom")

	inst := app.NewInstanceService(app.InstanceDeps{Repo: fakeRepo{}, Sessions: fakeSessions{}, Console: fakeConsole{}, Opener: &fakeOpener{}, Metrics: fakeMetrics{}})
	if s, err := inst.Metrics(ctx, domain.Instance{ID: "i-1"}, w); err != nil || s[0].Label != "i-1" {
		t.Errorf("instance metrics = %v, %v", s, err)
	}
	cont := app.NewContainerService(app.ContainerDeps{Repo: fakeContainerRepo{}, Sessions: fakeContainerSessions{}, Console: fakeContainerConsole{}, Opener: &fakeOpener{}, Metrics: fakeMetrics{err: boom}})
	if _, err := cont.TaskMetrics(ctx, domain.Task{ID: "t1"}, w); !errors.Is(err, boom) {
		t.Errorf("task metrics error = %v", err)
	}
	kv := app.NewKeyValueService(app.KeyValueDeps{Repo: &fakeKV{}, Console: fakeTableConsole{}, Opener: &fakeOpener{}, Metrics: fakeMetrics{}})
	if s, _ := kv.Metrics(ctx, domain.Table{Name: "users"}, w); s[0].Label != "users" {
		t.Errorf("table metrics = %v", s)
	}
	caches := app.NewCacheService(app.CacheDeps{Repo: fakeCacheRepo{}, Instances: fakeRepo{}, Sessions: &fakeJumpSessions{}, Console: fakeCacheConsole{}, Opener: &fakeOpener{}, Metrics: fakeMetrics{}})
	if s, _ := caches.Metrics(ctx, domain.Cache{ID: "c1"}, w); s[0].Label != "c1" {
		t.Errorf("cache metrics = %v", s)
	}
}
