package domain

// ResourceKind names a type of resource that can be bookmarked.
type ResourceKind string

const (
	KindInstance     ResourceKind = "instance"
	KindECSCluster   ResourceKind = "ecs-cluster"
	KindECSService   ResourceKind = "ecs-service"
	KindTable        ResourceKind = "table"
	KindCache        ResourceKind = "cache"
	KindQueue        ResourceKind = "queue"
	KindLoadBalancer ResourceKind = "load-balancer"
	KindParameter    ResourceKind = "parameter"
	KindSecret       ResourceKind = "secret"
)

// Label is a readable name of the kind, e.g. "ECS service".
func (k ResourceKind) Label() string {
	switch k {
	case KindInstance:
		return "instance"
	case KindECSCluster:
		return "ECS cluster"
	case KindECSService:
		return "ECS service"
	case KindTable:
		return "table"
	case KindCache:
		return "cache"
	case KindQueue:
		return "queue"
	case KindLoadBalancer:
		return "load balancer"
	case KindParameter:
		return "parameter"
	case KindSecret:
		return "secret"
	}
	return string(k)
}

// Bookmark points at a resource of the current profile and region: a
// favorite or a recently visited one.
type Bookmark struct {
	Kind   ResourceKind `json:"kind"`
	ID     string       `json:"id"`
	Parent string       `json:"parent,omitempty"` // e.g. the cluster of an ECS service
	Label  string       `json:"label,omitempty"`
}

// Same reports whether b and o point at the same resource.
func (b Bookmark) Same(o Bookmark) bool {
	return b.Kind == o.Kind && b.ID == o.ID && b.Parent == o.Parent
}
