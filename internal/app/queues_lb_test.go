package app_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"infra/internal/app"
	"infra/internal/domain"
)

type fakeQueues struct{ peeked int }

func (f *fakeQueues) ListQueues(context.Context) ([]domain.Queue, error) {
	return []domain.Queue{{Name: "orders"}, {Name: "emails"}}, nil
}

func (f *fakeQueues) PeekMessages(_ context.Context, _ domain.Queue, max int) ([]domain.Message, error) {
	f.peeked = max
	now := time.Now()
	return []domain.Message{{ID: "new", SentAt: now}, {ID: "old", SentAt: now.Add(-time.Hour)}}, nil
}

func TestQueues(t *testing.T) {
	repo := &fakeQueues{}
	svc := app.NewQueueService(app.QueueDeps{Repo: repo})
	qs, _ := svc.List(context.Background())
	if qs[0].Name != "emails" {
		t.Errorf("not sorted: %v", qs)
	}
	msgs, err := svc.Peek(context.Background(), qs[0])
	if err != nil || repo.peeked != app.PeekLimit || msgs[0].ID != "old" {
		t.Errorf("Peek() = %v, %v (limit %d)", msgs, err, repo.peeked)
	}
	readOnly := app.NewQueueService(app.QueueDeps{Repo: repo, Policy: app.Policy{ReadOnly: true}})
	if _, err := readOnly.Peek(context.Background(), qs[0]); !errors.Is(err, domain.ErrReadOnly) {
		t.Errorf("peeking changes receive counts and must be blocked when read-only: %v", err)
	}
}

type fakeLBs struct{}

func (fakeLBs) ListLoadBalancers(context.Context) ([]domain.LoadBalancer, error) {
	return []domain.LoadBalancer{{Name: "web"}, {Name: "api"}}, nil
}

func (fakeLBs) ListListeners(context.Context, domain.LoadBalancer) ([]domain.Listener, error) {
	return []domain.Listener{{Port: 443}, {Port: 80}}, nil
}

func (fakeLBs) ListTargetGroups(context.Context, domain.LoadBalancer) ([]domain.TargetGroup, error) {
	return []domain.TargetGroup{{Name: "tg", Targets: []domain.Target{
		{ID: "a", State: "healthy"}, {ID: "b", State: "draining"}, {ID: "c", State: "unhealthy"},
	}}}, nil
}

func TestLoadBalancers(t *testing.T) {
	svc := app.NewLoadBalancerService(app.LoadBalancerDeps{Repo: fakeLBs{}})
	ctx := context.Background()
	lbs, _ := svc.List(ctx)
	ls, _ := svc.Listeners(ctx, lbs[0])
	groups, _ := svc.TargetGroups(ctx, lbs[0])
	if lbs[0].Name != "api" || ls[0].Port != 80 {
		t.Errorf("not sorted: %v %v", lbs, ls)
	}
	targets := groups[0].Targets
	if targets[0].ID != "c" || targets[1].ID != "b" || targets[2].ID != "a" || groups[0].Healthy() != 1 {
		t.Errorf("targets should be ordered unhealthy first: %v", targets)
	}
}
