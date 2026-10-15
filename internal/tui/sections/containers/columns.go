package containers

import (
	"fmt"
	"strconv"
	"time"

	"infra/internal/domain"
	"infra/internal/filter"
	"infra/internal/tui/components/listview"
)

var clusterColumns = []listview.Column[domain.Cluster]{
	{Title: "Cluster", Value: func(c domain.Cluster) string { return c.Name }},
	{Title: "Status", Width: 10, Value: func(c domain.Cluster) string { return c.Status }},
	{Title: "Services", Width: 9, Value: func(c domain.Cluster) string { return strconv.Itoa(c.ActiveServices) }},
	{Title: "Running", Width: 8, Value: func(c domain.Cluster) string { return strconv.Itoa(c.RunningTasks) }},
	{Title: "Pending", Width: 8, Value: func(c domain.Cluster) string { return strconv.Itoa(c.PendingTasks) }},
}

var serviceColumns = []listview.Column[domain.Service]{
	{Title: "Service", Value: func(s domain.Service) string { return s.Name }},
	{Title: "Status", Width: 9, Value: func(s domain.Service) string { return s.Status }},
	{Title: "Tasks", Width: 7, Value: func(s domain.Service) string { return fmt.Sprintf("%d/%d", s.Running, s.Desired) }},
	{Title: "Task definition", Width: 24, Value: func(s domain.Service) string { return s.TaskDefinition }},
	{Title: "Launch", Width: 12, Value: func(s domain.Service) string { return s.LaunchType }},
	{Title: "Exec", Width: 4, Value: func(s domain.Service) string { return yesNo(s.ExecEnabled) }},
}

var taskColumns = []listview.Column[domain.Task]{
	{Title: "Task", Value: func(t domain.Task) string { return t.ID }},
	{Title: "Service", Width: 20, Value: func(t domain.Task) string { return t.Service }},
	{Title: "Status", Width: 10, Value: func(t domain.Task) string { return t.LastStatus }},
	{Title: "Exec", Width: 4, Value: func(t domain.Task) string { return yesNo(t.ExecEnabled) }},
	{Title: "Private IP", Width: 15, Value: func(t domain.Task) string { return t.PrivateIP }},
	{Title: "Task definition", Width: 24, Value: func(t domain.Task) string { return t.TaskDefinition }},
	{Title: "Started", Width: 8, Value: func(t domain.Task) string { return age(t.StartedAt) }},
}

const (
	clusterPlaceholder = "name, status:active"
	servicePlaceholder = "name, status:active, launch:fargate, def:api, exec:yes"
	taskPlaceholder    = "id, service:api, status:running, ip:10.0, container:app, image:nginx"
)

func clusterFields(c domain.Cluster) []filter.Field {
	return []filter.Field{{Name: "name", Value: c.Name}, {Name: "status", Value: c.Status}}
}

func serviceFields(s domain.Service) []filter.Field {
	return []filter.Field{
		{Name: "name", Value: s.Name},
		{Name: "status", Value: s.Status},
		{Name: "launch", Value: s.LaunchType},
		{Name: "def", Value: s.TaskDefinition},
		{Name: "exec", Value: yesNo(s.ExecEnabled)},
	}
}

func taskFields(t domain.Task) []filter.Field {
	fields := []filter.Field{
		{Name: "id", Value: t.ID},
		{Name: "service", Value: t.Service},
		{Name: "status", Value: t.LastStatus},
		{Name: "health", Value: t.Health},
		{Name: "launch", Value: t.LaunchType},
		{Name: "def", Value: t.TaskDefinition},
		{Name: "ip", Value: t.PrivateIP},
		{Name: "az", Value: t.AvailabilityZone},
		{Name: "exec", Value: yesNo(t.ExecEnabled)},
	}
	for _, c := range t.Containers {
		fields = append(fields,
			filter.Field{Name: "container", Value: c.Name},
			filter.Field{Name: "image", Value: c.Image},
		)
	}
	return fields
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

// age renders how long ago t was, e.g. "3h" or "12d".
func age(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	return fmt.Sprintf("%dd", int(d.Hours()/24))
}
