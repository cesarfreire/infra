// Package app contains the use cases of the application and the ports
// (interfaces) that adapters must implement. It depends only on domain.
package app

import (
	"context"

	"infra/internal/domain"
)

// InstanceRepository lists compute instances from a provider.
type InstanceRepository interface {
	ListInstances(ctx context.Context) ([]domain.Instance, error)
}

// Command describes an external interactive process (e.g. an SSM session)
// to be run in the user's terminal. Use cases return it instead of running
// it, so the UI decides how to hand over the terminal and tests stay pure.
type Command struct {
	Name string
	Args []string
}

// SessionCommandBuilder builds remote session commands for an instance.
type SessionCommandBuilder interface {
	ShellSession(instanceID string) Command
	PortForwardSession(pf domain.PortForward) Command
}

// ConsoleURLBuilder builds web console links for resources.
type ConsoleURLBuilder interface {
	InstanceURL(instanceID string) string
}

// URLOpener opens a URL in the user's browser.
type URLOpener interface {
	Open(url string) error
}

// ContainerRepository lists container clusters, services and tasks.
type ContainerRepository interface {
	ListClusters(ctx context.Context) ([]domain.Cluster, error)
	ListServices(ctx context.Context, cluster string) ([]domain.Service, error)
	// GetService reads one service, with its deployments and events.
	GetService(ctx context.Context, cluster, name string) (domain.Service, error)
	// GetTaskDefinition reads a revision ("family:revision").
	GetTaskDefinition(ctx context.Context, name string) (domain.TaskDefinition, error)
	// ListDeploymentTasks lists the running, starting and recently
	// stopped tasks of a service.
	ListDeploymentTasks(ctx context.Context, cluster, service string) ([]domain.Task, error)
	// ListTasks lists running tasks; an empty service lists every task in
	// the cluster.
	ListTasks(ctx context.Context, cluster, service string) ([]domain.Task, error)
}

// ContainerSessionBuilder builds remote session commands for containers.
type ContainerSessionBuilder interface {
	ContainerShellSession(task domain.Task, container domain.Container) Command
	ContainerPortForwardSession(task domain.Task, container domain.Container, pf domain.PortForward) Command
}

// ContainerConsoleURLBuilder builds web console links for container resources.
type ContainerConsoleURLBuilder interface {
	ClusterURL(cluster string) string
	ServiceURL(cluster, service string) string
	TaskURL(cluster, taskID string) string
}

// KeyValueRepository reads key-value tables and their items.
type KeyValueRepository interface {
	ListTables(ctx context.Context) ([]domain.Table, error)
	// Scan reads up to limit items starting at cursor (nil for the start).
	Scan(ctx context.Context, table domain.Table, cursor domain.Cursor, limit int) (domain.ItemPage, error)
	// Query reads items matching q, up to limit, starting at cursor.
	Query(ctx context.Context, table domain.Table, q domain.KeyQuery, cursor domain.Cursor, limit int) (domain.ItemPage, error)
}

// KeyValueConsoleURLBuilder builds web console links for tables.
type KeyValueConsoleURLBuilder interface {
	TableURL(table string) string
}

// CacheRepository lists in-memory caches.
type CacheRepository interface {
	ListCaches(ctx context.Context) ([]domain.Cache, error)
}

// JumpHostSessionBuilder builds sessions that run on an instance to reach
// resources only visible from inside the network.
type JumpHostSessionBuilder interface {
	// PortForwardSession tunnels to pf.RemoteHost through pf.Target.
	PortForwardSession(pf domain.PortForward) Command
	// RemoteCommandSession runs command interactively on the instance.
	RemoteCommandSession(instanceID, command string) Command
}

// CacheConsoleURLBuilder builds web console links for caches.
type CacheConsoleURLBuilder interface {
	CacheURL(c domain.Cache) string
}

// InstanceController changes the power state of instances.
type InstanceController interface {
	StartInstance(ctx context.Context, id string) error
	StopInstance(ctx context.Context, id string) error
	RebootInstance(ctx context.Context, id string) error
}

// ServiceController changes container services.
type ServiceController interface {
	// ForceNewDeployment replaces every task, e.g. to pick up a new image
	// pushed under the same tag.
	ForceNewDeployment(ctx context.Context, cluster, service string) error
	UpdateDesiredCount(ctx context.Context, cluster, service string, count int) error
	// UpdateTaskDefinition deploys another revision (used to roll back).
	UpdateTaskDefinition(ctx context.Context, cluster, service, taskDefinition string) error
}

// TargetHealthReader reads the health of the targets of a target group.
type TargetHealthReader interface {
	TargetHealth(ctx context.Context, targetGroupARN string) ([]domain.Target, error)
}

// ItemWriter changes items of key-value tables.
type ItemWriter interface {
	// PutItem replaces an existing item; it fails if the item was deleted
	// meanwhile, so an edit never resurrects it.
	PutItem(ctx context.Context, table domain.Table, item domain.Item) error
	DeleteItem(ctx context.Context, table domain.Table, key domain.Item) error
}

// QueueRepository reads message queues.
type QueueRepository interface {
	ListQueues(ctx context.Context) ([]domain.Queue, error)
	// PeekMessages receives up to max messages without deleting them; they
	// become visible again right away, but their receive count goes up.
	PeekMessages(ctx context.Context, q domain.Queue, max int) ([]domain.Message, error)
}

// QueueConsoleURLBuilder builds web console links for queues.
type QueueConsoleURLBuilder interface {
	QueueURL(q domain.Queue) string
}

// LoadBalancerRepository reads load balancers and their targets.
type LoadBalancerRepository interface {
	ListLoadBalancers(ctx context.Context) ([]domain.LoadBalancer, error)
	ListListeners(ctx context.Context, lb domain.LoadBalancer) ([]domain.Listener, error)
	// ListTargetGroups returns the target groups of lb with target health.
	ListTargetGroups(ctx context.Context, lb domain.LoadBalancer) ([]domain.TargetGroup, error)
}

// LoadBalancerConsoleURLBuilder builds web console links for load balancers.
type LoadBalancerConsoleURLBuilder interface {
	LoadBalancerURL(lb domain.LoadBalancer) string
}

// ParameterRepository reads the parameter store.
type ParameterRepository interface {
	ListParameters(ctx context.Context) ([]domain.Parameter, error)
	// ParameterValue returns the value, decrypted for SecureString.
	ParameterValue(ctx context.Context, name string) (string, error)
}

// SecretRepository reads secrets.
type SecretRepository interface {
	ListSecrets(ctx context.Context) ([]domain.Secret, error)
	SecretValue(ctx context.Context, arn string) (domain.SecretValue, error)
}

// ConfigConsoleURLBuilder builds web console links for parameters and secrets.
type ConfigConsoleURLBuilder interface {
	ParameterURL(p domain.Parameter) string
	SecretURL(s domain.Secret) string
}
