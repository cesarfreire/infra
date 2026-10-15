package aws

import (
	"context"

	sdkaws "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"

	"infra/internal/domain"
)

// EC2API is the subset of *ec2.Client used, so tests can pass a fake.
type EC2API interface {
	ec2.DescribeInstancesAPIClient
	StartInstances(ctx context.Context, in *ec2.StartInstancesInput, optFns ...func(*ec2.Options)) (*ec2.StartInstancesOutput, error)
	StopInstances(ctx context.Context, in *ec2.StopInstancesInput, optFns ...func(*ec2.Options)) (*ec2.StopInstancesOutput, error)
	RebootInstances(ctx context.Context, in *ec2.RebootInstancesInput, optFns ...func(*ec2.Options)) (*ec2.RebootInstancesOutput, error)
}

// InstanceRepository lists and controls EC2 instances. It implements
// app.InstanceRepository and app.InstanceController.
type InstanceRepository struct {
	client EC2API
}

// NewInstanceRepository returns a repository backed by client.
func NewInstanceRepository(client EC2API) *InstanceRepository {
	return &InstanceRepository{client: client}
}

// StartInstance starts a stopped instance.
func (r *InstanceRepository) StartInstance(ctx context.Context, id string) error {
	_, err := r.client.StartInstances(ctx, &ec2.StartInstancesInput{InstanceIds: []string{id}})
	return classify(err)
}

// StopInstance stops a running instance.
func (r *InstanceRepository) StopInstance(ctx context.Context, id string) error {
	_, err := r.client.StopInstances(ctx, &ec2.StopInstancesInput{InstanceIds: []string{id}})
	return classify(err)
}

// RebootInstance reboots a running instance.
func (r *InstanceRepository) RebootInstance(ctx context.Context, id string) error {
	_, err := r.client.RebootInstances(ctx, &ec2.RebootInstancesInput{InstanceIds: []string{id}})
	return classify(err)
}

// ListInstances returns every instance in the configured region.
func (r *InstanceRepository) ListInstances(ctx context.Context) ([]domain.Instance, error) {
	var instances []domain.Instance
	pages := ec2.NewDescribeInstancesPaginator(r.client, &ec2.DescribeInstancesInput{})
	for pages.HasMorePages() {
		page, err := pages.NextPage(ctx)
		if err != nil {
			return nil, classify(err)
		}
		for _, reservation := range page.Reservations {
			for _, inst := range reservation.Instances {
				instances = append(instances, toDomainInstance(inst))
			}
		}
	}
	return instances, nil
}

func toDomainInstance(i types.Instance) domain.Instance {
	inst := domain.Instance{
		ID:         sdkaws.ToString(i.InstanceId),
		Type:       string(i.InstanceType),
		PrivateIP:  sdkaws.ToString(i.PrivateIpAddress),
		PublicIP:   sdkaws.ToString(i.PublicIpAddress),
		Platform:   sdkaws.ToString(i.PlatformDetails),
		LaunchTime: sdkaws.ToTime(i.LaunchTime),
		Tags:       make(map[string]string, len(i.Tags)),
	}
	if i.State != nil {
		inst.State = domain.InstanceState(i.State.Name)
	}
	if i.Placement != nil {
		inst.AvailabilityZone = sdkaws.ToString(i.Placement.AvailabilityZone)
	}
	for _, tag := range i.Tags {
		inst.Tags[sdkaws.ToString(tag.Key)] = sdkaws.ToString(tag.Value)
	}
	inst.Name = inst.Tags["Name"]
	return inst
}
