package domain_test

import (
	"fmt"
	"testing"

	"infra/internal/domain"
)

func TestDiffTaskDefinitions(t *testing.T) {
	old := domain.TaskDefinition{Family: "api", Revision: 7, CPU: "512", Memory: "1024", Containers: []domain.ContainerDefinition{
		{Name: "app", Image: "repo/api:v41", Env: map[string]string{"LOG": "info", "OLD": "x"}, Secrets: map[string]string{"DB": "arn:1"}, Ports: []int{8080}},
		{Name: "envoy", Image: "envoy:1"},
	}}
	new := domain.TaskDefinition{Family: "api", Revision: 8, CPU: "512", Memory: "2048", Containers: []domain.ContainerDefinition{
		{Name: "app", Image: "repo/api:v42", Env: map[string]string{"LOG": "debug", "NEW": "y"}, Secrets: map[string]string{"DB": "arn:2"}, Ports: []int{8080}},
		{Name: "otel", Image: "otel:2"},
	}}
	got := fmt.Sprint(domain.DiffTaskDefinitions(old, new))
	want := "[{app.env.LOG info debug} {app.env.NEW  y} {app.env.OLD x } {app.image repo/api:v41 repo/api:v42} " +
		"{app.secret.DB arn:1 arn:2} {envoy removed (envoy:1) } {memory 1024 2048} {otel  added (otel:2)}]"
	if got != want {
		t.Errorf("diff =\n%s\nwant\n%s", got, want)
	}
	if len(domain.DiffTaskDefinitions(old, old)) != 0 {
		t.Error("a definition has no diff with itself")
	}
}

func TestTaskDefinitionNames(t *testing.T) {
	d := domain.TaskDefinition{Family: "api", Revision: 8}
	if d.Name() != "api:8" || d.PreviousRevision() != "api:7" {
		t.Errorf("Name() = %s, PreviousRevision() = %s", d.Name(), d.PreviousRevision())
	}
	if (domain.TaskDefinition{Family: "api", Revision: 1}).PreviousRevision() != "" {
		t.Error("revision 1 has no previous one")
	}
	if f, r, ok := domain.ParseTaskDefinitionName("api:12"); !ok || f != "api" || r != 12 {
		t.Errorf("parse = %s %d %v", f, r, ok)
	}
	if _, _, ok := domain.ParseTaskDefinitionName("api"); ok {
		t.Error("a name without revision is not valid")
	}
}

func TestRolloutTargetAndImage(t *testing.T) {
	d := domain.Deployment{ID: "ecs-svc/1", TaskDefinition: "api:8"}
	task := domain.Task{StartedBy: "ecs-svc/1", PrivateIP: "10.0.0.5",
		Containers: []domain.Container{{Name: "app", ImageDigest: "sha256:abc"}}}
	r := domain.Rollout{
		Service:      domain.Service{Deployments: []domain.Deployment{d}},
		Tasks:        []domain.Task{task},
		TargetGroups: []domain.TargetGroup{{Name: "api-tg", Targets: []domain.Target{{ID: "10.0.0.5", State: "healthy"}}}},
		Definitions:  map[string]domain.TaskDefinition{"api:8": {Containers: []domain.ContainerDefinition{{Name: "app", Image: "repo/api:v42"}}}},
	}
	if target, group, ok := r.TargetOf(task); !ok || target.State != "healthy" || group != "api-tg" {
		t.Errorf("TargetOf() = %+v, %s, %v", target, group, ok)
	}
	if image, digest := r.Image(d); image != "repo/api:v42" || digest != "sha256:abc" {
		t.Errorf("Image() = %s, %s", image, digest)
	}
}
