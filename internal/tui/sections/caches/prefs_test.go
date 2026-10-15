package caches

import (
	"testing"

	"infra/internal/domain"
	"infra/internal/tui/section"
	"infra/internal/tui/theme"
)

func TestJumpHostIsRememberedAcrossModels(t *testing.T) {
	deps := section.MemoryDeps(theme.Default())
	first := New(&fakeService{}, &recorder{}, deps)
	first.SetSize(140, 20)
	first.Focus()
	send(first, run(first.Init())...)
	send(first, press("c"))
	send(first, press("down"), press("enter")) // worker (i-2)

	// A new model, as after a restart or a profile switch back.
	second := New(&fakeService{}, &recorder{}, deps)
	second.SetSize(140, 20)
	second.Focus()
	send(second, run(second.Init())...)
	send(second, press("c"))
	if sel, _ := second.picker.Selected(); sel.Value != "i-2" {
		t.Errorf("preselected = %v, want i-2 from the preferences", sel)
	}
}

func TestRevealCache(t *testing.T) {
	m, _, _ := newModel(t)
	send(m, section.RevealMsg{Bookmark: domain.Bookmark{Kind: domain.KindCache, ID: "memo"}})
	if c, _ := m.list.Selected(); c.ID != "memo" {
		t.Errorf("selected %q", c.ID)
	}
}
