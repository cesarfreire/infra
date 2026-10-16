package caches

import "infra/internal/tui/section"

// SearchItems offers the caches to the global search.
func (m *Model) SearchItems() []section.SearchItem {
	var items []section.SearchItem
	for _, c := range m.list.Items() {
		items = append(items, section.SearchItem{Bookmark: bookmark(c), Detail: engineLabel(c)})
	}
	return items
}

var _ section.Searchable = (*Model)(nil)
