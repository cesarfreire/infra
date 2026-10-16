package config

import "infra/internal/tui/section"

// SearchItems offers the parameters and, once loaded, the secrets to the
// global search.
func (m *Model) SearchItems() []section.SearchItem {
	var items []section.SearchItem
	for _, p := range m.params.Items() {
		items = append(items, section.SearchItem{Bookmark: paramBookmark(p.Name), Detail: p.Type})
	}
	for _, s := range m.secrets.Items() {
		items = append(items, section.SearchItem{Bookmark: secretBookmark(s.Name)})
	}
	return items
}

var _ section.Searchable = (*Model)(nil)
