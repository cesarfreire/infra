package aws

import (
	"context"
	"fmt"
	"slices"
	"strings"

	sdkaws "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ecs"
	"github.com/aws/aws-sdk-go-v2/service/ecs/types"

	"infra/internal/domain"
)

// ECS Describe* calls accept a limited number of ARNs per request.
const (
	describeClustersBatch = 100
	describeServicesBatch = 10
	describeTasksBatch    = 100
)

// ECSAPI is the subset of *ecs.Client used, so tests can pass a fake.
type ECSAPI interface {
	ecs.ListClustersAPIClient
	ecs.ListServicesAPIClient
	ecs.ListTasksAPIClient
	DescribeClusters(ctx context.Context, in *ecs.DescribeClustersInput, optFns ...func(*ecs.Options)) (*ecs.DescribeClustersOutput, error)
	DescribeServices(ctx context.Context, in *ecs.DescribeServicesInput, optFns ...func(*ecs.Options)) (*ecs.DescribeServicesOutput, error)
	DescribeTasks(ctx context.Context, in *ecs.DescribeTasksInput, optFns ...func(*ecs.Options)) (*ecs.DescribeTasksOutput, error)
	UpdateService(ctx context.Context, in *ecs.UpdateServiceInput, optFns ...func(*ecs.Options)) (*ecs.UpdateServiceOutput, error)
}

// maxServiceEvents bounds the events kept per service (ECS returns up to 100).
const maxServiceEvents = 30

// ContainerRepository lists and controls ECS resources. It implements
// app.ContainerRepository and app.ServiceController.
type ContainerRepository struct {
	client ECSAPI
}

// NewContainerRepository returns a repository backed by client.
func NewContainerRepository(client ECSAPI) *ContainerRepository {
	return &ContainerRepository{client: client}
}

// ListClusters returns every cluster in the region.
func (r *ContainerRepository) ListClusters(ctx context.Context) ([]domain.Cluster, error) {
	var arns []string
	pages := ecs.NewListClustersPaginator(r.client, &ecs.ListClustersInput{})
	for pages.HasMorePages() {
		page, err := pages.NextPage(ctx)
		if err != nil {
			return nil, classify(err)
		}
		arns = append(arns, page.ClusterArns...)
	}

	var clusters []domain.Cluster
	for batch := range chunks(arns, describeClustersBatch) {
		out, err := r.client.DescribeClusters(ctx, &ecs.DescribeClustersInput{Clusters: batch})
		if err != nil {
			return nil, classify(err)
		}
		for _, c := range out.Clusters {
			clusters = append(clusters, domain.Cluster{
				Name:           sdkaws.ToString(c.ClusterName),
				ARN:            sdkaws.ToString(c.ClusterArn),
				Status:         sdkaws.ToString(c.Status),
				ActiveServices: int(c.ActiveServicesCount),
				RunningTasks:   int(c.RunningTasksCount),
				PendingTasks:   int(c.PendingTasksCount),
			})
		}
	}
	return clusters, nil
}

// ListServices returns every service of cluster.
func (r *ContainerRepository) ListServices(ctx context.Context, cluster string) ([]domain.Service, error) {
	var arns []string
	pages := ecs.NewListServicesPaginator(r.client, &ecs.ListServicesInput{Cluster: &cluster})
	for pages.HasMorePages() {
		page, err := pages.NextPage(ctx)
		if err != nil {
			return nil, classify(err)
		}
		arns = append(arns, page.ServiceArns...)
	}

	var services []domain.Service
	for batch := range chunks(arns, describeServicesBatch) {
		out, err := r.client.DescribeServices(ctx, &ecs.DescribeServicesInput{Cluster: &cluster, Services: batch})
		if err != nil {
			return nil, classify(err)
		}
		for _, s := range out.Services {
			services = append(services, toDomainService(cluster, s))
		}
	}
	return services, nil
}

// GetService reads one service.
func (r *ContainerRepository) GetService(ctx context.Context, cluster, name string) (domain.Service, error) {
	out, err := r.client.DescribeServices(ctx, &ecs.DescribeServicesInput{Cluster: &cluster, Services: []string{name}})
	if err != nil {
		return domain.Service{}, classify(err)
	}
	if len(out.Services) == 0 {
		return domain.Service{}, fmt.Errorf("service %s not found in %s", name, cluster)
	}
	return toDomainService(cluster, out.Services[0]), nil
}

// ForceNewDeployment replaces every task of the service.
func (r *ContainerRepository) ForceNewDeployment(ctx context.Context, cluster, service string) error {
	_, err := r.client.UpdateService(ctx, &ecs.UpdateServiceInput{Cluster: &cluster, Service: &service, ForceNewDeployment: true})
	return classify(err)
}

// UpdateDesiredCount scales the service.
func (r *ContainerRepository) UpdateDesiredCount(ctx context.Context, cluster, service string, count int) error {
	_, err := r.client.UpdateService(ctx, &ecs.UpdateServiceInput{Cluster: &cluster, Service: &service, DesiredCount: sdkaws.Int32(int32(count))})
	return classify(err)
}

func toDomainService(cluster string, s types.Service) domain.Service {
	svc := domain.Service{
		Name:           sdkaws.ToString(s.ServiceName),
		ARN:            sdkaws.ToString(s.ServiceArn),
		Cluster:        cluster,
		Status:         sdkaws.ToString(s.Status),
		LaunchType:     launchType(s.LaunchType, s.CapacityProviderStrategy),
		TaskDefinition: lastSegment(sdkaws.ToString(s.TaskDefinition)),
		Desired:        int(s.DesiredCount),
		Running:        int(s.RunningCount),
		Pending:        int(s.PendingCount),
		ExecEnabled:    s.EnableExecuteCommand,
	}
	for _, d := range s.Deployments {
		svc.Deployments = append(svc.Deployments, domain.Deployment{
			ID:             sdkaws.ToString(d.Id),
			Status:         sdkaws.ToString(d.Status),
			RolloutState:   string(d.RolloutState),
			RolloutReason:  sdkaws.ToString(d.RolloutStateReason),
			TaskDefinition: lastSegment(sdkaws.ToString(d.TaskDefinition)),
			Desired:        int(d.DesiredCount),
			Running:        int(d.RunningCount),
			Pending:        int(d.PendingCount),
			Failed:         int(d.FailedTasks),
			CreatedAt:      sdkaws.ToTime(d.CreatedAt),
			UpdatedAt:      sdkaws.ToTime(d.UpdatedAt),
		})
	}
	slices.SortFunc(svc.Deployments, func(a, b domain.Deployment) int { return b.CreatedAt.Compare(a.CreatedAt) })
	for _, e := range s.Events[:min(len(s.Events), maxServiceEvents)] { // ECS returns newest first
		svc.Events = append(svc.Events, domain.ServiceEvent{Time: sdkaws.ToTime(e.CreatedAt), Message: sdkaws.ToString(e.Message)})
	}
	return svc
}

// ListTasks returns the running tasks of service, or of the whole cluster
// when service is empty.
func (r *ContainerRepository) ListTasks(ctx context.Context, cluster, service string) ([]domain.Task, error) {
	in := &ecs.ListTasksInput{Cluster: &cluster}
	if service != "" {
		in.ServiceName = &service
	}
	var arns []string
	pages := ecs.NewListTasksPaginator(r.client, in)
	for pages.HasMorePages() {
		page, err := pages.NextPage(ctx)
		if err != nil {
			return nil, classify(err)
		}
		arns = append(arns, page.TaskArns...)
	}

	var tasks []domain.Task
	for batch := range chunks(arns, describeTasksBatch) {
		out, err := r.client.DescribeTasks(ctx, &ecs.DescribeTasksInput{Cluster: &cluster, Tasks: batch})
		if err != nil {
			return nil, classify(err)
		}
		for _, t := range out.Tasks {
			tasks = append(tasks, toDomainTask(cluster, t))
		}
	}
	return tasks, nil
}

func toDomainTask(cluster string, t types.Task) domain.Task {
	task := domain.Task{
		ID:               lastSegment(sdkaws.ToString(t.TaskArn)),
		ARN:              sdkaws.ToString(t.TaskArn),
		Cluster:          cluster,
		LastStatus:       sdkaws.ToString(t.LastStatus),
		DesiredStatus:    sdkaws.ToString(t.DesiredStatus),
		Health:           string(t.HealthStatus),
		LaunchType:       string(t.LaunchType),
		TaskDefinition:   lastSegment(sdkaws.ToString(t.TaskDefinitionArn)),
		CPU:              sdkaws.ToString(t.Cpu),
		Memory:           sdkaws.ToString(t.Memory),
		AvailabilityZone: sdkaws.ToString(t.AvailabilityZone),
		StartedAt:        sdkaws.ToTime(t.StartedAt),
		ExecEnabled:      t.EnableExecuteCommand,
	}
	// Tasks started by a service have the group "service:<name>".
	if name, ok := strings.CutPrefix(sdkaws.ToString(t.Group), "service:"); ok {
		task.Service = name
	}
	if task.LaunchType == "" {
		task.LaunchType = sdkaws.ToString(t.CapacityProviderName)
	}
	for _, c := range t.Containers {
		container := domain.Container{
			Name:      sdkaws.ToString(c.Name),
			RuntimeID: sdkaws.ToString(c.RuntimeId),
			Image:     sdkaws.ToString(c.Image),
			Status:    sdkaws.ToString(c.LastStatus),
			Health:    string(c.HealthStatus),
		}
		for _, agent := range c.ManagedAgents {
			if agent.Name == types.ManagedAgentNameExecuteCommandAgent {
				container.ExecAgent = sdkaws.ToString(agent.LastStatus)
			}
		}
		for _, ni := range c.NetworkInterfaces {
			if task.PrivateIP == "" {
				task.PrivateIP = sdkaws.ToString(ni.PrivateIpv4Address)
			}
		}
		task.Containers = append(task.Containers, container)
	}
	return task
}

func launchType(lt types.LaunchType, strategy []types.CapacityProviderStrategyItem) string {
	if lt != "" {
		return string(lt)
	}
	if len(strategy) > 0 {
		return sdkaws.ToString(strategy[0].CapacityProvider)
	}
	return ""
}

// lastSegment returns what follows the last "/" of an ARN.
func lastSegment(arn string) string {
	if i := strings.LastIndex(arn, "/"); i >= 0 {
		return arn[i+1:]
	}
	return arn
}

// chunks yields consecutive slices of at most size elements.
func chunks[T any](items []T, size int) func(func([]T) bool) {
	return func(yield func([]T) bool) {
		for len(items) > 0 {
			n := min(size, len(items))
			if !yield(items[:n]) {
				return
			}
			items = items[n:]
		}
	}
}
