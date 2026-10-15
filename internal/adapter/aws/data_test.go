package aws

import (
	"context"
	"encoding/json"
	"slices"
	"sync"
	"testing"

	sdkaws "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	dtypes "github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/aws/aws-sdk-go-v2/service/elasticache"
	etypes "github.com/aws/aws-sdk-go-v2/service/elasticache/types"

	"infra/internal/domain"
)

type fakeDynamo struct {
	mu     sync.Mutex
	scan   *dynamodb.ScanInput
	query  *dynamodb.QueryInput
	put    *dynamodb.PutItemInput
	putErr error
	del    *dynamodb.DeleteItemInput
}

func (f *fakeDynamo) ListTables(context.Context, *dynamodb.ListTablesInput, ...func(*dynamodb.Options)) (*dynamodb.ListTablesOutput, error) {
	return &dynamodb.ListTablesOutput{TableNames: []string{"orders", "users"}}, nil
}

func (f *fakeDynamo) DescribeTable(_ context.Context, in *dynamodb.DescribeTableInput, _ ...func(*dynamodb.Options)) (*dynamodb.DescribeTableOutput, error) {
	return &dynamodb.DescribeTableOutput{Table: &dtypes.TableDescription{
		TableName:   in.TableName,
		TableStatus: dtypes.TableStatusActive,
		ItemCount:   sdkaws.Int64(42),
		AttributeDefinitions: []dtypes.AttributeDefinition{
			{AttributeName: sdkaws.String("pk"), AttributeType: dtypes.ScalarAttributeTypeS},
			{AttributeName: sdkaws.String("sk"), AttributeType: dtypes.ScalarAttributeTypeN},
		},
		KeySchema: []dtypes.KeySchemaElement{
			{AttributeName: sdkaws.String("pk"), KeyType: dtypes.KeyTypeHash},
			{AttributeName: sdkaws.String("sk"), KeyType: dtypes.KeyTypeRange},
		},
		BillingModeSummary: &dtypes.BillingModeSummary{BillingMode: dtypes.BillingModePayPerRequest},
		GlobalSecondaryIndexes: []dtypes.GlobalSecondaryIndexDescription{{
			IndexName: sdkaws.String("by-sk"),
			KeySchema: []dtypes.KeySchemaElement{{AttributeName: sdkaws.String("sk"), KeyType: dtypes.KeyTypeHash}},
		}},
	}}, nil
}

func (f *fakeDynamo) Scan(_ context.Context, in *dynamodb.ScanInput, _ ...func(*dynamodb.Options)) (*dynamodb.ScanOutput, error) {
	f.mu.Lock()
	f.scan = in
	f.mu.Unlock()
	return &dynamodb.ScanOutput{
		Items: []map[string]dtypes.AttributeValue{{
			"pk":    &dtypes.AttributeValueMemberS{Value: "u1"},
			"big":   &dtypes.AttributeValueMemberN{Value: "12345678901234567890"},
			"ok":    &dtypes.AttributeValueMemberBOOL{Value: true},
			"none":  &dtypes.AttributeValueMemberNULL{Value: true},
			"tags":  &dtypes.AttributeValueMemberSS{Value: []string{"a", "b"}},
			"inner": &dtypes.AttributeValueMemberM{Value: map[string]dtypes.AttributeValue{"n": &dtypes.AttributeValueMemberN{Value: "1.5"}}},
			"list":  &dtypes.AttributeValueMemberL{Value: []dtypes.AttributeValue{&dtypes.AttributeValueMemberS{Value: "x"}}},
		}},
		LastEvaluatedKey: map[string]dtypes.AttributeValue{"pk": &dtypes.AttributeValueMemberS{Value: "u1"}},
	}, nil
}

func (f *fakeDynamo) Query(_ context.Context, in *dynamodb.QueryInput, _ ...func(*dynamodb.Options)) (*dynamodb.QueryOutput, error) {
	f.query = in
	return &dynamodb.QueryOutput{}, nil
}

func TestListTables(t *testing.T) {
	got, err := NewKeyValueRepository(&fakeDynamo{}).ListTables(context.Background())
	if err != nil || len(got) != 2 || got[0].Name != "orders" || got[1].Name != "users" {
		t.Fatalf("ListTables() = %+v, %v", got, err)
	}
	tbl := got[0]
	if tbl.PartitionKey != (domain.KeyAttribute{Name: "pk", Type: "S"}) || tbl.SortKey == nil || *tbl.SortKey != (domain.KeyAttribute{Name: "sk", Type: "N"}) {
		t.Errorf("key schema = %+v / %+v", tbl.PartitionKey, tbl.SortKey)
	}
	if tbl.BillingMode != "PAY_PER_REQUEST" || tbl.ItemCount != 42 || len(tbl.Indexes) != 1 || !tbl.Indexes[0].Global {
		t.Errorf("unexpected mapping: %+v", tbl)
	}
}

func TestScanConvertsValuesAndPaginates(t *testing.T) {
	f := &fakeDynamo{}
	repo := NewKeyValueRepository(f)
	page, err := repo.Scan(context.Background(), domain.Table{Name: "users"}, nil, 25)
	if err != nil {
		t.Fatal(err)
	}
	if *f.scan.Limit != 25 || f.scan.ExclusiveStartKey != nil {
		t.Errorf("scan input = %+v", f.scan)
	}
	got, _ := json.Marshal(page.Items[0]) // sets marshal as arrays
	want := `{"big":12345678901234567890,"inner":{"n":1.5},"list":["x"],"none":null,"ok":true,"pk":"u1","tags":["a","b"]}`
	if string(got) != want {
		t.Errorf("item = %s\nwant   %s", got, want)
	}
	if page.Next == nil {
		t.Fatal("expected a cursor")
	}
	repo.Scan(context.Background(), domain.Table{Name: "users"}, page.Next, 25)
	if f.scan.ExclusiveStartKey == nil {
		t.Error("the cursor should be sent as ExclusiveStartKey")
	}
}

func TestQueryBuildsKeyCondition(t *testing.T) {
	f := &fakeDynamo{}
	table := domain.Table{Name: "orders", PartitionKey: domain.KeyAttribute{Name: "customer", Type: "N"},
		SortKey: &domain.KeyAttribute{Name: "date", Type: "S"}}
	_, err := NewKeyValueRepository(f).Query(context.Background(), table, domain.KeyQuery{PartitionValue: "7", SortKeyPrefix: "2026-"}, nil, 10)
	if err != nil {
		t.Fatal(err)
	}
	if *f.query.KeyConditionExpression != "#pk = :pk AND begins_with(#sk, :sk)" ||
		f.query.ExpressionAttributeNames["#pk"] != "customer" || f.query.ExpressionAttributeNames["#sk"] != "date" {
		t.Errorf("query = %+v", f.query)
	}
	if n, ok := f.query.ExpressionAttributeValues[":pk"].(*dtypes.AttributeValueMemberN); !ok || n.Value != "7" {
		t.Errorf("a numeric partition key must be sent as N, got %#v", f.query.ExpressionAttributeValues[":pk"])
	}
}

type fakeElastiCache struct{}

func (fakeElastiCache) DescribeCacheClusters(context.Context, *elasticache.DescribeCacheClustersInput, ...func(*elasticache.Options)) (*elasticache.DescribeCacheClustersOutput, error) {
	return &elasticache.DescribeCacheClustersOutput{CacheClusters: []etypes.CacheCluster{
		{CacheClusterId: sdkaws.String("sessions-001"), ReplicationGroupId: sdkaws.String("sessions"), EngineVersion: sdkaws.String("7.1.0")},
		{CacheClusterId: sdkaws.String("memo"), Engine: sdkaws.String("memcached"), CacheClusterStatus: sdkaws.String("available"),
			NumCacheNodes:         sdkaws.Int32(2),
			ConfigurationEndpoint: &etypes.Endpoint{Address: sdkaws.String("memo.cfg.cache.amazonaws.com"), Port: sdkaws.Int32(11211)}},
	}}, nil
}

func (fakeElastiCache) DescribeReplicationGroups(context.Context, *elasticache.DescribeReplicationGroupsInput, ...func(*elasticache.Options)) (*elasticache.DescribeReplicationGroupsOutput, error) {
	return &elasticache.DescribeReplicationGroupsOutput{ReplicationGroups: []etypes.ReplicationGroup{{
		ReplicationGroupId:       sdkaws.String("sessions"),
		Engine:                   sdkaws.String("valkey"),
		Status:                   sdkaws.String("available"),
		MemberClusters:           []string{"sessions-001", "sessions-002"},
		TransitEncryptionEnabled: sdkaws.Bool(true),
		NodeGroups: []etypes.NodeGroup{{PrimaryEndpoint: &etypes.Endpoint{
			Address: sdkaws.String("master.sessions.cache.amazonaws.com"), Port: sdkaws.Int32(6379)}}},
	}}}, nil
}

func (fakeElastiCache) DescribeServerlessCaches(context.Context, *elasticache.DescribeServerlessCachesInput, ...func(*elasticache.Options)) (*elasticache.DescribeServerlessCachesOutput, error) {
	return &elasticache.DescribeServerlessCachesOutput{ServerlessCaches: []etypes.ServerlessCache{{
		ServerlessCacheName: sdkaws.String("burst"), Engine: sdkaws.String("redis"), Status: sdkaws.String("available"),
		Endpoint: &etypes.Endpoint{Address: sdkaws.String("burst.serverless.cache.amazonaws.com"), Port: sdkaws.Int32(6379)},
	}}}, nil
}

func TestListCaches(t *testing.T) {
	got, err := NewCacheRepository(fakeElastiCache{}).ListCaches(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]domain.Cache{}
	for _, c := range got {
		byID[c.ID] = c
	}
	if len(got) != 3 || byID["sessions-001"].ID != "" {
		t.Fatalf("replication group members must not be listed on their own: %+v", got)
	}
	rg := byID["sessions"]
	if rg.Engine != "valkey" || rg.EngineVersion != "7.1.0" || rg.Nodes != 2 || !rg.TLS || rg.Endpoint.Host != "master.sessions.cache.amazonaws.com" {
		t.Errorf("replication group = %+v", rg)
	}
	if m := byID["memo"]; m.Engine != "memcached" || m.Endpoint.Port != 11211 {
		t.Errorf("memcached = %+v", m)
	}
	if s := byID["burst"]; !s.Serverless || !s.TLS {
		t.Errorf("serverless = %+v", s)
	}
}

func TestRemoteHostSessions(t *testing.T) {
	b := NewSessionCommandBuilder("", "", Shells{})
	pf := b.PortForwardSession(domain.PortForward{Target: "i-1", RemoteHost: "redis.local", RemotePort: 6379, LocalPort: 16379})
	if i := slices.Index(pf.Args, "--document-name"); pf.Args[i+1] != "AWS-StartPortForwardingSessionToRemoteHost" {
		t.Fatalf("args = %v", pf.Args)
	}
	var params map[string][]string
	json.Unmarshal([]byte(pf.Args[slices.Index(pf.Args, "--parameters")+1]), &params)
	if params["host"][0] != "redis.local" || params["portNumber"][0] != "6379" || params["localPortNumber"][0] != "16379" {
		t.Errorf("params = %v", params)
	}

	cmd := b.RemoteCommandSession("i-1", "redis-cli -h x")
	json.Unmarshal([]byte(cmd.Args[slices.Index(cmd.Args, "--parameters")+1]), &params)
	if params["command"][0] != "redis-cli -h x" {
		t.Errorf("params = %v", params)
	}
}

func TestDataConsoleURLs(t *testing.T) {
	b := NewConsoleURLBuilder("us-east-1")
	if got := b.TableURL("users"); got != "https://us-east-1.console.aws.amazon.com/dynamodbv2/home?region=us-east-1#table?name=users" {
		t.Errorf("TableURL() = %s", got)
	}
	if got := b.CacheURL(domain.Cache{ID: "burst", Serverless: true}); got != "https://us-east-1.console.aws.amazon.com/elasticache/home?region=us-east-1#/serverless/burst" {
		t.Errorf("CacheURL() = %s", got)
	}
}
