package aws

import (
	"cmp"
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	sdkaws "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatch"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatch/types"

	"infra/internal/domain"
)

// Metrics reads CloudWatch metrics. It implements every app.*MetricsSource.
type Metrics struct {
	client cloudwatch.GetMetricDataAPIClient
	now    func() time.Time
}

// NewMetrics returns a metrics reader backed by client.
func NewMetrics(client cloudwatch.GetMetricDataAPIClient) *Metrics {
	return &Metrics{client: client, now: time.Now}
}

// metric describes one chart.
type metric struct {
	label     string
	unit      domain.Unit
	namespace string
	name      string
	dims      map[string]string
	stat      string
	// rate divides each value (a Sum over the period) by the period, giving
	// a per-second rate.
	rate bool
	// ratioOf, when set, plots 100 * name / ratioOf (e.g. used / reserved).
	ratioOf string
}

// InstanceMetrics returns CPU and network usage, plus CPU credits for
// burstable (t*) instances.
func (m *Metrics) InstanceMetrics(ctx context.Context, inst domain.Instance, w domain.MetricWindow) ([]domain.Series, error) {
	dims := map[string]string{"InstanceId": inst.ID}
	specs := []metric{
		{label: "CPU", unit: domain.UnitPercent, namespace: "AWS/EC2", name: "CPUUtilization", dims: dims, stat: "Average"},
		{label: "Network in", unit: domain.UnitBytesPerSecond, namespace: "AWS/EC2", name: "NetworkIn", dims: dims, stat: "Sum", rate: true},
		{label: "Network out", unit: domain.UnitBytesPerSecond, namespace: "AWS/EC2", name: "NetworkOut", dims: dims, stat: "Sum", rate: true},
	}
	if strings.HasPrefix(inst.Type, "t") {
		specs = append(specs, metric{label: "CPU credits", unit: domain.UnitCount, namespace: "AWS/EC2", name: "CPUCreditBalance", dims: dims, stat: "Average"})
	}
	return m.fetch(ctx, w, specs)
}

// ServiceMetrics returns the average CPU and memory use of a service's
// tasks, relative to what they reserve.
func (m *Metrics) ServiceMetrics(ctx context.Context, s domain.Service, w domain.MetricWindow) ([]domain.Series, error) {
	dims := map[string]string{"ClusterName": s.Cluster, "ServiceName": s.Name}
	return m.fetch(ctx, w, []metric{
		{label: "CPU", unit: domain.UnitPercent, namespace: "AWS/ECS", name: "CPUUtilization", dims: dims, stat: "Average"},
		{label: "Memory", unit: domain.UnitPercent, namespace: "AWS/ECS", name: "MemoryUtilization", dims: dims, stat: "Average"},
	})
}

// TaskMetrics returns a task's CPU, memory and network use. Task-level
// metrics only exist with Container Insights enhanced observability.
func (m *Metrics) TaskMetrics(ctx context.Context, t domain.Task, w domain.MetricWindow) ([]domain.Series, error) {
	family, _, _ := strings.Cut(t.TaskDefinition, ":")
	dims := map[string]string{"ClusterName": t.Cluster, "TaskDefinitionFamily": family, "TaskId": t.ID}
	const ns = "ECS/ContainerInsights"
	return m.fetch(ctx, w, []metric{
		{label: "CPU", unit: domain.UnitPercent, namespace: ns, name: "CpuUtilized", ratioOf: "CpuReserved", dims: dims, stat: "Average"},
		{label: "Memory", unit: domain.UnitPercent, namespace: ns, name: "MemoryUtilized", ratioOf: "MemoryReserved", dims: dims, stat: "Average"},
		{label: "Network in", unit: domain.UnitBytesPerSecond, namespace: ns, name: "NetworkRxBytes", dims: dims, stat: "Average"},
		{label: "Network out", unit: domain.UnitBytesPerSecond, namespace: ns, name: "NetworkTxBytes", dims: dims, stat: "Average"},
	})
}

// TableMetrics returns consumed capacity and throttling of a table.
func (m *Metrics) TableMetrics(ctx context.Context, t domain.Table, w domain.MetricWindow) ([]domain.Series, error) {
	dims := map[string]string{"TableName": t.Name}
	const ns = "AWS/DynamoDB"
	return m.fetch(ctx, w, []metric{
		{label: "Read capacity", unit: domain.UnitPerSecond, namespace: ns, name: "ConsumedReadCapacityUnits", dims: dims, stat: "Sum", rate: true},
		{label: "Write capacity", unit: domain.UnitPerSecond, namespace: ns, name: "ConsumedWriteCapacityUnits", dims: dims, stat: "Sum", rate: true},
		{label: "Read throttles", unit: domain.UnitCount, namespace: ns, name: "ReadThrottleEvents", dims: dims, stat: "Sum"},
		{label: "Write throttles", unit: domain.UnitCount, namespace: ns, name: "WriteThrottleEvents", dims: dims, stat: "Sum"},
	})
}

// CacheMetrics returns load, memory, connections and hit rate. Replication
// groups report per member, so the first member (the initial primary) is used.
func (m *Metrics) CacheMetrics(ctx context.Context, c domain.Cache, w domain.MetricWindow) ([]domain.Series, error) {
	const ns = "AWS/ElastiCache"
	switch {
	case c.Serverless:
		dims := map[string]string{"clusterId": c.ID}
		return m.fetch(ctx, w, []metric{
			{label: "ECPUs", unit: domain.UnitPerSecond, namespace: ns, name: "ElastiCacheProcessingUnits", dims: dims, stat: "Sum", rate: true},
			{label: "Data stored", unit: domain.UnitBytes, namespace: ns, name: "BytesUsedForCache", dims: dims, stat: "Average"},
			{label: "Connections", unit: domain.UnitCount, namespace: ns, name: "CurrConnections", dims: dims, stat: "Average"},
			{label: "Hit rate", unit: domain.UnitPercent, namespace: ns, name: "CacheHitRate", dims: dims, stat: "Average"},
		})
	case c.Engine == domain.EngineMemcached:
		dims := map[string]string{"CacheClusterId": c.ID}
		return m.fetch(ctx, w, []metric{
			{label: "CPU", unit: domain.UnitPercent, namespace: ns, name: "CPUUtilization", dims: dims, stat: "Average"},
			{label: "Memory used", unit: domain.UnitBytes, namespace: ns, name: "BytesUsedForCacheItems", dims: dims, stat: "Average"},
			{label: "Connections", unit: domain.UnitCount, namespace: ns, name: "CurrConnections", dims: dims, stat: "Average"},
			{label: "Evictions", unit: domain.UnitCount, namespace: ns, name: "Evictions", dims: dims, stat: "Sum"},
		})
	}
	node := c.ID
	if len(c.Members) > 0 {
		node = c.Members[0]
	}
	dims := map[string]string{"CacheClusterId": node}
	return m.fetch(ctx, w, []metric{
		{label: "Engine CPU", unit: domain.UnitPercent, namespace: ns, name: "EngineCPUUtilization", dims: dims, stat: "Average"},
		{label: "Memory", unit: domain.UnitPercent, namespace: ns, name: "DatabaseMemoryUsagePercentage", dims: dims, stat: "Average"},
		{label: "Connections", unit: domain.UnitCount, namespace: ns, name: "CurrConnections", dims: dims, stat: "Average"},
		{label: "Hit rate", unit: domain.UnitPercent, namespace: ns, name: "CacheHitRate", dims: dims, stat: "Average"},
	})
}

// QueueMetrics returns backlog, age of the oldest message and throughput.
func (m *Metrics) QueueMetrics(ctx context.Context, q domain.Queue, w domain.MetricWindow) ([]domain.Series, error) {
	dims := map[string]string{"QueueName": q.Name}
	const ns = "AWS/SQS"
	return m.fetch(ctx, w, []metric{
		{label: "Messages visible", unit: domain.UnitCount, namespace: ns, name: "ApproximateNumberOfMessagesVisible", dims: dims, stat: "Maximum"},
		{label: "Oldest message age", unit: domain.UnitSeconds, namespace: ns, name: "ApproximateAgeOfOldestMessage", dims: dims, stat: "Maximum"},
		{label: "Sent", unit: domain.UnitPerSecond, namespace: ns, name: "NumberOfMessagesSent", dims: dims, stat: "Sum", rate: true},
		{label: "Deleted", unit: domain.UnitPerSecond, namespace: ns, name: "NumberOfMessagesDeleted", dims: dims, stat: "Sum", rate: true},
	})
}

// LoadBalancerMetrics returns traffic, latency and errors. Gateway load
// balancers have none of these and return no series.
func (m *Metrics) LoadBalancerMetrics(ctx context.Context, lb domain.LoadBalancer, w domain.MetricWindow) ([]domain.Series, error) {
	// The dimension is the ARN suffix after "loadbalancer/": app/name/id.
	_, id, _ := strings.Cut(lb.ARN, ":loadbalancer/")
	dims := map[string]string{"LoadBalancer": id}
	switch lb.Type {
	case "application":
		const ns = "AWS/ApplicationELB"
		return m.fetch(ctx, w, []metric{
			{label: "Requests", unit: domain.UnitPerSecond, namespace: ns, name: "RequestCount", dims: dims, stat: "Sum", rate: true},
			{label: "Target response time", unit: domain.UnitSeconds, namespace: ns, name: "TargetResponseTime", dims: dims, stat: "Average"},
			{label: "Target 5XX", unit: domain.UnitCount, namespace: ns, name: "HTTPCode_Target_5XX_Count", dims: dims, stat: "Sum"},
			{label: "Load balancer 5XX", unit: domain.UnitCount, namespace: ns, name: "HTTPCode_ELB_5XX_Count", dims: dims, stat: "Sum"},
		})
	case "network":
		const ns = "AWS/NetworkELB"
		return m.fetch(ctx, w, []metric{
			{label: "Active flows", unit: domain.UnitCount, namespace: ns, name: "ActiveFlowCount", dims: dims, stat: "Average"},
			{label: "New flows", unit: domain.UnitPerSecond, namespace: ns, name: "NewFlowCount", dims: dims, stat: "Sum", rate: true},
			{label: "Processed", unit: domain.UnitBytesPerSecond, namespace: ns, name: "ProcessedBytes", dims: dims, stat: "Sum", rate: true},
			{label: "Target resets", unit: domain.UnitCount, namespace: ns, name: "TCP_Target_Reset_Count", dims: dims, stat: "Sum"},
		})
	}
	return nil, nil
}

// fetch reads every metric of specs in one GetMetricData call (paginated).
func (m *Metrics) fetch(ctx context.Context, w domain.MetricWindow, specs []metric) ([]domain.Series, error) {
	period := int32(w.Period.Seconds())
	var queries []types.MetricDataQuery
	stat := func(id, name string, s metric, visible bool) types.MetricDataQuery {
		return types.MetricDataQuery{
			Id:         sdkaws.String(id),
			ReturnData: sdkaws.Bool(visible),
			MetricStat: &types.MetricStat{
				Metric: &types.Metric{Namespace: &s.namespace, MetricName: sdkaws.String(name), Dimensions: dimensions(s.dims)},
				Period: &period,
				Stat:   &s.stat,
			},
		}
	}
	for i, s := range specs {
		id := fmt.Sprintf("m%d", i)
		if s.ratioOf == "" {
			queries = append(queries, stat(id, s.name, s, true))
			continue
		}
		queries = append(queries,
			stat(id+"u", s.name, s, false),
			stat(id+"r", s.ratioOf, s, false),
			types.MetricDataQuery{
				Id:         sdkaws.String(id),
				Expression: sdkaws.String(fmt.Sprintf("100 * %su / %sr", id, id)),
				ReturnData: sdkaws.Bool(true),
			},
		)
	}

	end := m.now().Truncate(w.Period).Add(w.Period)
	in := &cloudwatch.GetMetricDataInput{
		MetricDataQueries: queries,
		StartTime:         sdkaws.Time(end.Add(-w.Range)),
		EndTime:           &end,
		ScanBy:            types.ScanByTimestampAscending,
	}
	points := map[string][]domain.Point{}
	pages := cloudwatch.NewGetMetricDataPaginator(m.client, in)
	for pages.HasMorePages() {
		page, err := pages.NextPage(ctx)
		if err != nil {
			return nil, classify(err)
		}
		for _, r := range page.MetricDataResults {
			id := sdkaws.ToString(r.Id)
			for j, ts := range r.Timestamps {
				if j < len(r.Values) {
					points[id] = append(points[id], domain.Point{Time: ts, Value: r.Values[j]})
				}
			}
		}
	}

	series := make([]domain.Series, len(specs))
	for i, s := range specs {
		ps := points[fmt.Sprintf("m%d", i)]
		slices.SortFunc(ps, func(a, b domain.Point) int { return a.Time.Compare(b.Time) })
		if s.rate {
			for j := range ps {
				ps[j].Value /= w.Period.Seconds()
			}
		}
		series[i] = domain.Series{Label: s.label, Unit: s.unit, Points: ps}
	}
	return series, nil
}

func dimensions(dims map[string]string) []types.Dimension {
	names := slices.SortedFunc(maps.Keys(dims), cmp.Compare[string])
	out := make([]types.Dimension, len(names))
	for i, n := range names {
		out[i] = types.Dimension{Name: sdkaws.String(n), Value: sdkaws.String(dims[n])}
	}
	return out
}
