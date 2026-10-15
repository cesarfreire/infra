package domain

// Table is a key-value table (a DynamoDB table).
type Table struct {
	Name         string
	ARN          string
	Status       string
	ItemCount    int64 // approximate, refreshed by the provider every few hours
	SizeBytes    int64
	BillingMode  string
	Class        string
	PartitionKey KeyAttribute
	SortKey      *KeyAttribute
	Indexes      []TableIndex
	Stream       string // stream view type; empty when disabled
	Protected    bool   // deletion protection
}

// KeyAttribute is a key attribute and its scalar type: S, N or B.
type KeyAttribute struct {
	Name string
	Type string
}

// TableIndex is a secondary index.
type TableIndex struct {
	Name         string
	Global       bool
	PartitionKey KeyAttribute
	SortKey      *KeyAttribute
}

// Item is a table item. Values are string, Number, bool, nil, []byte,
// []any (lists), StringSet, NumberSet, BinarySet or map[string]any.
type Item map[string]any

// Sets keep their type so an edited item is written back as a set; they
// marshal to JSON as plain arrays.
type (
	StringSet []string
	NumberSet []Number
	BinarySet [][]byte
)

// Number keeps numeric values exactly as stored (no float rounding).
type Number string

// Cursor is an opaque position in a paginated read, owned by the adapter.
// A nil cursor means "from the start" in requests and "no more" in pages.
type Cursor any

// ItemPage is one page of a scan or query.
type ItemPage struct {
	Items []Item
	Next  Cursor
}

// KeyQuery selects items by partition key, optionally narrowing by a sort
// key prefix.
type KeyQuery struct {
	PartitionValue string
	SortKeyPrefix  string
}

// MarshalJSON writes the number unquoted, exactly as stored.
func (n Number) MarshalJSON() ([]byte, error) { return []byte(n), nil }

// Key returns the key attributes of item for table.
func (t Table) Key(item Item) Item {
	key := Item{t.PartitionKey.Name: item[t.PartitionKey.Name]}
	if t.SortKey != nil {
		key[t.SortKey.Name] = item[t.SortKey.Name]
	}
	return key
}
