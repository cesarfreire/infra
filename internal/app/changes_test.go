package app_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"infra/internal/app"
	"infra/internal/domain"
)

// fakeControl records every change made through the controllers.
type fakeControl struct {
	calls []string
	items []domain.Item
}

func (f *fakeControl) StartInstance(_ context.Context, id string) error {
	f.calls = append(f.calls, "start "+id)
	return nil
}

func (f *fakeControl) StopInstance(_ context.Context, id string) error {
	f.calls = append(f.calls, "stop "+id)
	return nil
}

func (f *fakeControl) RebootInstance(_ context.Context, id string) error {
	f.calls = append(f.calls, "reboot "+id)
	return nil
}

func (f *fakeControl) ForceNewDeployment(_ context.Context, cluster, service string) error {
	f.calls = append(f.calls, "deploy "+cluster+"/"+service)
	return nil
}

func (f *fakeControl) UpdateDesiredCount(_ context.Context, cluster, service string, n int) error {
	f.calls = append(f.calls, "scale "+cluster+"/"+service)
	return nil
}

func (f *fakeControl) UpdateTaskDefinition(_ context.Context, cluster, service, td string) error {
	f.calls = append(f.calls, "rollback "+cluster+"/"+service+" to "+td)
	return nil
}

func (f *fakeControl) PutItem(_ context.Context, _ domain.Table, item domain.Item) error {
	f.items = append(f.items, item)
	return nil
}

func (f *fakeControl) DeleteItem(_ context.Context, _ domain.Table, key domain.Item) error {
	f.items = append(f.items, key)
	return nil
}

func TestInstancePowerChanges(t *testing.T) {
	ctrl := &fakeControl{}
	svc := app.NewInstanceService(app.InstanceDeps{Repo: fakeRepo{}, Control: ctrl})
	ctx := context.Background()
	stopped := domain.Instance{ID: "i-2", State: domain.InstanceStateStopped}

	if err := svc.Stop(ctx, running); err != nil {
		t.Fatal(err)
	}
	if err := svc.Start(ctx, stopped); err != nil {
		t.Fatal(err)
	}
	if err := svc.Reboot(ctx, stopped); !errors.Is(err, domain.ErrInvalidState) {
		t.Errorf("rebooting a stopped instance: %v", err)
	}
	if strings.Join(ctrl.calls, ",") != "stop i-1,start i-2" {
		t.Errorf("calls = %v", ctrl.calls)
	}

	readOnly := app.NewInstanceService(app.InstanceDeps{Control: ctrl, Policy: app.Policy{ReadOnly: true}})
	if err := readOnly.Stop(ctx, running); !errors.Is(err, domain.ErrReadOnly) {
		t.Errorf("read-only stop: %v", err)
	}
	if len(ctrl.calls) != 2 {
		t.Error("a read-only policy must not reach the controller")
	}
}

func TestServiceChanges(t *testing.T) {
	ctrl := &fakeControl{}
	svc := app.NewContainerService(app.ContainerDeps{Repo: fakeContainerRepo{}, Control: ctrl})
	api := domain.Service{Cluster: "prod", Name: "api"}
	ctx := context.Background()
	if err := svc.ForceDeployment(ctx, api); err != nil {
		t.Fatal(err)
	}
	if err := svc.SetDesiredCount(ctx, api, -1); err == nil {
		t.Error("negative counts must be rejected")
	}
	if err := svc.SetDesiredCount(ctx, api, 3); err != nil {
		t.Fatal(err)
	}
	if strings.Join(ctrl.calls, ",") != "deploy prod/api,scale prod/api" {
		t.Errorf("calls = %v", ctrl.calls)
	}
	if s, err := svc.GetService(ctx, "prod", "api"); err != nil || s.Name != "api" {
		t.Errorf("GetService() = %v, %v", s, err)
	}
	readOnly := app.NewContainerService(app.ContainerDeps{Control: ctrl, Policy: app.Policy{ReadOnly: true}})
	if err := readOnly.ForceDeployment(ctx, api); !errors.Is(err, domain.ErrReadOnly) {
		t.Errorf("read-only deploy: %v", err)
	}
}

var users = domain.Table{Name: "users",
	PartitionKey: domain.KeyAttribute{Name: "id", Type: "S"},
	SortKey:      &domain.KeyAttribute{Name: "v", Type: "N"}}

func TestSaveItemKeepsSetAndBinaryTypes(t *testing.T) {
	ctrl := &fakeControl{}
	svc := app.NewKeyValueService(app.KeyValueDeps{Writer: ctrl})
	original := domain.Item{
		"id":    "u1",
		"v":     domain.Number("1"),
		"tags":  domain.StringSet{"a"},
		"blob":  []byte("hi"),
		"inner": map[string]any{"scores": domain.NumberSet{"1"}},
	}
	edited := `{"id": "u1", "v": 1, "tags": ["a", "b"], "blob": "aGk=", "inner": {"scores": [1, 2.5]}, "big": 12345678901234567890}`
	item, err := svc.SaveItem(context.Background(), users, original, edited)
	if err != nil {
		t.Fatal(err)
	}
	if tags, ok := item["tags"].(domain.StringSet); !ok || len(tags) != 2 {
		t.Errorf("tags = %#v, want a StringSet", item["tags"])
	}
	if b, ok := item["blob"].([]byte); !ok || string(b) != "hi" {
		t.Errorf("blob = %#v, want []byte", item["blob"])
	}
	if s, ok := item["inner"].(map[string]any)["scores"].(domain.NumberSet); !ok || s[1] != "2.5" {
		t.Errorf("nested set = %#v", item["inner"])
	}
	if item["big"] != domain.Number("12345678901234567890") {
		t.Errorf("big = %#v, numbers must keep their exact text", item["big"])
	}
	if len(ctrl.items) != 1 {
		t.Error("the item should be written")
	}
}

func TestSaveItemRejectsKeyChangesAndBadJSON(t *testing.T) {
	svc := app.NewKeyValueService(app.KeyValueDeps{Writer: &fakeControl{}})
	original := domain.Item{"id": "u1", "v": domain.Number("1")}
	if _, err := svc.SaveItem(context.Background(), users, original, `{"id": "u2", "v": 1}`); err == nil || !strings.Contains(err.Error(), `"id"`) {
		t.Errorf("changing the key: %v", err)
	}
	if _, err := svc.SaveItem(context.Background(), users, original, `{"id": "u1"`); err == nil {
		t.Error("invalid JSON must be rejected")
	}
	readOnly := app.NewKeyValueService(app.KeyValueDeps{Writer: &fakeControl{}, Policy: app.Policy{ReadOnly: true}})
	if err := readOnly.DeleteItem(context.Background(), users, original); !errors.Is(err, domain.ErrReadOnly) {
		t.Errorf("read-only delete: %v", err)
	}
}

func TestDeleteItemSendsOnlyTheKey(t *testing.T) {
	ctrl := &fakeControl{}
	svc := app.NewKeyValueService(app.KeyValueDeps{Writer: ctrl})
	svc.DeleteItem(context.Background(), users, domain.Item{"id": "u1", "v": domain.Number("1"), "name": "x"})
	if len(ctrl.items[0]) != 2 {
		t.Errorf("key = %v", ctrl.items[0])
	}
}
