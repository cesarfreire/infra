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
	// TargetGroups are the ARNs of the load balancer target groups the
	// service registers its tasks in.
	TargetGroups []string
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
	// StartedBy is the ID of the deployment that started the task, for
	// tasks of a service (e.g. "ecs-svc/123...").
	StartedBy     string
	StoppedAt     time.Time
	StoppedReason string
	StopCode      string
}

// Container is a container inside a task.
type Container struct {
	Name        string
	RuntimeID   string
	Image       string
	ImageDigest string // what the tag resolved to when the task started
	Status      string
	Health      string
	// ExecAgent is the status of the agent that serves exec sessions
	// ("RUNNING" when usable); empty when unknown.
	ExecAgent string
	ExitCode  *int   // set once the container stopped
	Reason    string // why it stopped, when ECS says
}

// Rollout is a snapshot of a service and its tasks, including the ones
// stopped recently, to follow a deployment.
type Rollout struct {
	Service Service
	Tasks   []Task
	// TargetGroups have the load balancer health of the targets.
	TargetGroups []TargetGroup
	// Definitions are the task definitions of the deployments, by name
	// ("family:revision"); missing when they could not be read.
	Definitions map[string]TaskDefinition
	// Problems explain extra information that could not be read (target
	// health, task definitions), without failing the whole snapshot.
	Problems []string
}

// TargetOf returns the load balancer target of a task, matched by IP
// (target groups of type ip, used with awsvpc networking).
func (r Rollout) TargetOf(t Task) (Target, string, bool) {
	if t.PrivateIP == "" {
		return Target{}, "", false
	}
	for _, g := range r.TargetGroups {
		for _, target := range g.Targets {
			if target.ID == t.PrivateIP {
				return target, g.Name, true
			}
		}
	}
	return Target{}, "", false
}

// Image returns the image of the first container of task definition name,
// and the digest it resolved to in the tasks of deployment d.
func (r Rollout) Image(d Deployment) (image, digest string) {
	if def, ok := r.Definitions[d.TaskDefinition]; ok && len(def.Containers) > 0 {
		image = def.Containers[0].Image
		for _, t := range r.TasksOf(d) {
			for _, c := range t.Containers {
				if c.Name == def.Containers[0].Name && c.ImageDigest != "" {
					return image, c.ImageDigest
				}
			}
		}
	}
	return image, ""
}

// TasksOf returns the tasks started by deployment d.
func (r Rollout) TasksOf(d Deployment) []Task {
	var out []Task
	for _, t := range r.Tasks {
		if t.StartedBy == d.ID {
			out = append(out, t)
		}
	}
	return out
}

// Orphans returns the tasks of no current deployment (e.g. stopped tasks
// of a deployment that already finished).
func (r Rollout) Orphans() []Task {
	var out []Task
	for _, t := range r.Tasks {
		found := false
		for _, d := range r.Service.Deployments {
			found = found || t.StartedBy == d.ID
		}
		if !found {
			out = append(out, t)
		}
	}
	return out
}

// Stopping reports whether ECS is stopping the task or already did.
func (t Task) Stopping() bool {
	return t.DesiredStatus == "STOPPED"
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
