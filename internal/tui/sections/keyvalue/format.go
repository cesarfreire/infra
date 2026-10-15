package keyvalue

import (
	"cmp"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"infra/internal/domain"
	"infra/internal/filter"
	"infra/internal/tui/components/listview"
)

const maxItemColumns = 8

var tableColumns = []listview.Column[domain.Table]{
	{Title: "Table", Value: func(t domain.Table) string { return t.Name }},
	{Title: "Status", Width: 9, Value: func(t domain.Table) string { return t.Status }},
	{Title: "Items", Width: 8, Value: func(t domain.Table) string { return humanCount(t.ItemCount) }},
	{Title: "Size", Width: 9, Value: func(t domain.Table) string { return humanBytes(t.SizeBytes) }},
	{Title: "Billing", Width: 11, Value: func(t domain.Table) string { return billing(t.BillingMode) }},
	{Title: "Keys", Width: 30, Value: keysLabel},
}

const tablePlaceholder = "name, status:active, billing:on-demand, key:userId"

func tableFields(t domain.Table) []filter.Field {
	return []filter.Field{
		{Name: "name", Value: t.Name},
		{Name: "status", Value: t.Status},
		{Name: "billing", Value: billing(t.BillingMode)},
		{Name: "key", Value: keysLabel(t)},
	}
}

const itemPlaceholder = "any value, or attribute:value (e.g. status:active)"

// itemColumns shows the key attributes first, then the attributes most
// present in the loaded items.
func itemColumns(t domain.Table, items []domain.Item) []listview.Column[domain.Item] {
	keys := []string{t.PartitionKey.Name}
	if t.SortKey != nil {
		keys = append(keys, t.SortKey.Name)
	}
	counts := map[string]int{}
	for _, it := range items {
		for k := range it {
			if !slices.Contains(keys, k) {
				counts[k]++
			}
		}
	}
	others := make([]string, 0, len(counts))
	for k := range counts {
		others = append(others, k)
	}
	slices.SortFunc(others, func(a, b string) int { return cmp.Or(counts[b]-counts[a], cmp.Compare(a, b)) })
	if len(others) > maxItemColumns {
		others = others[:maxItemColumns]
	}

	var cols []listview.Column[domain.Item]
	for i, name := range append(keys, others...) {
		width := 18
		if i == 1 && t.SortKey != nil {
			width = 24
		}
		cols = append(cols, listview.Column[domain.Item]{
			Title: name,
			Width: width,
			Value: func(it domain.Item) string { return formatValue(it[name]) },
		})
	}
	return cols
}

func itemFields(it domain.Item) []filter.Field {
	fields := make([]filter.Field, 0, len(it))
	for k, v := range it {
		fields = append(fields, filter.Field{Name: k, Value: formatValue(v)})
	}
	return fields
}

// formatValue renders a value on one line for table cells.
func formatValue(v any) string {
	switch v := v.(type) {
	case nil:
		return ""
	case string:
		return strings.ReplaceAll(v, "\n", "⏎")
	case domain.Number:
		return string(v)
	case bool:
		return fmt.Sprint(v)
	case []byte:
		return fmt.Sprintf("<%d bytes>", len(v))
	}
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprint(v)
	}
	return string(b)
}

// prettyItem renders an item as indented JSON with the key attributes first.
func prettyItem(t domain.Table, it domain.Item) string {
	var names []string
	for _, k := range []string{t.PartitionKey.Name, sortKeyName(t)} {
		if _, ok := it[k]; ok && k != "" {
			names = append(names, k)
		}
	}
	var rest []string
	for k := range it {
		if !slices.Contains(names, k) {
			rest = append(rest, k)
		}
	}
	slices.Sort(rest)

	var lines []string
	for _, k := range append(names, rest...) {
		name, _ := json.Marshal(k)
		value, err := json.MarshalIndent(it[k], "  ", "  ")
		if err != nil {
			value = []byte(fmt.Sprintf("%q", fmt.Sprint(it[k])))
		}
		lines = append(lines, "  "+string(name)+": "+string(value))
	}
	return "{\n" + strings.Join(lines, ",\n") + "\n}"
}

func sortKeyName(t domain.Table) string {
	if t.SortKey == nil {
		return ""
	}
	return t.SortKey.Name
}

func keysLabel(t domain.Table) string {
	label := t.PartitionKey.Name + " (" + t.PartitionKey.Type + ")"
	if t.SortKey != nil {
		label += ", " + t.SortKey.Name + " (" + t.SortKey.Type + ")"
	}
	return label
}

func billing(mode string) string {
	switch mode {
	case "PAY_PER_REQUEST":
		return "on-demand"
	case "PROVISIONED":
		return "provisioned"
	}
	return strings.ToLower(mode)
}

func humanCount(n int64) string {
	switch {
	case n >= 1_000_000_000:
		return fmt.Sprintf("%.1fB", float64(n)/1e9)
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1e6)
	case n >= 1_000:
		return fmt.Sprintf("%.1fk", float64(n)/1e3)
	}
	return fmt.Sprint(n)
}

func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}
