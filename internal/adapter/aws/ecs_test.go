package aws

import (
	"context"
	"slices"
	"testing"

	sdkaws "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ecs"
	"github.com/aws/aws-sdk-go-v2/service/ecs/types"

	"infra/internal/domain"
)

type fakeECS struct {
	serviceArns     []string
	describeBatches [][]string
	listTasksInput  *ecs.ListTasksInput
	listedStatuses  []string
	updates         []*ecs.UpdateServiceInput
}

func (f *fakeECS) ListClusters(context.Context, *ecs.ListClustersInput, ...func(*ecs.Options)) (*ecs.ListClustersOutput, error) {
	return &ecs.ListClustersOutput{ClusterArns: []string{"arn:aws:ecs:r:1:cluster/prod"}}, nil
}

func (f *fakeECS) DescribeClusters(_ context.Context, in *ecs.DescribeClustersInput, _ ...func(*ecs.Options)) (*ecs.DescribeClustersOutput, error) {
	return &ecs.DescribeClustersOutput{Clusters: []types.Cluster{{
		ClusterName: sdkaws.String("prod"), Status: sdkaws.String("ACTIVE"), RunningTasksCount: 3, ActiveServicesCount: 2,
	}}}, nil
}

func (f *fakeECS) ListServices(context.Context, *ecs.ListServicesInput, ...func(*ecs.Options)) (*ecs.ListServicesOutput, error) {
	return &ecs.ListServicesOutput{ServiceArns: f.serviceArns}, nil
}

func (f *fakeECS) DescribeServices(_ context.Context, in *ecs.DescribeServicesInput, _ ...func(*ecs.Options)) (*ecs.DescribeServicesOutput, error) {
	f.describeBatches = append(f.describeBatches, in.Services)
	var out []types.Service
	for _, arn := range in.Services {
		out = append(out, types.Service{
			ServiceName:              sdkaws.String(lastSegment(arn)),
			TaskDefinition:           sdkaws.String("arn:aws:ecs:r:1:task-definition/api:7"),
			CapacityProviderStrategy: []types.CapacityProviderStrategyItem{{CapacityProvider: sdkaws.String("FARGATE_SPOT")}},
			EnableExecuteCommand:     true,
		})
	}
	return &ecs.DescribeServicesOutput{Services: out}, nil
}

func (f *fakeECS) ListTasks(_ context.Context, in *ecs.ListTasksInput, _ ...func(*ecs.Options)) (*ecs.ListTasksOutput, error) {
	f.listTasksInput = in
	f.listedStatuses = append(f.listedStatuses, string(in.DesiredStatus))
	return &ecs.ListTasksOutput{TaskArns: []string{"arn:aws:ecs:r:1:task/prod/abc123"}}, nil
}

func (f *fakeECS) DescribeTasks(context.Context, *ecs.DescribeTasksInput, ...func(*ecs.Options)) (*ecs.DescribeTasksOutput, error) {
	return &ecs.DescribeTasksOutput{Tasks: []types.Task{{
		TaskArn:              sdkaws.String("arn:aws:ecs:r:1:task/prod/abc123"),
		Group:                sdkaws.String("service:api"),
		LastStatus:           sdkaws.String("RUNNING"),
		LaunchType:           types.LaunchTypeFargate,
		TaskDefinitionArn:    sdkaws.String("arn:aws:ecs:r:1:task-definition/api:7"),
		EnableExecuteCommand: true,
		Containers: []types.Container{{
			Name:              sdkaws.String("app"),
			RuntimeId:         sdkaws.String("abc123-111"),
			LastStatus:        sdkaws.String("RUNNING"),
			NetworkInterfaces: []types.NetworkInterface{{PrivateIpv4Address: sdkaws.String("10.0.0.5")}},
			ManagedAgents: []types.ManagedAgent{{
				Name: types.ManagedAgentNameExecuteCommandAgent, LastStatus: sdkaws.String("RUNNING"),
			}},
		}},
	}}}, nil
}

func TestListClusters(t *testing.T) {
	got, err := NewContainerRepository(&fakeECS{}).ListClusters(context.Background())
	if err != nil || len(got) != 1 || got[0].Name != "prod" || got[0].RunningTasks != 3 {
		t.Errorf("ListClusters() = %+v, %v", got, err)
	}
}

func TestListServicesDescribesInBatches(t *testing.T) {
	f := &fakeECS{}
	for i := range 25 {
		f.serviceArns = append(f.serviceArns, "arn:aws:ecs:r:1:service/prod/svc"+string(rune('a'+i)))
	}
	got, err := NewContainerRepository(f).ListServices(context.Background(), "prod")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 25 || len(f.describeBatches) != 3 || len(f.describeBatches[2]) != 5 {
		t.Fatalf("got %d services in batches %v", len(got), f.describeBatches)
	}
	if s := got[0]; s.Cluster != "prod" || s.TaskDefinition != "api:7" || s.LaunchType != "FARGATE_SPOT" || !s.ExecEnabled {
		t.Errorf("unexpected mapping: %+v", s)
	}
}

func TestListTasks(t *testing.T) {
	f := &fakeECS{}
	repo := NewContainerRepository(f)

	got, err := repo.ListTasks(context.Background(), "prod", "api")
	if err != nil {
		t.Fatal(err)
	}
	if sdkaws.ToString(f.listTasksInput.ServiceName) != "api" {
		t.Error("service filter not sent")
	}
	task := got[0]
	want := domain.Container{Name: "app", RuntimeID: "abc123-111", Status: "RUNNING", ExecAgent: "RUNNING"}
	if task.ID != "abc123" || task.Service != "api" || task.PrivateIP != "10.0.0.5" ||
		task.LaunchType != "FARGATE" || !task.ExecEnabled || task.Containers[0] != want {
		t.Errorf("unexpected mapping: %+v", task)
	}

	if _, err := repo.ListTasks(context.Background(), "prod", ""); err != nil || f.listTasksInput.ServiceName != nil {
		t.Error("an empty service should list the whole cluster")
	}
}

func TestContainerSessions(t *testing.T) {
	b := NewSessionCommandBuilder("", "us-east-1", Shells{})
	task := domain.Task{ID: "abc123", ARN: "arn:task", Cluster: "prod"}
	c := domain.Container{Name: "app", RuntimeID: "abc123-111"}

	shell := b.ContainerShellSession(task, c)
	want := []string{"ecs", "execute-command", "--cluster", "prod", "--task", "arn:task", "--container", "app",
		"--interactive", "--command", DefaultContainerShell, "--region", "us-east-1"}
	if !slices.Equal(shell.Args, want) {
		t.Errorf("ContainerShellSession() args = %v, want %v", shell.Args, want)
	}

	pf := b.ContainerPortForwardSession(task, c, domain.PortForward{LocalPort: 8080, RemotePort: 80})
	if i := slices.Index(pf.Args, "--target"); i < 0 || pf.Args[i+1] != "ecs:prod_abc123_abc123-111" {
		t.Errorf("ContainerPortForwardSession() args = %v", pf.Args)
	}
}

func TestECSConsoleURLs(t *testing.T) {
	b := NewConsoleURLBuilder("us-east-1")
	base := "https://us-east-1.console.aws.amazon.com/ecs/v2/clusters/prod/"
	tests := map[string]string{
		b.ClusterURL("prod"):        base + "services?region=us-east-1",
		b.ServiceURL("prod", "api"): base + "services/api/health?region=us-east-1",
		b.TaskURL("prod", "abc123"): base + "tasks/abc123/configuration?region=us-east-1",
	}
	for got, want := range tests {
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	}
}
