package keyvalue

import (
	"context"
	"fmt"
	"reflect"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"

	"infra/internal/domain"
	"infra/internal/tui/section"
)

func (m *Model) showItem(it domain.Item) {
	m.item = it
	m.viewer.SetContent(prettyItem(m.table, it))
	m.viewer.GotoTop()
	m.view = viewItem
}

func (m *Model) openEditor() tea.Cmd {
	if cmd := m.deps.BlockWrite("edit item of " + m.table.Name); cmd != nil {
		return cmd
	}
	m.editor.SetValue(prettyItem(m.table, m.item))
	m.view = viewEdit
	return m.editor.Focus()
}

func (m *Model) confirmSave() tea.Cmd {
	svc, table, original, edited := m.svc, m.table, m.item, m.editor.Value()
	return section.Confirm("Save item", "Replace this item of "+table.Name+" with the edited version?", func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), loadTimeout)
		defer cancel()
		saved, err := svc.SaveItem(ctx, table, original, edited)
		if err != nil {
			return itemChangedMsg{err: err}
		}
		return itemChangedMsg{text: "Item saved", saved: saved}
	})
}

func (m *Model) confirmDelete(it domain.Item) tea.Cmd {
	if cmd := m.deps.BlockWrite("delete item of " + m.table.Name); cmd != nil {
		return cmd
	}
	svc, table := m.svc, m.table
	body := fmt.Sprintf("Delete the item %s from %s? This cannot be undone.", keyLabel(table, it), table.Name)
	return section.Confirm("Delete item", body, func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), loadTimeout)
		defer cancel()
		if err := svc.DeleteItem(ctx, table, it); err != nil {
			return itemChangedMsg{err: err}
		}
		return itemChangedMsg{text: "Item " + keyLabel(table, it) + " deleted", saved: it, deleted: true}
	})
}

// handleItemChanged updates the loaded items in place, so the list keeps
// its position without reading the table again.
func (m *Model) handleItemChanged(msg itemChangedMsg) tea.Cmd {
	if msg.err != nil {
		return section.Error(msg.err)
	}
	key := m.table.Key(msg.saved)
	idx := -1
	for i, it := range m.loaded {
		if reflect.DeepEqual(m.table.Key(it), key) {
			idx = i
		}
	}
	switch {
	case msg.deleted && idx >= 0:
		m.loaded = slices.Delete(m.loaded, idx, idx+1)
		m.view = viewList
	case !msg.deleted && idx >= 0:
		m.loaded[idx] = msg.saved
		m.showItem(msg.saved)
	}
	m.items.SetItems(m.loaded)
	return section.Status(msg.text)
}

func keyLabel(t domain.Table, it domain.Item) string {
	parts := []string{fmt.Sprintf("%s=%s", t.PartitionKey.Name, formatValue(it[t.PartitionKey.Name]))}
	if t.SortKey != nil {
		parts = append(parts, fmt.Sprintf("%s=%s", t.SortKey.Name, formatValue(it[t.SortKey.Name])))
	}
	return strings.Join(parts, ", ")
}
