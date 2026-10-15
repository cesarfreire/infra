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
