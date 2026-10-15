package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"infra/internal/domain"
	"infra/internal/tui/section"
	"infra/internal/tui/theme"
)

type openCall struct{ profile, region string }

type fakeWorkspace struct {
	opened    []openCall
	shared    *fakeSection // reused across environments, like Sessions
	verifyErr error
	openErr   error
	readOnly  bool
	logins    []string
	loginErr  error
}

func (w *fakeWorkspace) Profiles() ([]domain.Profile, error) {
	return []domain.Profile{{Name: "default", Region: "us-east-1"}, {Name: "prod", Region: "sa-east-1", SSO: true}, {Name: "noregion"}}, nil
}

func (w *fakeWorkspace) Regions() []domain.Region {
	return []domain.Region{{Code: "us-east-1", Name: "N. Virginia"}, {Code: "sa-east-1", Name: "São Paulo"}}
}

func (w *fakeWorkspace) Login(profile string) (Login, error) {
	w.logins = append(w.logins, profile)
	return Login{SessionID: 9, Wait: func() error { return w.loginErr }}, nil
}

func (w *fakeWorkspace) Open(profile, region string) (Environment, error) {
	w.opened = append(w.opened, openCall{profile, region})
	if w.openErr != nil {
		return Environment{}, w.openErr
	}
	if region == "" {
		return Environment{}, domain.ErrNoRegion
	}
	return Environment{
		Profile:  profile,
		Region:   region,
		Sections: []section.Section{&fakeSection{title: "Instances " + profile}, w.shared},
		Verify: func(context.Context) (domain.Identity, error) {
			return domain.Identity{Account: "123456789012"}, w.verifyErr
		},
		ReadOnly: w.readOnly,
	}, nil
}

// run executes cmd and returns the messages it produces (batches flattened).
// Commands that take long are timers (cursor blink) and are dropped.
func run(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	ch := make(chan tea.Msg, 1)
	go func() { ch <- cmd() }()
	var result tea.Msg
	select {
	case result = <-ch:
	case <-time.After(100 * time.Millisecond):
		return nil
	}
	switch msg := result.(type) {
	case nil:
		return nil
	case tea.BatchMsg:
		var out []tea.Msg
		for _, c := range msg {
			out = append(out, run(c)...)
		}
		return out
	default:
		if strings.Contains(fmt.Sprintf("%T", msg), "Blink") {
			return nil
		}
		return []tea.Msg{msg}
	}
}

// drive updates m with msgs and every message their commands produce.
func drive(m Model, msgs ...tea.Msg) Model {
	for len(msgs) > 0 {
		msg := msgs[0]
		msgs = msgs[1:]
		next, cmd := m.Update(msg)
		m = next.(Model)
		msgs = append(msgs, run(cmd)...)
	}
	return m
}

func ctrl(r rune) tea.KeyPressMsg { return tea.KeyPressMsg{Code: r, Mod: tea.ModCtrl} }

func typeKeys(s string) []tea.Msg {
	var out []tea.Msg
	for _, r := range s {
		out = append(out, tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	return out
}

func startWorkspace(t *testing.T, ws *fakeWorkspace, profile, region string) Model {
	t.Helper()
	if ws.shared == nil {
		ws.shared = &fakeSection{title: "Sessions"}
	}
	m := New(Options{Title: "infra", Workspace: ws, Profile: profile, Region: region}, theme.Default())
	m = drive(m, tea.WindowSizeMsg{Width: 120, Height: 30})
	return drive(m, run(m.Init())...)
}

func header(m Model) string { return ansi.Strip(m.renderHeader()) }

func TestWorkspaceOpensAndVerifies(t *testing.T) {
	ws := &fakeWorkspace{}
	m := startWorkspace(t, ws, "prod", "sa-east-1")
	if len(m.sections) != 2 || m.sections[0].Title() != "Instances prod" {
		t.Fatalf("sections = %v", m.sections)
	}
	h := header(m)
	for _, want := range []string{"profile: prod", "region: sa-east-1", "account 123456789012"} {
		if !strings.Contains(h, want) {
			t.Errorf("header %q missing %q", h, want)
		}
	}
	if !ws.shared.listened || ws.shared.inits != 0 {
		t.Error("the shared section should be listened to but initialized lazily")
	}
}

func TestSwitchProfileUsesItsRegion(t *testing.T) {
	ws := &fakeWorkspace{}
	m := startWorkspace(t, ws, "default", "us-east-1")
	m = drive(m, ctrl('p'))
	if m.dialog != dialogProfile {
		t.Fatal("ctrl+p should open the profile picker")
	}
	m = drive(m, append(typeKeys("prod"), tea.KeyPressMsg{Code: tea.KeyEnter})...)
	if m.dialog != dialogNone || ws.opened[len(ws.opened)-1] != (openCall{"prod", "sa-east-1"}) {
		t.Fatalf("opened = %v", ws.opened)
	}
	if !strings.Contains(header(m), "profile: prod") || m.sections[0].Title() != "Instances prod" {
		t.Error("the new environment should replace the sections")
	}
	if ws.shared.listened && ws.shared.inits != 0 {
		t.Error("a reused section must not be re-initialized")
	}
}

func TestSwitchProfileWithoutRegionKeepsCurrent(t *testing.T) {
	ws := &fakeWorkspace{}
	m := startWorkspace(t, ws, "default", "us-east-1")
	drive(m, append(append([]tea.Msg{ctrl('p')}, typeKeys("noregion")...), tea.KeyPressMsg{Code: tea.KeyEnter})...)
	if got := ws.opened[len(ws.opened)-1]; got != (openCall{"noregion", "us-east-1"}) {
		t.Errorf("opened %v, want the current region kept", got)
	}
}

func TestSwitchRegion(t *testing.T) {
	ws := &fakeWorkspace{}
	m := startWorkspace(t, ws, "default", "us-east-1")
	m = drive(m, ctrl('r'), tea.KeyPressMsg{Code: tea.KeyDown}, tea.KeyPressMsg{Code: tea.KeyEnter})
	if got := ws.opened[len(ws.opened)-1]; got != (openCall{"default", "sa-east-1"}) {
		t.Errorf("opened %v", got)
	}
	if !strings.Contains(header(m), "region: sa-east-1") {
		t.Errorf("header = %q", header(m))
	}
}

func TestMissingRegionOpensRegionPicker(t *testing.T) {
	m := startWorkspace(t, &fakeWorkspace{}, "noregion", "")
	if m.dialog != dialogRegion {
		t.Fatal("a profile without region should ask for one")
	}
	m = drive(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.region != "us-east-1" || len(m.sections) == 0 {
		t.Errorf("region = %q, sections = %d", m.region, len(m.sections))
	}
}

func TestStaleEnvironmentIsIgnored(t *testing.T) {
	ws := &fakeWorkspace{}
	m := startWorkspace(t, ws, "default", "us-east-1")
	old, _ := ws.Open("stale", "us-east-1")
	m = drive(m, envOpenedMsg{seq: m.seq - 1, profile: "stale", env: old})
	if m.profile != "default" {
		t.Error("an older switch must not override the current one")
	}
}

func TestCredentialFailureShowsAlert(t *testing.T) {
	ws := &fakeWorkspace{verifyErr: fmt.Errorf("%w: token expired", domain.ErrUnauthenticated)}
	m := startWorkspace(t, ws, "prod", "sa-east-1")
	if m.dialog != dialogAlert || m.alert.Kind != section.AlertUnauthenticated {
		t.Fatalf("dialog = %v, alert = %+v", m.dialog, m.alert)
	}
	screen := ansi.Strip(m.render())
	for _, want := range []string{"Credentials problem", "aws sso login", "token expired"} {
		if !strings.Contains(screen, want) {
			t.Errorf("alert missing %q", want)
		}
	}
	if !strings.Contains(header(m), "credentials problem") {
		t.Error("the header should flag the problem after the alert is closed too")
	}
	m = drive(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.dialog != dialogNone {
		t.Error("enter should close the alert")
	}
}

func TestAccessDeniedFromSectionShowsAlert(t *testing.T) {
	m := startWorkspace(t, &fakeWorkspace{}, "prod", "sa-east-1")
	err := fmt.Errorf("listing instances: %w", fmt.Errorf("%w: UnauthorizedOperation", domain.ErrAccessDenied))
	m = drive(m, run(section.Error(err))...)
	if m.dialog != dialogAlert || !strings.Contains(ansi.Strip(m.render()), "Permission denied") {
		t.Error("access denied should open an alert")
	}
}

func TestPlainErrorsStayInStatusBar(t *testing.T) {
	m := startWorkspace(t, &fakeWorkspace{}, "prod", "sa-east-1")
	m = drive(m, run(section.Error(errors.New("throttled")))...)
	if m.dialog != dialogNone || m.status.Text != "throttled" {
		t.Error("ordinary errors should not open a dialog")
	}
}

func TestOpenErrorKeepsCurrentEnvironment(t *testing.T) {
	ws := &fakeWorkspace{}
	m := startWorkspace(t, ws, "default", "us-east-1")
	ws.openErr = errors.New("profile not found")
	m = drive(m, ctrl('r'), tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.region != "us-east-1" || m.sections[0].Title() != "Instances default" || !m.status.Error {
		t.Errorf("a failed switch must keep the current environment; region = %q", m.region)
	}
}

func TestRawModeDoesNotInterceptSelectors(t *testing.T) {
	m := startWorkspace(t, &fakeWorkspace{}, "default", "us-east-1")
	m = drive(m, tea.KeyPressMsg{Code: tea.KeyEnter}) // focus content
	m.sections[0].(*fakeSection).mode = section.ModeRaw
	m = drive(m, ctrl('p'))
	if m.dialog != dialogNone {
		t.Error("ctrl+p belongs to the attached terminal")
	}
}

func TestHeaderClickOpensPickers(t *testing.T) {
	m := startWorkspace(t, &fakeWorkspace{}, "default", "us-east-1")
	h := header(m)
	m2 := drive(m, tea.MouseClickMsg{X: strings.Index(h, "profile:"), Y: 0, Button: tea.MouseLeft})
	if m2.dialog != dialogProfile {
		t.Error("clicking the profile chip should open the profile picker")
	}
	m3 := drive(m, tea.MouseClickMsg{X: strings.Index(h, "region:"), Y: 0, Button: tea.MouseLeft})
	if m3.dialog != dialogRegion {
		t.Error("clicking the region chip should open the region picker")
	}
	// A click outside the dialog cancels it.
	m3 = drive(m3, tea.MouseClickMsg{X: 0, Y: 29, Button: tea.MouseLeft})
	if m3.dialog != dialogNone {
		t.Error("clicking outside should close the picker")
	}
}

func TestPickerMouseChoice(t *testing.T) {
	ws := &fakeWorkspace{}
	m := startWorkspace(t, ws, "default", "us-east-1")
	m = drive(m, ctrl('r'))
	x, y, _, _ := m.dialogRect()
	// border (1) + title, input, blank (3) → second region is at +5.
	drive(m, tea.MouseClickMsg{X: x + 3, Y: y + 5, Button: tea.MouseLeft})
	if got := ws.opened[len(ws.opened)-1]; got.region != "sa-east-1" {
		t.Errorf("clicking sa-east-1 opened %v", got)
	}
}
