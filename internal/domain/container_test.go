package domain_test

import (
	"errors"
	"testing"

	"infra/internal/domain"
)

func TestTaskCanExec(t *testing.T) {
	running := domain.Container{Name: "app", Status: "RUNNING", ExecAgent: "RUNNING"}
	task := domain.Task{ID: "t1", LastStatus: "RUNNING", ExecEnabled: true, Containers: []domain.Container{running}}

	tests := []struct {
		name string
		task func(domain.Task) domain.Task
		c    domain.Container
		want error
	}{
		{"ok", func(t domain.Task) domain.Task { return t }, running, nil},
		{"agent unknown is ok", func(t domain.Task) domain.Task { return t }, domain.Container{Status: "RUNNING"}, nil},
		{"task stopped", func(t domain.Task) domain.Task { t.LastStatus = "STOPPED"; return t }, running, domain.ErrTaskNotRunning},
		{"exec disabled", func(t domain.Task) domain.Task { t.ExecEnabled = false; return t }, running, domain.ErrExecNotEnabled},
		{"container stopped", func(t domain.Task) domain.Task { return t }, domain.Container{Status: "STOPPED"}, domain.ErrContainerNotRunning},
		{"agent stopped", func(t domain.Task) domain.Task { return t }, domain.Container{Status: "RUNNING", ExecAgent: "STOPPED"}, domain.ErrExecNotEnabled},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.task(task).CanExec(tt.c)
			if !errors.Is(err, tt.want) || (tt.want == nil && err != nil) {
				t.Errorf("CanExec() = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestRunningContainers(t *testing.T) {
	task := domain.Task{Containers: []domain.Container{
		{Name: "app", Status: "RUNNING"}, {Name: "init", Status: "STOPPED"},
	}}
	if got := task.RunningContainers(); len(got) != 1 || got[0].Name != "app" {
		t.Errorf("RunningContainers() = %v", got)
	}
}

func TestRollingOut(t *testing.T) {
	steady := domain.Service{Deployments: []domain.Deployment{{Status: "PRIMARY", RolloutState: "COMPLETED"}}}
	rolling := domain.Service{Deployments: []domain.Deployment{{Status: "PRIMARY", RolloutState: "IN_PROGRESS"}, {Status: "ACTIVE"}}}
	if steady.RollingOut() || !rolling.RollingOut() {
		t.Error("RollingOut() wrong")
	}
}
