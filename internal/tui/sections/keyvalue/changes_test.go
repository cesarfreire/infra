package keyvalue

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"infra/internal/tui/section"
)

func confirmIn(t *testing.T, out []tea.Msg) section.ConfirmMsg {
	t.Helper()
	for _, msg := range out {
		if c, ok := msg.(section.ConfirmMsg); ok {
			return c
		}
	}
	t.Fatalf("expected a confirmation in %v", out)
	return section.ConfirmMsg{}
}

func TestEditItem(t *testing.T) {
	m, svc := newModel(t)
	send(m, press("enter"))
	send(m, press("e"))
	if m.view != viewEdit || !strings.Contains(m.editor.Value(), `"customer": "c1"`) {
		t.Fatalf("e should open the editor with the item:\n%s", m.editor.Value())
	}
	c := confirmIn(t, send(m, tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl}))
	if len(svc.saved) != 0 {
		t.Fatal("nothing is saved before confirming")
	}
	send(m, c.Yes())
	if len(svc.saved) != 1 || m.view != viewItem || !strings.Contains(m.View(), `"status": "edited"`) {
		t.Errorf("after saving the item view should show the saved item:\n%s", m.View())
	}
	if got := m.items.Items()[0]["status"]; got != "edited" {
		t.Errorf("the loaded items should be updated in place, got %v", got)
	}
}

func TestDeleteItem(t *testing.T) {
	m, svc := newModel(t)
	send(m, press("enter"))
	c := confirmIn(t, send(m, press("x")))
	if !strings.Contains(c.Body, "customer=c1, date=2026-01-01") {
		t.Errorf("body = %q", c.Body)
	}
	send(m, c.Yes())
	if len(svc.deleted) != 1 || len(m.items.Items()) != 1 {
		t.Errorf("deleted = %v, items left = %d", svc.deleted, len(m.items.Items()))
	}
}

func TestReadOnlyBlocksEditing(t *testing.T) {
	m, _ := newModel(t)
	m.deps.ReadOnly = true
	send(m, press("enter"))
	out := send(m, press("e"))
	if m.view == viewEdit {
		t.Error("the editor must not open when read-only")
	}
	for _, msg := range out {
		if st, ok := msg.(section.StatusMsg); ok && strings.Contains(st.Text, "read-only") {
			return
		}
	}
	t.Errorf("expected a read-only message, got %v", out)
}
