package app

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"infra/internal/domain"
)

// ItemPageSize is how many items a scan or query page reads.
const ItemPageSize = 100

// KeyValueDeps are the adapters the key-value use cases need.
type KeyValueDeps struct {
	Repo    KeyValueRepository
	Writer  ItemWriter
	Console KeyValueConsoleURLBuilder
	Opener  URLOpener
	Metrics TableMetricsSource
	Policy  Policy
}

// KeyValueService implements the use cases available for key-value tables.
type KeyValueService struct {
	repo    KeyValueRepository
	writer  ItemWriter
	console KeyValueConsoleURLBuilder
	opener  URLOpener
	metrics TableMetricsSource
	policy  Policy
}

// NewKeyValueService wires the key-value use cases with their adapters.
func NewKeyValueService(d KeyValueDeps) *KeyValueService {
	return &KeyValueService{repo: d.Repo, writer: d.Writer, console: d.Console, opener: d.Opener,
		metrics: d.Metrics, policy: d.Policy}
}

// SaveItem replaces original with the item described by editedJSON and
// returns what was written. Key attributes cannot change.
func (s *KeyValueService) SaveItem(ctx context.Context, table domain.Table, original domain.Item, editedJSON string) (domain.Item, error) {
	if err := s.policy.CheckWrite("edit item of " + table.Name); err != nil {
		return nil, err
	}
	edited, err := ParseItemJSON(editedJSON)
	if err != nil {
		return nil, err
	}
	item, err := PrepareItemUpdate(table, original, edited)
	if err != nil {
		return nil, err
	}
	if err := s.writer.PutItem(ctx, table, item); err != nil {
		return nil, fmt.Errorf("saving item: %w", err)
	}
	return item, nil
}

// DeleteItem deletes item from table.
func (s *KeyValueService) DeleteItem(ctx context.Context, table domain.Table, item domain.Item) error {
	if err := s.policy.CheckWrite("delete item of " + table.Name); err != nil {
		return err
	}
	if err := s.writer.DeleteItem(ctx, table, table.Key(item)); err != nil {
		return fmt.Errorf("deleting item: %w", err)
	}
	return nil
}

// Metrics returns the monitoring series of a table over w.
func (s *KeyValueService) Metrics(ctx context.Context, t domain.Table, w domain.MetricWindow) ([]domain.Series, error) {
	series, err := s.metrics.TableMetrics(ctx, t, w)
	if err != nil {
		return nil, fmt.Errorf("reading metrics of %s: %w", t.Name, err)
	}
	return series, nil
}

// ListTables returns tables sorted by name.
func (s *KeyValueService) ListTables(ctx context.Context) ([]domain.Table, error) {
	tables, err := s.repo.ListTables(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing tables: %w", err)
	}
	slices.SortFunc(tables, func(a, b domain.Table) int { return cmp.Compare(a.Name, b.Name) })
	return tables, nil
}

// Scan reads the next page of every item in the table.
func (s *KeyValueService) Scan(ctx context.Context, table domain.Table, cursor domain.Cursor) (domain.ItemPage, error) {
	page, err := s.repo.Scan(ctx, table, cursor, ItemPageSize)
	if err != nil {
		return domain.ItemPage{}, fmt.Errorf("scanning %s: %w", table.Name, err)
	}
	return page, nil
}

// Query reads the next page of items with the given keys.
func (s *KeyValueService) Query(ctx context.Context, table domain.Table, q domain.KeyQuery, cursor domain.Cursor) (domain.ItemPage, error) {
	if err := ValidateKeyQuery(table, q); err != nil {
		return domain.ItemPage{}, err
	}
	page, err := s.repo.Query(ctx, table, q, cursor, ItemPageSize)
	if err != nil {
		return domain.ItemPage{}, fmt.Errorf("querying %s: %w", table.Name, err)
	}
	return page, nil
}

// ValidateKeyQuery checks q against the table's key schema.
func ValidateKeyQuery(table domain.Table, q domain.KeyQuery) error {
	if strings.TrimSpace(q.PartitionValue) == "" {
		return fmt.Errorf("%s (partition key) is required", table.PartitionKey.Name)
	}
	if table.PartitionKey.Type == "N" {
		if _, err := strconv.ParseFloat(q.PartitionValue, 64); err != nil {
			return fmt.Errorf("%s must be a number", table.PartitionKey.Name)
		}
	}
	if q.SortKeyPrefix != "" {
		switch {
		case table.SortKey == nil:
			return errors.New("the table has no sort key")
		case table.SortKey.Type != "S":
			return fmt.Errorf("prefix search needs a string sort key; %s is %s", table.SortKey.Name, table.SortKey.Type)
		}
	}
	return nil
}

// OpenConsole opens the table in the web console and returns the URL.
func (s *KeyValueService) OpenConsole(table domain.Table) (string, error) {
	url := s.console.TableURL(table.Name)
	if err := s.opener.Open(url); err != nil {
		return url, fmt.Errorf("opening console: %w", err)
	}
	return url, nil
}
