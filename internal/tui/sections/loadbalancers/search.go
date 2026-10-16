package loadbalancers

import "infra/internal/tui/section"

// SearchItems offers the load balancers to the global search.
func (m *Model) SearchItems() []section.SearchItem {
	var items []section.SearchItem
	for _, lb := range m.balancers.Items() {
		items = append(items, section.SearchItem{Bookmark: bookmark(lb), Detail: lb.Type + " · " + lb.Scheme})
	}
	return items
}

var _ section.Searchable = (*Model)(nil)
