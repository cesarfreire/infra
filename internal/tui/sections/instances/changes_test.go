package instances

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"infra/internal/domain"
	"infra/internal/tui/section"
	"infra/internal/tui/theme"
)

func confirmOf(t *testing.T, cmd tea.Cmd) section.ConfirmMsg {
	t.Helper()
	if cmd == nil {
		t.Fatal("expected a command")
	}
	c, ok := cmd().(section.ConfirmMsg)
	if !ok {
		t.Fatalf("expected a confirmation, got %#v", cmd())
	}
	return c
}

func TestStopAsksThenReloads(t *testing.T) {
	m, svc, _ := newLoaded(t)
	_, cmd := m.Update(press("X"))
	c := confirmOf(t, cmd)
	if !strings.Contains(c.Body, "web") || !strings.Contains(c.Body, "instance store") {
		t.Errorf("body = %q", c.Body)
	}
	if len(svc.powered) != 0 {
		t.Fatal("nothing must happen before confirming")
	}
	_, cmd = m.Update(c.Yes())
	if len(svc.powered) != 1 || svc.powered[0] != "stop i-1" {
		t.Errorf("powered = %v", svc.powered)
	}
	if cmd == nil {
		t.Error("the change should report a status and reload the list")
	}
}

func TestStartRunningInstanceFailsFast(t *testing.T) {
	m, _, _ := newLoaded(t)
	_, cmd := m.Update(press("S"))
	if st := statusOf(t, cmd); !st.Error || !strings.Contains(st.Text, "running") {
		t.Errorf("status = %+v", st)
	}
}

func TestReadOnlyBlocksBeforeConfirming(t *testing.T) {
	svc, rec := &fakeService{instances: []domain.Instance{{ID: "i-1", Name: "web", State: domain.InstanceStateRunning}}}, &recorder{}
	deps := section.MemoryDeps(theme.Default())
	deps.ReadOnly = true
	m := New(svc, rec, deps)
	m.SetSize(120, 20)
	m.Focus()
	m.Update(instancesLoadedMsg{instances: svc.instances})
	_, cmd := m.Update(press("B"))
	if st := statusOf(t, cmd); !st.Error || !strings.Contains(st.Text, "read-only") {
		t.Errorf("status = %+v", st)
	}
}

func TestFavoriteAndReveal(t *testing.T) {
	m, _, _ := newLoaded(t)
	m.Update(press("down"))
	_, cmd := m.Update(press("*"))
	if st := statusOf(t, cmd); !strings.Contains(st.Text, "db") {
		t.Errorf("status = %+v", st)
	}
	if !strings.Contains(m.View(), "★ db") {
		t.Errorf("the favorite should be starred:\n%s", m.View())
	}

	m.Update(press("up"))
	m.Update(section.RevealMsg{Bookmark: domain.Bookmark{Kind: domain.KindInstance, ID: "i-2"}})
	if sel, _ := m.list.Selected(); sel.ID != "i-2" {
		t.Errorf("reveal should select i-2, got %s", sel.ID)
	}
	m.Update(section.RevealMsg{Bookmark: domain.Bookmark{Kind: domain.KindTable, ID: "i-1"}})
	if sel, _ := m.list.Selected(); sel.ID != "i-2" {
		t.Error("bookmarks of other kinds must be ignored")
	}
}

func TestOpeningRecordsRecents(t *testing.T) {
	m, _, _ := newLoaded(t)
	m.Update(press("enter"))
	recents := m.deps.Bookmarks.Recents()
	if len(recents) != 1 || recents[0].ID != "i-1" {
		t.Errorf("recents = %v", recents)
	}
}
