package sessions

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"infra/internal/app"
	"infra/internal/session"
	"infra/internal/tui/section"
	"infra/internal/tui/theme"
)

// These tests use a real session manager running local processes.

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("timed out")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func setup(t *testing.T, cmd app.Command) (*Model, *session.Manager, *session.Session) {
	t.Helper()
	mgr := session.NewManager()
	t.Cleanup(mgr.CloseAll)
	m := New(mgr, theme.Default())
	m.SetSize(80, 10)
	m.Focus()
	s, err := mgr.Start(session.KindShell, "test · shell", cmd)
	if err != nil {
		t.Fatal(err)
	}
	return m, mgr, s
}

func TestOpenSessionAttachesAndNavigates(t *testing.T) {
	m, _, s := setup(t, app.Command{Name: "cat"})
	_, cmd := m.Update(section.OpenSessionMsg{ID: s.ID})
	if nav, ok := cmd().(section.NavigateMsg); !ok || nav.Title != Title {
		t.Errorf("expected NavigateMsg to %q, got %#v", Title, cmd())
	}
	if m.InputMode() != section.ModeRaw {
		t.Error("an attached running session should take raw input")
	}
}

func TestKeysReachProcessAndDetach(t *testing.T) {
	m, _, s := setup(t, app.Command{Name: "cat"})
	m.Update(section.OpenSessionMsg{ID: s.ID})
	for _, r := range "hello" {
		m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	waitFor(t, func() bool { return strings.Contains(m.View(), "hello") })
	if m.Cursor() == nil {
		t.Error("expected a terminal cursor while attached")
	}

	m.Update(tea.KeyPressMsg{Code: ']', Mod: tea.ModCtrl})
	if m.InputMode() != section.ModeNormal || !strings.Contains(m.View(), "1 open") {
		t.Error("ctrl+] should detach and show the list")
	}
	if !s.Running() {
		t.Error("detaching must keep the session running")
	}
}

func TestExitIsReported(t *testing.T) {
	m, _, s := setup(t, app.Command{Name: "sh", Args: []string{"-c", "sleep 0.2; exit 3"}})
	m.Update(section.OpenSessionMsg{ID: s.ID})
	<-s.Done()

	cmd := m.reportExits()
	if cmd == nil {
		t.Fatal("expected an exit status message")
	}
	if st, ok := cmd().(section.StatusMsg); !ok || !st.Error || !strings.Contains(st.Text, "exit status 3") {
		t.Errorf("unexpected status %#v", cmd())
	}
	if m.reportExits() != nil {
		t.Error("an exit should be reported only once")
	}

	if m.InputMode() != section.ModeCapture {
		t.Error("an ended session should leave raw mode")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.InputMode() != section.ModeNormal {
		t.Error("esc should go back to the list after the session ended")
	}
}

func TestCloseRemovesSession(t *testing.T) {
	m, mgr, s := setup(t, app.Command{Name: "cat"})
	_, cmd := m.Update(tea.KeyPressMsg{Code: 'x', Text: "x"})
	if st, ok := cmd().(section.StatusMsg); !ok || st.Error {
		t.Errorf("unexpected status %#v", cmd())
	}
	<-s.Done()
	if len(mgr.List()) != 0 {
		t.Error("x should remove the session")
	}
}
