package aws

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"

	sdkaws "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"golang.org/x/sync/errgroup"

	"infra/internal/domain"
)

// describeConcurrency bounds parallel DescribeTable calls.
const describeConcurrency = 8

// DynamoDBAPI is the subset of *dynamodb.Client used.
type DynamoDBAPI interface {
	dynamodb.ListTablesAPIClient
	DescribeTable(ctx context.Context, in *dynamodb.DescribeTableInput, optFns ...func(*dynamodb.Options)) (*dynamodb.DescribeTableOutput, error)
	Scan(ctx context.Context, in *dynamodb.ScanInput, optFns ...func(*dynamodb.Options)) (*dynamodb.ScanOutput, error)
	Query(ctx context.Context, in *dynamodb.QueryInput, optFns ...func(*dynamodb.Options)) (*dynamodb.QueryOutput, error)
	PutItem(ctx context.Context, in *dynamodb.PutItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.PutItemOutput, error)
	DeleteItem(ctx context.Context, in *dynamodb.DeleteItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.DeleteItemOutput, error)
}

// KeyValueRepository reads and writes DynamoDB tables. It implements
// app.KeyValueRepository and app.ItemWriter.
type KeyValueRepository struct {
	client DynamoDBAPI
}

// NewKeyValueRepository returns a repository backed by client.
func NewKeyValueRepository(client DynamoDBAPI) *KeyValueRepository {
	return &KeyValueRepository{client: client}
}

// ListTables returns every table, described in parallel.
func (r *KeyValueRepository) ListTables(ctx context.Context) ([]domain.Table, error) {
	var names []string
	pages := dynamodb.NewListTablesPaginator(r.client, &dynamodb.ListTablesInput{})
	for pages.HasMorePages() {
		page, err := pages.NextPage(ctx)
		if err != nil {
			return nil, classify(err)
		}
		names = append(names, page.TableNames...)
	}

	tables := make([]domain.Table, len(names))
	g, ctx := errgroup.WithContext(ctx)
	g.SetLimit(describeConcurrency)
	for i, name := range names {
		g.Go(func() error {
			out, err := r.client.DescribeTable(ctx, &dynamodb.DescribeTableInput{TableName: &name})
			if err != nil {
				return classify(err)
			}
			tables[i] = toDomainTable(out.Table)
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return nil, err
	}
	return tables, nil
}

// Scan reads up to limit items starting at cursor.
func (r *KeyValueRepository) Scan(ctx context.Context, table domain.Table, cursor domain.Cursor, limit int) (domain.ItemPage, error) {
	in := &dynamodb.ScanInput{TableName: &table.Name, Limit: sdkaws.Int32(int32(limit))}
	if key, ok := cursor.(map[string]types.AttributeValue); ok {
		in.ExclusiveStartKey = key
	}
	out, err := r.client.Scan(ctx, in)
	if err != nil {
		return domain.ItemPage{}, classify(err)
	}
	return toPage(out.Items, out.LastEvaluatedKey), nil
}

// Query reads items by partition key and optional sort key prefix.
func (r *KeyValueRepository) Query(ctx context.Context, table domain.Table, q domain.KeyQuery, cursor domain.Cursor, limit int) (domain.ItemPage, error) {
	pk, err := keyValue(table.PartitionKey.Type, q.PartitionValue)
	if err != nil {
		return domain.ItemPage{}, err
	}
	in := &dynamodb.QueryInput{
		TableName:                 &table.Name,
		Limit:                     sdkaws.Int32(int32(limit)),
		KeyConditionExpression:    sdkaws.String("#pk = :pk"),
		ExpressionAttributeNames:  map[string]string{"#pk": table.PartitionKey.Name},
		ExpressionAttributeValues: map[string]types.AttributeValue{":pk": pk},
	}
	if q.SortKeyPrefix != "" && table.SortKey != nil {
		*in.KeyConditionExpression += " AND begins_with(#sk, :sk)"
		in.ExpressionAttributeNames["#sk"] = table.SortKey.Name
		in.ExpressionAttributeValues[":sk"] = &types.AttributeValueMemberS{Value: q.SortKeyPrefix}
	}
	if key, ok := cursor.(map[string]types.AttributeValue); ok {
		in.ExclusiveStartKey = key
	}
	out, err := r.client.Query(ctx, in)
	if err != nil {
		return domain.ItemPage{}, classify(err)
	}
	return toPage(out.Items, out.LastEvaluatedKey), nil
}

// PutItem replaces an existing item. The condition makes it fail instead of
// re-creating an item deleted since it was read.
func (r *KeyValueRepository) PutItem(ctx context.Context, table domain.Table, item domain.Item) error {
	av, err := toAttributeMap(item)
	if err != nil {
		return err
	}
	_, err = r.client.PutItem(ctx, &dynamodb.PutItemInput{
		TableName:                &table.Name,
		Item:                     av,
		ConditionExpression:      sdkaws.String("attribute_exists(#pk)"),
		ExpressionAttributeNames: map[string]string{"#pk": table.PartitionKey.Name},
	})
	var cond *types.ConditionalCheckFailedException
	if errors.As(err, &cond) {
		return errors.New("the item no longer exists (deleted since it was loaded)")
	}
	return classify(err)
}

// DeleteItem deletes the item with key.
func (r *KeyValueRepository) DeleteItem(ctx context.Context, table domain.Table, key domain.Item) error {
	av, err := toAttributeMap(key)
	if err != nil {
		return err
	}
	_, err = r.client.DeleteItem(ctx, &dynamodb.DeleteItemInput{TableName: &table.Name, Key: av})
	return classify(err)
}

func toAttributeMap(item map[string]any) (map[string]types.AttributeValue, error) {
	out := make(map[string]types.AttributeValue, len(item))
	for k, v := range item {
		av, err := toAttribute(v)
		if err != nil {
			return nil, fmt.Errorf("attribute %q: %w", k, err)
		}
		out[k] = av
	}
	return out, nil
}

// toAttribute is the inverse of toValue.
func toAttribute(v any) (types.AttributeValue, error) {
	switch v := v.(type) {
	case nil:
		return &types.AttributeValueMemberNULL{Value: true}, nil
	case string:
		return &types.AttributeValueMemberS{Value: v}, nil
	case domain.Number:
		return &types.AttributeValueMemberN{Value: string(v)}, nil
	case bool:
		return &types.AttributeValueMemberBOOL{Value: v}, nil
	case []byte:
		return &types.AttributeValueMemberB{Value: v}, nil
	case domain.StringSet:
		return &types.AttributeValueMemberSS{Value: v}, nil
	case domain.NumberSet:
		ns := make([]string, len(v))
		for i, n := range v {
			ns[i] = string(n)
		}
		return &types.AttributeValueMemberNS{Value: ns}, nil
	case domain.BinarySet:
		return &types.AttributeValueMemberBS{Value: v}, nil
	case domain.Item:
		m, err := toAttributeMap(v)
		return &types.AttributeValueMemberM{Value: m}, err
	case map[string]any:
		m, err := toAttributeMap(v)
		return &types.AttributeValueMemberM{Value: m}, err
	case []any:
		list := make([]types.AttributeValue, len(v))
		for i, e := range v {
			av, err := toAttribute(e)
			if err != nil {
				return nil, err
			}
			list[i] = av
		}
		return &types.AttributeValueMemberL{Value: list}, nil
	}
	return nil, fmt.Errorf("unsupported value type %T", v)
}

func keyValue(typ, value string) (types.AttributeValue, error) {
	switch typ {
	case "N":
		return &types.AttributeValueMemberN{Value: value}, nil
	case "B":
		b, err := base64.StdEncoding.DecodeString(value)
		if err != nil {
			return nil, fmt.Errorf("binary keys must be base64: %w", err)
		}
		return &types.AttributeValueMemberB{Value: b}, nil
	}
	return &types.AttributeValueMemberS{Value: value}, nil
}

func toPage(items []map[string]types.AttributeValue, next map[string]types.AttributeValue) domain.ItemPage {
	page := domain.ItemPage{Items: make([]domain.Item, len(items))}
	for i, it := range items {
		page.Items[i] = toItem(it)
	}
	if len(next) > 0 {
		page.Next = next
	}
	return page
}

func toItem(m map[string]types.AttributeValue) domain.Item {
	item := make(domain.Item, len(m))
	for k, v := range m {
		item[k] = toValue(v)
	}
	return item
}

// toValue converts an attribute value without losing precision: numbers stay
// as their decimal string.
func toValue(av types.AttributeValue) any {
	switch v := av.(type) {
	case *types.AttributeValueMemberS:
		return v.Value
	case *types.AttributeValueMemberN:
		return domain.Number(v.Value)
	case *types.AttributeValueMemberB:
		return v.Value
	case *types.AttributeValueMemberBOOL:
		return v.Value
	case *types.AttributeValueMemberNULL:
		return nil
	case *types.AttributeValueMemberM:
		return map[string]any(toItem(v.Value))
	case *types.AttributeValueMemberL:
		list := make([]any, len(v.Value))
		for i, e := range v.Value {
			list[i] = toValue(e)
		}
		return list
	case *types.AttributeValueMemberSS:
		return domain.StringSet(v.Value)
	case *types.AttributeValueMemberNS:
		set := make(domain.NumberSet, len(v.Value))
		for i, n := range v.Value {
			set[i] = domain.Number(n)
		}
		return set
	case *types.AttributeValueMemberBS:
		return domain.BinarySet(v.Value)
	}
	return nil
}

func toDomainTable(t *types.TableDescription) domain.Table {
	attrTypes := map[string]string{}
	for _, a := range t.AttributeDefinitions {
		attrTypes[sdkaws.ToString(a.AttributeName)] = string(a.AttributeType)
	}
	table := domain.Table{
		Name:      sdkaws.ToString(t.TableName),
		ARN:       sdkaws.ToString(t.TableArn),
		Status:    string(t.TableStatus),
		ItemCount: sdkaws.ToInt64(t.ItemCount),
		SizeBytes: sdkaws.ToInt64(t.TableSizeBytes),
		Protected: sdkaws.ToBool(t.DeletionProtectionEnabled),
	}
	table.PartitionKey, table.SortKey = keySchema(t.KeySchema, attrTypes)
	// Tables created as on-demand have no BillingModeSummary.
	table.BillingMode = "PROVISIONED"
	if t.BillingModeSummary != nil {
		table.BillingMode = string(t.BillingModeSummary.BillingMode)
	}
	if t.TableClassSummary != nil {
		table.Class = string(t.TableClassSummary.TableClass)
	}
	if t.StreamSpecification != nil && sdkaws.ToBool(t.StreamSpecification.StreamEnabled) {
		table.Stream = string(t.StreamSpecification.StreamViewType)
	}
	for _, idx := range t.GlobalSecondaryIndexes {
		pk, sk := keySchema(idx.KeySchema, attrTypes)
		table.Indexes = append(table.Indexes, domain.TableIndex{Name: sdkaws.ToString(idx.IndexName), Global: true, PartitionKey: pk, SortKey: sk})
	}
	for _, idx := range t.LocalSecondaryIndexes {
		pk, sk := keySchema(idx.KeySchema, attrTypes)
		table.Indexes = append(table.Indexes, domain.TableIndex{Name: sdkaws.ToString(idx.IndexName), PartitionKey: pk, SortKey: sk})
	}
	return table
}

func keySchema(schema []types.KeySchemaElement, attrTypes map[string]string) (domain.KeyAttribute, *domain.KeyAttribute) {
	var pk domain.KeyAttribute
	var sk *domain.KeyAttribute
	for _, k := range schema {
		attr := domain.KeyAttribute{Name: sdkaws.ToString(k.AttributeName), Type: attrTypes[sdkaws.ToString(k.AttributeName)]}
		if k.KeyType == types.KeyTypeHash {
			pk = attr
		} else {
			sk = &attr
		}
	}
	return pk, sk
}
