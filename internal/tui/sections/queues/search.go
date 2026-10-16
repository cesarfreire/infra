package queues

import (
	"strconv"

	"infra/internal/tui/section"
)

// SearchItems offers the queues to the global search.
func (m *Model) SearchItems() []section.SearchItem {
	var items []section.SearchItem
	for _, q := range m.list.Items() {
		items = append(items, section.SearchItem{Bookmark: bookmark(q), Detail: strconv.Itoa(q.Visible) + " visible"})
	}
	return items
}

var _ section.Searchable = (*Model)(nil)
