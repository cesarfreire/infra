package session

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/vt"

	"infra/internal/app"
)

// These tests run real processes in a real pseudo-terminal.

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

func TestSessionRendersOutputAndExits(t *testing.T) {
	m := NewManager()
	s, err := m.Start(KindShell, "echo", app.Command{Name: "sh", Args: []string{"-c", "printf 'hello from pty'"}})
	if err != nil {
		t.Fatal(err)
	}
	<-s.Done()
	waitFor(t, func() bool { return strings.Contains(s.Render(), "hello from pty") })
	if s.Running() || s.Err() != nil {
		t.Errorf("Running() = %v, Err() = %v", s.Running(), s.Err())
	}
	select {
	case <-m.Updates():
	default:
		t.Error("expected an update notification")
	}
}

func TestSessionReceivesKeys(t *testing.T) {
	m := NewManager()
	s, err := m.Start(KindShell, "cat", app.Command{Name: "cat"})
	if err != nil {
		t.Fatal(err)
	}
	defer m.CloseAll()

	for _, r := range "ping" {
		s.SendKey(vt.KeyPressEvent{Code: r, Text: string(r)})
	}
	waitFor(t, func() bool { return strings.Contains(s.Render(), "ping") })
}

func TestRemoveKillsProcess(t *testing.T) {
	m := NewManager()
	s, err := m.Start(KindPortForward, "sleep", app.Command{Name: "sleep", Args: []string{"60"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Remove(s.ID); err != nil {
		t.Fatal(err)
	}
	select {
	case <-s.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("process was not killed")
	}
	if len(m.List()) != 0 || m.Get(s.ID) != nil {
		t.Error("session should be removed")
	}
}

func TestStartFailure(t *testing.T) {
	if _, err := NewManager().Start(KindShell, "x", app.Command{Name: "/nonexistent/binary"}); err == nil {
		t.Error("expected an error for a missing binary")
	}
}

func TestSetSizeResizesSessions(t *testing.T) {
	m := NewManager()
	s, err := m.Start(KindShell, "cat", app.Command{Name: "cat"})
	if err != nil {
		t.Fatal(err)
	}
	defer m.CloseAll()
	m.SetSize(100, 30)
	if s.term.Width() != 100 || s.term.Height() != 30 {
		t.Errorf("size = %dx%d, want 100x30", s.term.Width(), s.term.Height())
	}
}
