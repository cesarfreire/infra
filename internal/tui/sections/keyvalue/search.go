package keyvalue

import "infra/internal/tui/section"

// SearchItems offers the tables to the global search.
func (m *Model) SearchItems() []section.SearchItem {
	var items []section.SearchItem
	for _, t := range m.tables.Items() {
		items = append(items, section.SearchItem{Bookmark: tableBookmark(t.Name), Detail: keysLabel(t)})
	}
	return items
}

var _ section.Searchable = (*Model)(nil)
