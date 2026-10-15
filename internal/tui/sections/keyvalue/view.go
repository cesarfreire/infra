package keyvalue

import (
	"fmt"
	"strings"

	"infra/internal/tui/components/details"
)

func (m *Model) View() string {
	switch m.view {
	case viewItem:
		return m.theme.Title.Render(m.table.Name) + " " + m.theme.Muted.Render("item") + "\n\n" + m.viewer.View()
	case viewTableDetails:
		return m.details.View()
	case viewQuery:
		return m.form.view(m.theme, m.table.Name)
	case viewEdit:
		return m.theme.Title.Render("Edit item") + " " + m.theme.Muted.Render(m.table.Name) + "\n\n" +
			m.editor.View() + "\n" +
			m.theme.Muted.Render("ctrl+s to review and save · esc to discard · key attributes cannot change")
	}
	if m.level == levelItems {
		return m.items.View()
	}
	return m.tables.View()
}

func (m *Model) tableDetailsHeader() string {
	t := m.table
	var indexes []details.Field
	for _, idx := range t.Indexes {
		kind := "local"
		if idx.Global {
			kind = "global"
		}
		keys := idx.PartitionKey.Name
		if idx.SortKey != nil {
			keys += ", " + idx.SortKey.Name
		}
		indexes = append(indexes, details.Field{Label: idx.Name, Value: kind + " · " + keys})
	}
	stream := t.Stream
	if stream == "" {
		stream = "disabled"
	}
	return details.Render(m.theme, t.Name, "table details", []details.Field{
		{Label: "Status", Value: t.Status},
		{Label: "Keys", Value: keysLabel(t)},
		{Label: "Items", Value: fmt.Sprintf("~%d (updated every ~6h)", t.ItemCount)},
		{Label: "Size", Value: humanBytes(t.SizeBytes)},
		{Label: "Billing", Value: billing(t.BillingMode)},
		{Label: "Table class", Value: strings.ToLower(strings.ReplaceAll(t.Class, "_", " "))},
		{Label: "Stream", Value: stream},
		{Label: "Delete protection", Value: fmt.Sprint(t.Protected)},
		{Label: "ARN", Value: t.ARN},
	}, details.Section{Title: "Indexes", Fields: indexes})
}
