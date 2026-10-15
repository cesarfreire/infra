package aws

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	sdkaws "github.com/aws/aws-sdk-go-v2/aws"
	elb "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2"
	ltypes "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2/types"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	stypes "github.com/aws/aws-sdk-go-v2/service/sqs/types"

	"infra/internal/domain"
)

type fakeSQS struct {
	mu      sync.Mutex
	receive *sqs.ReceiveMessageInput
}

func (f *fakeSQS) ListQueues(context.Context, *sqs.ListQueuesInput, ...func(*sqs.Options)) (*sqs.ListQueuesOutput, error) {
	return &sqs.ListQueuesOutput{QueueUrls: []string{"https://sqs.us-east-1.amazonaws.com/1/orders", "https://sqs.us-east-1.amazonaws.com/1/orders-dlq"}}, nil
}

func (f *fakeSQS) GetQueueAttributes(_ context.Context, in *sqs.GetQueueAttributesInput, _ ...func(*sqs.Options)) (*sqs.GetQueueAttributesOutput, error) {
	attrs := map[string]string{"ApproximateNumberOfMessages": "7", "VisibilityTimeout": "30", "SqsManagedSseEnabled": "true"}
	if !strings.HasSuffix(*in.QueueUrl, "-dlq") {
		attrs["RedrivePolicy"] = `{"deadLetterTargetArn":"arn:aws:sqs:us-east-1:1:orders-dlq","maxReceiveCount":"5"}`
	}
	return &sqs.GetQueueAttributesOutput{Attributes: attrs}, nil
}

func (f *fakeSQS) ReceiveMessage(_ context.Context, in *sqs.ReceiveMessageInput, _ ...func(*sqs.Options)) (*sqs.ReceiveMessageOutput, error) {
	f.mu.Lock()
	f.receive = in
	f.mu.Unlock()
	return &sqs.ReceiveMessageOutput{Messages: []stypes.Message{{
		MessageId:         sdkaws.String("m1"),
		Body:              sdkaws.String(`{"order":1}`),
		Attributes:        map[string]string{"SentTimestamp": "1760530000000", "ApproximateReceiveCount": "3"},
		MessageAttributes: map[string]stypes.MessageAttributeValue{"type": {StringValue: sdkaws.String("created")}},
	}}}, nil
}

func TestListQueues(t *testing.T) {
	got, err := NewQueueRepository(&fakeSQS{}).ListQueues(context.Background())
	if err != nil || len(got) != 2 {
		t.Fatalf("ListQueues() = %v, %v", got, err)
	}
	q := got[0]
	if q.Name != "orders" || q.Visible != 7 || q.VisibilityTimeout != 30*time.Second || !q.Encrypted ||
		q.DeadLetterQueue != "orders-dlq" || q.MaxReceiveCount != 5 {
		t.Errorf("unexpected mapping: %+v", q)
	}
}

func TestPeekNeverSendsZeroVisibility(t *testing.T) {
	f := &fakeSQS{}
	msgs, err := NewQueueRepository(f).PeekMessages(context.Background(), domain.Queue{URL: "u"}, 10)
	if err != nil {
		t.Fatal(err)
	}
	if f.receive.VisibilityTimeout == 0 || f.receive.MaxNumberOfMessages != 10 {
		t.Errorf("a zero VisibilityTimeout is omitted by the SDK; got %+v", f.receive)
	}
	m := msgs[0]
	if m.ReceiveCount != 3 || m.SentAt.UnixMilli() != 1760530000000 || m.Attributes["type"] != "created" {
		t.Errorf("unexpected message: %+v", m)
	}
}

type fakeELB struct{}

func (fakeELB) DescribeLoadBalancers(context.Context, *elb.DescribeLoadBalancersInput, ...func(*elb.Options)) (*elb.DescribeLoadBalancersOutput, error) {
	return &elb.DescribeLoadBalancersOutput{LoadBalancers: []ltypes.LoadBalancer{{
		LoadBalancerName:  sdkaws.String("web"),
		LoadBalancerArn:   sdkaws.String("arn:aws:elasticloadbalancing:us-east-1:1:loadbalancer/app/web/abc"),
		Type:              ltypes.LoadBalancerTypeEnumApplication,
		State:             &ltypes.LoadBalancerState{Code: ltypes.LoadBalancerStateEnumActive},
		AvailabilityZones: []ltypes.AvailabilityZone{{ZoneName: sdkaws.String("us-east-1a")}},
	}}}, nil
}

func (fakeELB) DescribeListeners(context.Context, *elb.DescribeListenersInput, ...func(*elb.Options)) (*elb.DescribeListenersOutput, error) {
	return &elb.DescribeListenersOutput{Listeners: []ltypes.Listener{
		{Protocol: ltypes.ProtocolEnumHttps, Port: sdkaws.Int32(443), DefaultActions: []ltypes.Action{{
			Type: ltypes.ActionTypeEnumForward, TargetGroupArn: sdkaws.String("arn:aws:elasticloadbalancing:us-east-1:1:targetgroup/api-tg/123")}}},
		{Protocol: ltypes.ProtocolEnumHttp, Port: sdkaws.Int32(80), DefaultActions: []ltypes.Action{{
			Type: ltypes.ActionTypeEnumRedirect, RedirectConfig: &ltypes.RedirectActionConfig{Protocol: sdkaws.String("HTTPS"), Port: sdkaws.String("443")}}}},
	}}, nil
}

func (fakeELB) DescribeTargetGroups(context.Context, *elb.DescribeTargetGroupsInput, ...func(*elb.Options)) (*elb.DescribeTargetGroupsOutput, error) {
	return &elb.DescribeTargetGroupsOutput{TargetGroups: []ltypes.TargetGroup{{
		TargetGroupName: sdkaws.String("api-tg"), TargetGroupArn: sdkaws.String("tg-arn"),
		Protocol: ltypes.ProtocolEnumHttp, Port: sdkaws.Int32(8080), HealthCheckProtocol: ltypes.ProtocolEnumHttp, HealthCheckPath: sdkaws.String("/health"),
	}}}, nil
}

func (fakeELB) DescribeTargetHealth(context.Context, *elb.DescribeTargetHealthInput, ...func(*elb.Options)) (*elb.DescribeTargetHealthOutput, error) {
	return &elb.DescribeTargetHealthOutput{TargetHealthDescriptions: []ltypes.TargetHealthDescription{{
		Target:       &ltypes.TargetDescription{Id: sdkaws.String("i-1"), Port: sdkaws.Int32(8080)},
		TargetHealth: &ltypes.TargetHealth{State: ltypes.TargetHealthStateEnumUnhealthy, Reason: ltypes.TargetHealthReasonEnumFailedHealthChecks},
	}}}, nil
}

func TestLoadBalancerRepository(t *testing.T) {
	repo := NewLoadBalancerRepository(fakeELB{})
	ctx := context.Background()
	lbs, err := repo.ListLoadBalancers(ctx)
	if err != nil || lbs[0].Type != "application" || lbs[0].State != "active" || lbs[0].Zones[0] != "us-east-1a" {
		t.Fatalf("ListLoadBalancers() = %+v, %v", lbs, err)
	}
	ls, _ := repo.ListListeners(ctx, lbs[0])
	if ls[0].Action != "forward to api-tg" || ls[1].Action != "redirect to HTTPS:443" {
		t.Errorf("listeners = %+v", ls)
	}
	groups, _ := repo.ListTargetGroups(ctx, lbs[0])
	if g := groups[0]; g.HealthCheck != "HTTP /health" || len(g.Targets) != 1 || g.Targets[0].State != "unhealthy" {
		t.Errorf("target groups = %+v", groups)
	}
}

func TestLoadBalancerMetricDimension(t *testing.T) {
	f := &fakeCloudWatch{}
	lb := domain.LoadBalancer{Type: "application", ARN: "arn:aws:elasticloadbalancing:us-east-1:1:loadbalancer/app/web/abc"}
	newTestMetrics(f).LoadBalancerMetrics(context.Background(), lb, threeHours)
	d := f.inputs[0].MetricDataQueries[0].MetricStat.Metric.Dimensions[0]
	if *d.Name != "LoadBalancer" || *d.Value != "app/web/abc" {
		t.Errorf("dimension = %s=%s", *d.Name, *d.Value)
	}
	if series, _ := newTestMetrics(&fakeCloudWatch{}).LoadBalancerMetrics(context.Background(), domain.LoadBalancer{Type: "gateway"}, threeHours); series != nil {
		t.Error("gateway load balancers have no charts")
	}
}
