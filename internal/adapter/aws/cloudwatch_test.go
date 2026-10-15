package aws

import (
	"context"
	"strings"
	"testing"
	"time"

	sdkaws "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatch"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatch/types"

	"infra/internal/domain"
)

var t0 = time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)

type fakeCloudWatch struct {
	inputs []*cloudwatch.GetMetricDataInput
}

func (f *fakeCloudWatch) GetMetricData(_ context.Context, in *cloudwatch.GetMetricDataInput, _ ...func(*cloudwatch.Options)) (*cloudwatch.GetMetricDataOutput, error) {
	f.inputs = append(f.inputs, in)
	// Two pages: the first answers m0, the second m1 (out of order).
	if in.NextToken == nil {
		return &cloudwatch.GetMetricDataOutput{
			NextToken: sdkaws.String("p2"),
			MetricDataResults: []types.MetricDataResult{{
				Id: sdkaws.String("m0"), Timestamps: []time.Time{t0.Add(-5 * time.Minute), t0.Add(-10 * time.Minute)}, Values: []float64{20, 10},
			}},
		}, nil
	}
	return &cloudwatch.GetMetricDataOutput{MetricDataResults: []types.MetricDataResult{{
		Id: sdkaws.String("m1"), Timestamps: []time.Time{t0.Add(-5 * time.Minute)}, Values: []float64{600},
	}}}, nil
}

func newTestMetrics(f *fakeCloudWatch) *Metrics {
	m := NewMetrics(f)
	m.now = func() time.Time { return t0.Add(2 * time.Minute) }
	return m
}

var threeHours = domain.MetricWindow{Label: "3h", Range: 3 * time.Hour, Period: 5 * time.Minute}

func TestInstanceMetrics(t *testing.T) {
	f := &fakeCloudWatch{}
	series, err := newTestMetrics(f).InstanceMetrics(context.Background(), domain.Instance{ID: "i-1", Type: "t3.micro"}, threeHours)
	if err != nil {
		t.Fatal(err)
	}
	if len(series) != 4 || series[3].Label != "CPU credits" {
		t.Fatalf("burstable instances should include CPU credits: %+v", series)
	}
	cpu := series[0]
	if cpu.Unit != domain.UnitPercent || len(cpu.Points) != 2 || cpu.Points[0].Value != 10 {
		t.Errorf("CPU points should be sorted by time: %+v", cpu)
	}
	// NetworkIn is a Sum over 5 minutes: 600 bytes → 2 B/s.
	if net := series[1]; net.Unit != domain.UnitBytesPerSecond || net.Points[0].Value != 2 {
		t.Errorf("network should be a per-second rate: %+v", net)
	}

	in := f.inputs[0]
	if !in.EndTime.Equal(t0.Add(5*time.Minute)) || !in.StartTime.Equal(t0.Add(5*time.Minute-3*time.Hour)) {
		t.Errorf("window = %v..%v", in.StartTime, in.EndTime)
	}
	q := in.MetricDataQueries[0].MetricStat
	if *q.Metric.Namespace != "AWS/EC2" || *q.Metric.MetricName != "CPUUtilization" || *q.Period != 300 ||
		*q.Metric.Dimensions[0].Name != "InstanceId" || *q.Metric.Dimensions[0].Value != "i-1" {
		t.Errorf("unexpected query: %+v", q)
	}

	series, _ = newTestMetrics(&fakeCloudWatch{}).InstanceMetrics(context.Background(), domain.Instance{ID: "i-1", Type: "m7g.large"}, threeHours)
	if len(series) != 3 {
		t.Error("non-burstable instances have no CPU credits")
	}
}

func TestTaskMetricsUseRatioExpressions(t *testing.T) {
	f := &fakeCloudWatch{}
	_, err := newTestMetrics(f).TaskMetrics(context.Background(),
		domain.Task{ID: "abc", Cluster: "prod", TaskDefinition: "api:7"}, threeHours)
	if err != nil {
		t.Fatal(err)
	}
	var expr string
	var dims []string
	for _, q := range f.inputs[0].MetricDataQueries {
		if q.Expression != nil && *q.Id == "m0" {
			expr = *q.Expression
		}
		if q.MetricStat != nil && *q.Id == "m0u" {
			for _, d := range q.MetricStat.Metric.Dimensions {
				dims = append(dims, *d.Name+"="+*d.Value)
			}
			if *q.ReturnData {
				t.Error("helper queries must not be returned")
			}
		}
	}
	if expr != "100 * m0u / m0r" {
		t.Errorf("expression = %q", expr)
	}
	if got := strings.Join(dims, ","); got != "ClusterName=prod,TaskDefinitionFamily=api,TaskId=abc" {
		t.Errorf("dimensions = %s", got)
	}
}

func TestCacheMetricsDimensions(t *testing.T) {
	tests := []struct {
		cache domain.Cache
		dim   string
		name  string
	}{
		{domain.Cache{ID: "sessions", Engine: "valkey", Members: []string{"sessions-001", "sessions-002"}}, "CacheClusterId=sessions-001", "EngineCPUUtilization"},
		{domain.Cache{ID: "memo", Engine: "memcached"}, "CacheClusterId=memo", "CPUUtilization"},
		{domain.Cache{ID: "burst", Engine: "redis", Serverless: true}, "clusterId=burst", "ElastiCacheProcessingUnits"},
	}
	for _, tt := range tests {
		f := &fakeCloudWatch{}
		newTestMetrics(f).CacheMetrics(context.Background(), tt.cache, threeHours)
		q := f.inputs[0].MetricDataQueries[0].MetricStat
		dim := *q.Metric.Dimensions[0].Name + "=" + *q.Metric.Dimensions[0].Value
		if dim != tt.dim || *q.Metric.MetricName != tt.name {
			t.Errorf("%s: got %s %s, want %s %s", tt.cache.ID, *q.Metric.MetricName, dim, tt.name, tt.dim)
		}
	}
}
