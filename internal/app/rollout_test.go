package app_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"infra/internal/app"
	"infra/internal/domain"
)

func def(family string, rev int, image string) domain.TaskDefinition {
	return domain.TaskDefinition{Family: family, Revision: rev, Containers: []domain.ContainerDefinition{{Name: "app", Image: image}}}
}

var defs = map[string]domain.TaskDefinition{
	"api:6": def("api", 6, "repo/api:v40"),
	"api:7": def("api", 7, "repo/api:v41"),
	"api:8": def("api", 8, "repo/api:v42"),
	"api:1": def("api", 1, "repo/api:v1"),
}

func TestPreviousRevision(t *testing.T) {
	tests := []struct {
		name        string
		deployments []domain.Deployment
		want        string
	}{
		{"older deployment still draining", []domain.Deployment{
			{Status: "PRIMARY", TaskDefinition: "api:8"}, {Status: "ACTIVE", TaskDefinition: "api:6"}}, "api:6"},
		{"steady: revision before", []domain.Deployment{{Status: "PRIMARY", TaskDefinition: "api:8"}}, "api:7"},
		{"forced redeploy of the same revision", []domain.Deployment{
			{Status: "PRIMARY", TaskDefinition: "api:8"}, {Status: "ACTIVE", TaskDefinition: "api:8"}}, "api:7"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := app.NewContainerService(app.ContainerDeps{Repo: fakeContainerRepo{defs: defs}})
			rc, err := svc.PreviousRevision(context.Background(), domain.Service{Deployments: tt.deployments})
			if err != nil || rc.From.Name() != tt.want || rc.To.Name() != "api:8" {
				t.Fatalf("PreviousRevision() = %s → %s, %v; want %s", rc.From.Name(), rc.To.Name(), err, tt.want)
			}
			if len(rc.Changes) == 0 || rc.Changes[0].Field != "app.image" {
				t.Errorf("changes = %v", rc.Changes)
			}
		})
	}
	svc := app.NewContainerService(app.ContainerDeps{Repo: fakeContainerRepo{defs: defs}})
	_, err := svc.PreviousRevision(context.Background(), domain.Service{Deployments: []domain.Deployment{{Status: "PRIMARY", TaskDefinition: "api:1"}}})
	if !errors.Is(err, domain.ErrInvalidState) {
		t.Errorf("the first revision has no previous one: %v", err)
	}
}

func TestTaskDefinitionsAreCached(t *testing.T) {
	reads := 0
	svc := app.NewContainerService(app.ContainerDeps{Repo: fakeContainerRepo{defs: defs, defReads: &reads}})
	for range 3 {
		svc.TaskDefinition(context.Background(), "api:8")
	}
	if reads != 1 {
		t.Errorf("reads = %d, want 1", reads)
	}
}

func TestRollback(t *testing.T) {
	ctrl := &fakeControl{}
	svc := app.NewContainerService(app.ContainerDeps{Repo: fakeContainerRepo{}, Control: ctrl})
	api := domain.Service{Cluster: "prod", Name: "api"}
	if err := svc.Rollback(context.Background(), api, defs["api:7"]); err != nil {
		t.Fatal(err)
	}
	if strings.Join(ctrl.calls, ",") != "rollback prod/api to api:7" {
		t.Errorf("calls = %v", ctrl.calls)
	}
	readOnly := app.NewContainerService(app.ContainerDeps{Control: ctrl, Policy: app.Policy{ReadOnly: true}})
	if err := readOnly.Rollback(context.Background(), api, defs["api:7"]); !errors.Is(err, domain.ErrReadOnly) {
		t.Errorf("read-only rollback: %v", err)
	}
}

type fakeTargets struct{ err error }

func (f fakeTargets) TargetHealth(_ context.Context, arn string) ([]domain.Target, error) {
	return []domain.Target{{ID: "10.0.0.5", State: "healthy"}}, f.err
}

func TestRolloutExtras(t *testing.T) {
	service := domain.Service{Name: "api", Cluster: "prod",
		TargetGroups: []string{"arn:aws:elasticloadbalancing:r:1:targetgroup/api-tg/abc"},
		Deployments:  []domain.Deployment{{Status: "PRIMARY", TaskDefinition: "api:8"}, {Status: "ACTIVE", TaskDefinition: "api:404"}},
	}
	repo := fakeContainerRepo{defs: defs, service: &service}
	svc := app.NewContainerService(app.ContainerDeps{Repo: repo, Targets: fakeTargets{}})
	r, err := svc.Rollout(context.Background(), service)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.TargetGroups) != 1 || r.TargetGroups[0].Name != "api-tg" || r.TargetGroups[0].Targets[0].State != "healthy" {
		t.Errorf("target groups = %+v", r.TargetGroups)
	}
	if _, ok := r.Definitions["api:8"]; !ok {
		t.Error("the primary definition should be read")
	}
	if len(r.Problems) != 1 || !strings.Contains(r.Problems[0], "api:404") {
		t.Errorf("a missing definition is a problem, not a failure: %v", r.Problems)
	}

	denied := app.NewContainerService(app.ContainerDeps{Repo: repo, Targets: fakeTargets{err: domain.ErrAccessDenied}})
	if r, err := denied.Rollout(context.Background(), service); err != nil || !strings.Contains(strings.Join(r.Problems, " "), "target health") {
		t.Errorf("target health errors become problems: %v, %v", r.Problems, err)
	}
}
