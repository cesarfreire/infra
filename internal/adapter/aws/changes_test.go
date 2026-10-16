package aws

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	sdkaws "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	dtypes "github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ecs"
	"github.com/aws/aws-sdk-go-v2/service/ecs/types"
	"github.com/aws/smithy-go"

	"infra/internal/domain"
)

// Controller methods of the fakes; the read methods live with their tests.

func (f fakeEC2) StartInstances(_ context.Context, in *ec2.StartInstancesInput, _ ...func(*ec2.Options)) (*ec2.StartInstancesOutput, error) {
	f.calls.add("start " + in.InstanceIds[0])
	return &ec2.StartInstancesOutput{}, nil
}

func (f fakeEC2) StopInstances(_ context.Context, in *ec2.StopInstancesInput, _ ...func(*ec2.Options)) (*ec2.StopInstancesOutput, error) {
	f.calls.add("stop " + in.InstanceIds[0])
	return &ec2.StopInstancesOutput{}, &smithy.GenericAPIError{Code: "UnauthorizedOperation"}
}

func (f fakeEC2) RebootInstances(_ context.Context, in *ec2.RebootInstancesInput, _ ...func(*ec2.Options)) (*ec2.RebootInstancesOutput, error) {
	f.calls.add("reboot " + in.InstanceIds[0])
	return &ec2.RebootInstancesOutput{}, nil
}

type callLog struct{ calls []string }

func (c *callLog) add(s string) {
	if c != nil {
		c.calls = append(c.calls, s)
	}
}

func TestInstanceController(t *testing.T) {
	log := &callLog{}
	repo := NewInstanceRepository(fakeEC2{calls: log})
	ctx := context.Background()
	if err := repo.StartInstance(ctx, "i-1"); err != nil {
		t.Fatal(err)
	}
	if err := repo.StopInstance(ctx, "i-1"); !errors.Is(err, domain.ErrAccessDenied) {
		t.Errorf("errors must be classified: %v", err)
	}
	repo.RebootInstance(ctx, "i-1")
	if strings.Join(log.calls, ",") != "start i-1,stop i-1,reboot i-1" {
		t.Errorf("calls = %v", log.calls)
	}
}

func (f *fakeECS) UpdateService(_ context.Context, in *ecs.UpdateServiceInput, _ ...func(*ecs.Options)) (*ecs.UpdateServiceOutput, error) {
	f.updates = append(f.updates, in)
	return &ecs.UpdateServiceOutput{}, nil
}

func TestServiceController(t *testing.T) {
	f := &fakeECS{}
	repo := NewContainerRepository(f)
	repo.ForceNewDeployment(context.Background(), "prod", "api")
	repo.UpdateDesiredCount(context.Background(), "prod", "api", 4)
	if len(f.updates) != 2 || !f.updates[0].ForceNewDeployment || f.updates[0].DesiredCount != nil ||
		*f.updates[1].DesiredCount != 4 || f.updates[1].ForceNewDeployment {
		t.Errorf("updates = %+v", f.updates)
	}
}

func TestServiceDeploymentsAndEvents(t *testing.T) {
	now := time.Now()
	s := toDomainService("prod", types.Service{
		ServiceName: sdkaws.String("api"),
		Deployments: []types.Deployment{
			{Id: sdkaws.String("old"), Status: sdkaws.String("ACTIVE"), CreatedAt: sdkaws.Time(now.Add(-time.Hour)), RunningCount: 1},
			{Id: sdkaws.String("new"), Status: sdkaws.String("PRIMARY"), RolloutState: types.DeploymentRolloutStateInProgress,
				CreatedAt: sdkaws.Time(now), DesiredCount: 3, RunningCount: 1, TaskDefinition: sdkaws.String("arn:x:task-definition/api:8")},
		},
		Events: []types.ServiceEvent{{Message: sdkaws.String("has reached a steady state"), CreatedAt: sdkaws.Time(now)}},
	})
	if len(s.Deployments) != 2 || s.Deployments[0].ID != "new" || s.Deployments[0].TaskDefinition != "api:8" || !s.RollingOut() {
		t.Errorf("deployments = %+v", s.Deployments)
	}
	if len(s.Events) != 1 || !strings.Contains(s.Events[0].Message, "steady") {
		t.Errorf("events = %+v", s.Events)
	}
}

func (f *fakeDynamo) PutItem(_ context.Context, in *dynamodb.PutItemInput, _ ...func(*dynamodb.Options)) (*dynamodb.PutItemOutput, error) {
	f.put = in
	if f.putErr != nil {
		return nil, f.putErr
	}
	return &dynamodb.PutItemOutput{}, nil
}

func (f *fakeDynamo) DeleteItem(_ context.Context, in *dynamodb.DeleteItemInput, _ ...func(*dynamodb.Options)) (*dynamodb.DeleteItemOutput, error) {
	f.del = in
	return &dynamodb.DeleteItemOutput{}, nil
}

func TestPutItemConvertsAndConditions(t *testing.T) {
	f := &fakeDynamo{}
	repo := NewKeyValueRepository(f)
	table := domain.Table{Name: "users", PartitionKey: domain.KeyAttribute{Name: "id", Type: "S"}}
	item := domain.Item{
		"id":   "u1",
		"n":    domain.Number("1.50"),
		"tags": domain.StringSet{"a"},
		"ns":   domain.NumberSet{"1"},
		"m":    map[string]any{"ok": true, "nil": nil},
		"l":    []any{"x", domain.Number("2")},
	}
	if err := repo.PutItem(context.Background(), table, item); err != nil {
		t.Fatal(err)
	}
	if *f.put.ConditionExpression != "attribute_exists(#pk)" || f.put.ExpressionAttributeNames["#pk"] != "id" {
		t.Errorf("the put must require an existing item: %+v", f.put)
	}
	// Round trip back through toValue.
	back := toItem(f.put.Item)
	if back["n"] != domain.Number("1.50") || back["tags"].(domain.StringSet)[0] != "a" || back["ns"].(domain.NumberSet)[0] != "1" {
		t.Errorf("round trip = %#v", back)
	}

	f.putErr = &dtypes.ConditionalCheckFailedException{}
	if err := repo.PutItem(context.Background(), table, item); err == nil || !strings.Contains(err.Error(), "no longer exists") {
		t.Errorf("a failed condition should explain the item is gone: %v", err)
	}

	repo.DeleteItem(context.Background(), table, domain.Item{"id": "u1"})
	if s, ok := f.del.Key["id"].(*dtypes.AttributeValueMemberS); !ok || s.Value != "u1" {
		t.Errorf("delete key = %+v", f.del.Key)
	}
}

func TestListDeploymentTasksIncludesStopped(t *testing.T) {
	f := &fakeECS{}
	tasks, err := NewContainerRepository(f).ListDeploymentTasks(context.Background(), "prod", "api")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(f.listedStatuses, ",") != "RUNNING,STOPPED" {
		t.Errorf("statuses listed = %v", f.listedStatuses)
	}
	// The fake lists the same task in both: it must be described once.
	if len(tasks) != 1 {
		t.Errorf("tasks = %d, want the duplicate removed", len(tasks))
	}
}

func TestTaskStopDetails(t *testing.T) {
	code := int32(137)
	task := toDomainTask("prod", types.Task{
		TaskArn:       sdkaws.String("arn:task/prod/abc"),
		StartedBy:     sdkaws.String("ecs-svc/123"),
		StoppedReason: sdkaws.String("Scaling activity initiated by (deployment ecs-svc/456)"),
		StopCode:      types.TaskStopCodeServiceSchedulerInitiated,
		Containers:    []types.Container{{Name: sdkaws.String("app"), ExitCode: &code, Reason: sdkaws.String("OOM")}},
	})
	if task.StartedBy != "ecs-svc/123" || task.StopCode != "ServiceSchedulerInitiated" || !strings.Contains(task.StoppedReason, "Scaling") {
		t.Errorf("task = %+v", task)
	}
	if c := task.Containers[0]; c.ExitCode == nil || *c.ExitCode != 137 || c.Reason != "OOM" {
		t.Errorf("container = %+v", c)
	}
}

func (f *fakeECS) DescribeTaskDefinition(_ context.Context, in *ecs.DescribeTaskDefinitionInput, _ ...func(*ecs.Options)) (*ecs.DescribeTaskDefinitionOutput, error) {
	return &ecs.DescribeTaskDefinitionOutput{TaskDefinition: &types.TaskDefinition{
		Family: sdkaws.String("api"), Revision: 8, Cpu: sdkaws.String("512"), Memory: sdkaws.String("1024"),
		ContainerDefinitions: []types.ContainerDefinition{{
			Name: sdkaws.String("app"), Image: sdkaws.String("repo/api:v42"), Cpu: 256, Memory: sdkaws.Int32(512),
			Environment:  []types.KeyValuePair{{Name: sdkaws.String("LOG"), Value: sdkaws.String("info")}},
			Secrets:      []types.Secret{{Name: sdkaws.String("DB"), ValueFrom: sdkaws.String("arn:secret")}},
			PortMappings: []types.PortMapping{{ContainerPort: sdkaws.Int32(8080)}},
		}},
	}}, nil
}

func TestGetTaskDefinition(t *testing.T) {
	def, err := NewContainerRepository(&fakeECS{}).GetTaskDefinition(context.Background(), "api:8")
	if err != nil {
		t.Fatal(err)
	}
	c := def.Containers[0]
	if def.Name() != "api:8" || c.Image != "repo/api:v42" || c.Memory != 512 || c.Env["LOG"] != "info" ||
		c.Secrets["DB"] != "arn:secret" || c.Ports[0] != 8080 {
		t.Errorf("unexpected mapping: %+v", def)
	}
}

func TestRollbackUpdatesTaskDefinition(t *testing.T) {
	f := &fakeECS{}
	NewContainerRepository(f).UpdateTaskDefinition(context.Background(), "prod", "api", "api:7")
	if len(f.updates) != 1 || sdkaws.ToString(f.updates[0].TaskDefinition) != "api:7" {
		t.Errorf("updates = %+v", f.updates)
	}
}

func TestServiceTargetGroupsAndDigest(t *testing.T) {
	s := toDomainService("prod", types.Service{LoadBalancers: []types.LoadBalancer{
		{TargetGroupArn: sdkaws.String("tg-1"), ContainerName: sdkaws.String("app")},
		{TargetGroupArn: sdkaws.String("tg-1"), ContainerName: sdkaws.String("app")}, // listed twice: kept once
	}})
	if len(s.TargetGroups) != 1 || s.TargetGroups[0] != "tg-1" {
		t.Errorf("target groups = %v", s.TargetGroups)
	}
	task := toDomainTask("prod", types.Task{TaskArn: sdkaws.String("t/1"),
		Containers: []types.Container{{Name: sdkaws.String("app"), ImageDigest: sdkaws.String("sha256:abc")}}})
	if task.Containers[0].ImageDigest != "sha256:abc" {
		t.Errorf("digest = %q", task.Containers[0].ImageDigest)
	}
}
