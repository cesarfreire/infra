package tui

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"infra/internal/domain"
	"infra/internal/tui/section"
)

func TestConfirmRunsOnlyOnYes(t *testing.T) {
	m := startWorkspace(t, &fakeWorkspace{}, "default", "us-east-1")
	type doneMsg struct{}
	ran := 0
	yes := func() tea.Msg { ran++; return nil }
	_ = doneMsg{}

	m = drive(m, section.ConfirmMsg{Title: "Stop instance", Body: "Stop web?", Yes: yes})
	if m.dialog != dialogConfirm || !strings.Contains(ansi.Strip(m.render()), "Stop web?") {
		t.Fatal("the confirmation should be shown")
	}
	m = drive(m, tea.KeyPressMsg{Code: tea.KeyEnter}) // enter is not "yes"
	m = drive(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.dialog != dialogNone || ran != 0 {
		t.Fatalf("esc should cancel; ran = %d", ran)
	}

	m = drive(m, section.ConfirmMsg{Title: "T", Body: "B", Yes: yes})
	m = drive(m, tea.MouseClickMsg{X: 1, Y: 1, Button: tea.MouseLeft})
	if m.dialog != dialogNone || ran != 0 {
		t.Fatal("a click must never confirm")
	}

	m = drive(m, section.ConfirmMsg{Title: "T", Body: "B", Yes: yes})
	drive(m, tea.KeyPressMsg{Code: 'y', Text: "y"})
	if ran != 1 {
		t.Errorf("y should run the action once; ran = %d", ran)
	}
}

func TestSSOLoginFromAlertReverifies(t *testing.T) {
	ws := &fakeWorkspace{verifyErr: fmt.Errorf("%w: token expired", domain.ErrUnauthenticated)}
	m := startWorkspace(t, ws, "prod", "sa-east-1")
	if !m.alertSSO {
		t.Fatal("prod is an SSO profile: the alert should offer a login")
	}
	opened := len(ws.opened)
	ws.verifyErr = nil // the login fixes the credentials
	m = drive(m, tea.KeyPressMsg{Code: 'l', Text: "l"})
	if len(ws.logins) != 1 || ws.logins[0] != "prod" {
		t.Fatalf("logins = %v", ws.logins)
	}
	if len(ws.opened) != opened+1 || m.identity.Account != "123456789012" || m.dialog != dialogNone {
		t.Errorf("after the login the environment should be verified again; opened %d → %d, dialog %v",
			opened, len(ws.opened), m.dialog)
	}
}

func TestFailedLoginIsReported(t *testing.T) {
	ws := &fakeWorkspace{verifyErr: fmt.Errorf("%w: expired", domain.ErrUnauthenticated)}
	m := startWorkspace(t, ws, "prod", "sa-east-1")
	ws.loginErr = errors.New("exit status 1")
	m = drive(m, tea.KeyPressMsg{Code: 'l', Text: "l"})
	if !m.status.Error || !strings.Contains(m.status.Text, "aws sso login") {
		t.Errorf("status = %+v", m.status)
	}
}

func TestNonSSOAlertShowsCommandOnly(t *testing.T) {
	ws := &fakeWorkspace{verifyErr: fmt.Errorf("%w: bad key", domain.ErrUnauthenticated)}
	m := startWorkspace(t, ws, "default", "us-east-1")
	if m.alertSSO {
		t.Error("default is not an SSO profile")
	}
	m = drive(m, tea.KeyPressMsg{Code: 'l', Text: "l"})
	if len(ws.logins) != 0 {
		t.Error("l must do nothing for non-SSO profiles")
	}
}

func TestReadOnlyChip(t *testing.T) {
	m := startWorkspace(t, &fakeWorkspace{readOnly: true}, "prod", "sa-east-1")
	if !strings.Contains(header(m), "read-only") {
		t.Errorf("header = %q", header(m))
	}
	m = startWorkspace(t, &fakeWorkspace{}, "dev", "sa-east-1")
	if strings.Contains(header(m), "read-only") {
		t.Error("writable profiles have no chip")
	}
}
