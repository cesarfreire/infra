package aws

import (
	"context"

	sdkaws "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/elasticache"
	"github.com/aws/aws-sdk-go-v2/service/elasticache/types"

	"infra/internal/domain"
)

// ElastiCacheAPI is the subset of *elasticache.Client used.
type ElastiCacheAPI interface {
	elasticache.DescribeReplicationGroupsAPIClient
	elasticache.DescribeCacheClustersAPIClient
	elasticache.DescribeServerlessCachesAPIClient
}

// CacheRepository lists ElastiCache caches. It implements app.CacheRepository.
type CacheRepository struct {
	client ElastiCacheAPI
}

// NewCacheRepository returns a repository backed by client.
func NewCacheRepository(client ElastiCacheAPI) *CacheRepository {
	return &CacheRepository{client: client}
}

// ListCaches returns replication groups (Redis/Valkey), standalone cache
// clusters (memcached, single-node Redis) and serverless caches.
func (r *CacheRepository) ListCaches(ctx context.Context) ([]domain.Cache, error) {
	var clusters []types.CacheCluster
	cp := elasticache.NewDescribeCacheClustersPaginator(r.client, &elasticache.DescribeCacheClustersInput{ShowCacheNodeInfo: sdkaws.Bool(true)})
	for cp.HasMorePages() {
		page, err := cp.NextPage(ctx)
		if err != nil {
			return nil, classify(err)
		}
		clusters = append(clusters, page.CacheClusters...)
	}

	var caches []domain.Cache
	// Replication groups don't report the engine version; their members do.
	versions := map[string]string{}
	for _, c := range clusters {
		if rg := sdkaws.ToString(c.ReplicationGroupId); rg != "" {
			versions[rg] = sdkaws.ToString(c.EngineVersion)
			continue
		}
		caches = append(caches, fromCacheCluster(c))
	}

	rp := elasticache.NewDescribeReplicationGroupsPaginator(r.client, &elasticache.DescribeReplicationGroupsInput{})
	for rp.HasMorePages() {
		page, err := rp.NextPage(ctx)
		if err != nil {
			return nil, classify(err)
		}
		for _, rg := range page.ReplicationGroups {
			c := fromReplicationGroup(rg)
			c.EngineVersion = versions[c.ID]
			caches = append(caches, c)
		}
	}

	sp := elasticache.NewDescribeServerlessCachesPaginator(r.client, &elasticache.DescribeServerlessCachesInput{})
	for sp.HasMorePages() {
		page, err := sp.NextPage(ctx)
		if err != nil {
			return nil, classify(err)
		}
		for _, sc := range page.ServerlessCaches {
			caches = append(caches, fromServerless(sc))
		}
	}
	return caches, nil
}

func fromReplicationGroup(rg types.ReplicationGroup) domain.Cache {
	c := domain.Cache{
		ID:          sdkaws.ToString(rg.ReplicationGroupId),
		ARN:         sdkaws.ToString(rg.ARN),
		Engine:      sdkaws.ToString(rg.Engine),
		Status:      sdkaws.ToString(rg.Status),
		NodeType:    sdkaws.ToString(rg.CacheNodeType),
		Nodes:       len(rg.MemberClusters),
		TLS:         sdkaws.ToBool(rg.TransitEncryptionEnabled),
		ClusterMode: sdkaws.ToBool(rg.ClusterEnabled),
		Description: sdkaws.ToString(rg.Description),
		Members:     rg.MemberClusters,
	}
	if c.Engine == "" {
		c.Engine = domain.EngineRedis
	}
	switch {
	case rg.ConfigurationEndpoint != nil: // cluster mode enabled
		c.Endpoint = endpoint(rg.ConfigurationEndpoint)
	case len(rg.NodeGroups) > 0:
		c.Endpoint = endpoint(rg.NodeGroups[0].PrimaryEndpoint)
	}
	return c
}

func fromCacheCluster(cc types.CacheCluster) domain.Cache {
	c := domain.Cache{
		ID:            sdkaws.ToString(cc.CacheClusterId),
		ARN:           sdkaws.ToString(cc.ARN),
		Engine:        sdkaws.ToString(cc.Engine),
		EngineVersion: sdkaws.ToString(cc.EngineVersion),
		Status:        sdkaws.ToString(cc.CacheClusterStatus),
		NodeType:      sdkaws.ToString(cc.CacheNodeType),
		Nodes:         int(sdkaws.ToInt32(cc.NumCacheNodes)),
		TLS:           sdkaws.ToBool(cc.TransitEncryptionEnabled),
	}
	switch {
	case cc.ConfigurationEndpoint != nil: // memcached
		c.Endpoint = endpoint(cc.ConfigurationEndpoint)
	case len(cc.CacheNodes) > 0:
		c.Endpoint = endpoint(cc.CacheNodes[0].Endpoint)
	}
	return c
}

func fromServerless(sc types.ServerlessCache) domain.Cache {
	return domain.Cache{
		ID:            sdkaws.ToString(sc.ServerlessCacheName),
		ARN:           sdkaws.ToString(sc.ARN),
		Engine:        sdkaws.ToString(sc.Engine),
		EngineVersion: sdkaws.ToString(sc.FullEngineVersion),
		Status:        sdkaws.ToString(sc.Status),
		Endpoint:      endpoint(sc.Endpoint),
		TLS:           true, // serverless caches always require TLS
		Serverless:    true,
		Description:   sdkaws.ToString(sc.Description),
	}
}

func endpoint(e *types.Endpoint) domain.Endpoint {
	if e == nil {
		return domain.Endpoint{}
	}
	return domain.Endpoint{Host: sdkaws.ToString(e.Address), Port: int(sdkaws.ToInt32(e.Port))}
}
