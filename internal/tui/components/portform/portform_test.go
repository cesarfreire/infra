package portform

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"infra/internal/tui/theme"
)

func typeText(f *Model, s string) {
	for _, r := range s {
		f.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
}

func TestPorts(t *testing.T) {
	f := New()
	f.Reset()
	if _, _, err := f.Ports(); err == nil {
		t.Error("remote port is required")
	}
	typeText(&f, "5432")
	if local, remote, err := f.Ports(); err != nil || local != 5432 || remote != 5432 {
		t.Errorf("Ports() = %d, %d, %v; local should default to remote", local, remote, err)
	}
	f.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	typeText(&f, "15432")
	if local, _, _ := f.Ports(); local != 15432 {
		t.Errorf("local = %d", local)
	}
}

func TestFixedRemote(t *testing.T) {
	f := New()
	f.ResetFixedRemote(6379, "use --tls")
	if local, remote, err := f.Ports(); err != nil || local != 6379 || remote != 6379 {
		t.Errorf("Ports() = %d, %d, %v", local, remote, err)
	}
	f.Update(tea.KeyPressMsg{Code: tea.KeyTab}) // stays on the local field
	typeText(&f, "16379")
	if local, remote, _ := f.Ports(); local != 16379 || remote != 6379 {
		t.Errorf("Ports() = %d, %d", local, remote)
	}
	view := ansi.Strip(f.View(theme.Default(), "Port forward", "x"))
	if !strings.Contains(view, "Remote port: 6379") || !strings.Contains(view, "use --tls") {
		t.Errorf("view:\n%s", view)
	}
	f.Reset()
	if _, _, err := f.Ports(); err == nil {
		t.Error("Reset should make the remote port editable (and required) again")
	}
}
