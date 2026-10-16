package containers

import "infra/internal/tui/section"

// SearchItems offers the clusters and the services loaded (those of the
// cluster opened last) to the global search.
func (m *Model) SearchItems() []section.SearchItem {
	var items []section.SearchItem
	for _, c := range m.clusters.Items() {
		items = append(items, section.SearchItem{Bookmark: clusterBookmark(c.Name), Detail: c.Status})
	}
	for _, s := range m.services.Items() {
		items = append(items, section.SearchItem{Bookmark: serviceBookmark(s), Detail: s.TaskDefinition})
	}
	return items
}

var _ section.Searchable = (*Model)(nil)
