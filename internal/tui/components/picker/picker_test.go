package picker

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"infra/internal/tui/theme"
)

func items(n int) []Item {
	out := make([]Item, n)
	for i := range out {
		out[i] = Item{Value: fmt.Sprintf("v%02d", i), Label: fmt.Sprintf("item-%02d", i), Detail: fmt.Sprintf("detail %d", i)}
	}
	return out
}

func press(code rune) tea.KeyPressMsg { return tea.KeyPressMsg{Code: code} }

func TestStartsOnCurrentAndScrolls(t *testing.T) {
	m := New("Pick", items(20), "v15", 5)
	if sel, _ := m.Selected(); sel.Value != "v15" {
		t.Fatalf("Selected() = %v, want v15", sel)
	}
	view := ansi.Strip(m.View(theme.Default(), 40))
	if !strings.Contains(view, "item-15") || strings.Contains(view, "item-00") {
		t.Errorf("the window should scroll to the current item:\n%s", view)
	}
}

func TestFilterChooseAndCancel(t *testing.T) {
	m := New("Pick", items(20), "", 5)
	for _, r := range "detail 7" {
		m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	if res, _ := m.Update(press(tea.KeyEnter)); res != Chosen {
		t.Fatal("enter should choose")
	}
	if sel, _ := m.Selected(); sel.Value != "v07" {
		t.Errorf("Selected() = %v, want v07 (filter matches details)", sel)
	}
	if res, _ := m.Update(press(tea.KeyEscape)); res != Cancelled {
		t.Error("esc should cancel")
	}
}

func TestNoMatchCannotChoose(t *testing.T) {
	m := New("Pick", items(3), "", 5)
	for _, r := range "zzz" {
		m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	if res, _ := m.Update(press(tea.KeyEnter)); res == Chosen {
		t.Error("nothing to choose when nothing matches")
	}
}

func TestSetItemsKeepsFilterAndSelection(t *testing.T) {
	m := New("Pick", items(5), "", 5)
	for _, r := range "item-0" {
		m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	m.Update(press(tea.KeyDown))
	m.Update(press(tea.KeyDown))
	m.SetItems(append([]Item{{Value: "new", Label: "item-00-new"}}, items(5)...))
	if sel, _ := m.Selected(); sel.Value != "v02" {
		t.Errorf("Selected() = %v, want v02 kept", sel)
	}
	if len(m.visible) != 6 {
		t.Errorf("the filter should still apply: %d visible", len(m.visible))
	}
}

func TestClick(t *testing.T) {
	m := New("Pick", items(20), "", 5)
	if !m.Click(Top + 2) {
		t.Fatal("click on the third row should hit")
	}
	if sel, _ := m.Selected(); sel.Value != "v02" {
		t.Errorf("Selected() = %v, want v02", sel)
	}
	if m.Click(Top+5) || m.Click(0) {
		t.Error("clicks outside the item rows should miss")
	}
}

func TestFilterIgnoresAccents(t *testing.T) {
	m := New("Region", []Item{{Value: "us-east-1", Label: "us-east-1", Detail: "N. Virginia"}, {Value: "sa-east-1", Label: "sa-east-1", Detail: "São Paulo"}}, "", 5)
	for _, r := range "sao" {
		m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	if sel, ok := m.Selected(); !ok || sel.Value != "sa-east-1" {
		t.Errorf("Selected() = %v, want sa-east-1", sel)
	}
}
