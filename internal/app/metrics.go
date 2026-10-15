package app

import (
	"context"
	"time"

	"infra/internal/domain"
)

// MetricWindows are the time ranges offered in the monitoring charts.
var MetricWindows = []domain.MetricWindow{
	{Label: "1h", Range: time.Hour, Period: time.Minute},
	{Label: "3h", Range: 3 * time.Hour, Period: 5 * time.Minute},
	{Label: "12h", Range: 12 * time.Hour, Period: 15 * time.Minute},
	{Label: "3d", Range: 3 * 24 * time.Hour, Period: time.Hour},
	{Label: "7d", Range: 7 * 24 * time.Hour, Period: 3 * time.Hour},
}

// DefaultMetricWindow indexes MetricWindows.
const DefaultMetricWindow = 1

// InstanceMetricsSource reads monitoring data of instances.
type InstanceMetricsSource interface {
	InstanceMetrics(ctx context.Context, inst domain.Instance, w domain.MetricWindow) ([]domain.Series, error)
}

// ContainerMetricsSource reads monitoring data of container services and tasks.
type ContainerMetricsSource interface {
	ServiceMetrics(ctx context.Context, s domain.Service, w domain.MetricWindow) ([]domain.Series, error)
	TaskMetrics(ctx context.Context, t domain.Task, w domain.MetricWindow) ([]domain.Series, error)
}

// TableMetricsSource reads monitoring data of key-value tables.
type TableMetricsSource interface {
	TableMetrics(ctx context.Context, t domain.Table, w domain.MetricWindow) ([]domain.Series, error)
}

// CacheMetricsSource reads monitoring data of caches.
type CacheMetricsSource interface {
	CacheMetrics(ctx context.Context, c domain.Cache, w domain.MetricWindow) ([]domain.Series, error)
}

// QueueMetricsSource reads monitoring data of queues.
type QueueMetricsSource interface {
	QueueMetrics(ctx context.Context, q domain.Queue, w domain.MetricWindow) ([]domain.Series, error)
}

// LoadBalancerMetricsSource reads monitoring data of load balancers.
type LoadBalancerMetricsSource interface {
	LoadBalancerMetrics(ctx context.Context, lb domain.LoadBalancer, w domain.MetricWindow) ([]domain.Series, error)
}
