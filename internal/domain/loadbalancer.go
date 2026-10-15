package domain

import "time"

// LoadBalancer is an application, network or gateway load balancer.
type LoadBalancer struct {
	Name      string
	ARN       string
	Type      string // application, network, gateway
	Scheme    string // internet-facing, internal
	State     string
	DNSName   string
	VPC       string
	Zones     []string
	CreatedAt time.Time
}

// Listener is a port a load balancer accepts connections on.
type Listener struct {
	Protocol string
	Port     int
	Action   string // e.g. "forward to api-tg", "redirect to HTTPS:443"
}

// TargetGroup routes requests to targets and checks their health.
type TargetGroup struct {
	Name        string
	ARN         string
	Protocol    string
	Port        int
	TargetType  string // instance, ip, lambda, alb
	HealthCheck string // e.g. "HTTP /health"
	Targets     []Target
}

// Target is a member of a target group and its health.
type Target struct {
	ID          string
	Port        int
	Zone        string
	State       string // healthy, unhealthy, initial, draining, unused...
	Reason      string
	Description string
}

// Healthy counts the healthy targets.
func (g TargetGroup) Healthy() int {
	n := 0
	for _, t := range g.Targets {
		if t.State == "healthy" {
			n++
		}
	}
	return n
}
