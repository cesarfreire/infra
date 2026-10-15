package domain

// Cache engines.
const (
	EngineRedis     = "redis"
	EngineValkey    = "valkey"
	EngineMemcached = "memcached"
)

// Cache is an in-memory data store (ElastiCache replication group, cache
// cluster or serverless cache).
type Cache struct {
	ID            string
	ARN           string
	Engine        string
	EngineVersion string
	Status        string
	NodeType      string // empty for serverless
	Nodes         int
	Endpoint      Endpoint
	TLS           bool
	ClusterMode   bool
	Serverless    bool
	Description   string
	// Members are the node clusters of a replication group (metrics are
	// reported per member); empty for standalone and serverless caches.
	Members []string
}

// Endpoint is a network address.
type Endpoint struct {
	Host string
	Port int
}

// IsAvailable reports whether the cache accepts connections.
func (c Cache) IsAvailable() bool { return c.Status == "available" }
