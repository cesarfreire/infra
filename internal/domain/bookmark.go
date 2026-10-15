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
)

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
