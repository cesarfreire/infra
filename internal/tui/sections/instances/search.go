package instances

import (
	"infra/internal/tui/section"
)

// SearchItems offers the loaded instances to the global search.
func (m *Model) SearchItems() []section.SearchItem {
	items := make([]section.SearchItem, 0, len(m.list.Items()))
	for _, i := range m.list.Items() {
		items = append(items, section.SearchItem{Bookmark: bookmark(i), Detail: i.ID + " · " + string(i.State)})
	}
	return items
}

var _ section.Searchable = (*Model)(nil)
