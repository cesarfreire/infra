package domain

import (
	"fmt"
	"time"
)

// Cluster groups container services and tasks (an ECS cluster).
type Cluster struct {
	Name           string
	ARN            string
	Status         string
	ActiveServices int
	RunningTasks   int
	PendingTasks   int
}

// Service keeps a desired number of tasks running (an ECS service).
type Service struct {
	Name           string
	ARN            string
	Cluster        string
	Status         string
	LaunchType     string
	TaskDefinition string // family:revision
	Desired        int
	Running        int
	Pending        int
	ExecEnabled    bool
	Deployments    []Deployment   // newest first
	Events         []ServiceEvent // newest first
}

// Deployment is a rollout of a task definition in a service. A service has
// one PRIMARY deployment and, while rolling out, older ACTIVE ones.
type Deployment struct {
	ID             string
	Status         string // PRIMARY, ACTIVE, INACTIVE
	RolloutState   string // IN_PROGRESS, COMPLETED, FAILED
	RolloutReason  string
	TaskDefinition string
	Desired        int
	Running        int
	Pending        int
	Failed         int
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// ServiceEvent is a message ECS logs about a service (deploys, placement
// failures, health check failures...).
type ServiceEvent struct {
	Time    time.Time
	Message string
}

// RollingOut reports whether a deployment is in progress.
func (s Service) RollingOut() bool {
	for _, d := range s.Deployments {
		if d.RolloutState == "IN_PROGRESS" {
			return true
		}
	}
	return len(s.Deployments) > 1
}

// Task is a running set of containers (an ECS task).
type Task struct {
	ID               string
	ARN              string
	Cluster          string
	Service          string // empty for standalone tasks
	LastStatus       string
	DesiredStatus    string
	Health           string
	LaunchType       string
	TaskDefinition   string // family:revision
	CPU              string
	Memory           string
	AvailabilityZone string
	PrivateIP        string
	StartedAt        time.Time
	ExecEnabled      bool
	Containers       []Container
}

// Container is a container inside a task.
type Container struct {
	Name      string
	RuntimeID string
	Image     string
	Status    string
	Health    string
	// ExecAgent is the status of the agent that serves exec sessions
	// ("RUNNING" when usable); empty when unknown.
	ExecAgent string
}

const statusRunning = "RUNNING"

// IsRunning reports whether the task is running.
func (t Task) IsRunning() bool { return t.LastStatus == statusRunning }

// RunningContainers returns the containers that can receive sessions.
func (t Task) RunningContainers() []Container {
	var out []Container
	for _, c := range t.Containers {
		if c.IsRunning() {
			out = append(out, c)
		}
	}
	return out
}

// IsRunning reports whether the container is running.
func (c Container) IsRunning() bool { return c.Status == statusRunning }

// CanExec checks that an interactive session can be opened in container.
func (t Task) CanExec(c Container) error {
	switch {
	case !t.IsRunning():
		return fmt.Errorf("task %s: %w", t.ID, ErrTaskNotRunning)
	case !t.ExecEnabled:
		return fmt.Errorf("task %s: %w", t.ID, ErrExecNotEnabled)
	case !c.IsRunning():
		return fmt.Errorf("container %s: %w", c.Name, ErrContainerNotRunning)
	case c.ExecAgent != "" && c.ExecAgent != statusRunning:
		return fmt.Errorf("container %s: %w (agent %s)", c.Name, ErrExecNotEnabled, c.ExecAgent)
	}
	return nil
}
