package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"infra/internal/domain"
	"infra/internal/tui/section"
	"infra/internal/tui/theme"
)

type fakeUpdates struct {
	release domain.Release
	newer   bool
	err     error
}

func (f fakeUpdates) Check(context.Context) (domain.Release, bool, error) {
	return f.release, f.newer, f.err
}

func startWithUpdates(t *testing.T, u UpdateChecker, open func(string) error) Model {
	t.Helper()
	ws := &fakeWorkspace{shared: &fakeSection{title: "Sessions"}}
	m := New(Options{Title: "infra", Workspace: ws, Profile: "default", Region: "us-east-1", Updates: u, OpenURL: open}, theme.Default())
	m = drive(m, tea.WindowSizeMsg{Width: 120, Height: 30})
	return drive(m, run(m.Init())...)
}

func TestNewerReleaseShowsInHeader(t *testing.T) {
	var opened string
	release := domain.Release{Version: "v1.3.0", URL: "https://github.com/acme/infra/releases/tag/v1.3.0"}
	m := startWithUpdates(t, fakeUpdates{release: release, newer: true}, func(u string) error { opened = u; return nil })
	h := header(m)
	if !strings.HasSuffix(strings.TrimRight(h, " "), "↑ v1.3.0 available") {
		t.Fatalf("header = %q", h)
	}
	drive(m, tea.MouseClickMsg{X: strings.Index(h, "↑"), Y: 0, Button: tea.MouseLeft})
	if opened != release.URL {
		t.Errorf("clicking the notice should open the release page; opened %q", opened)
	}
}

func TestNoNoticeWhenUpToDateOrFailing(t *testing.T) {
	for name, u := range map[string]fakeUpdates{
		"up to date": {release: domain.Release{Version: "v1.0.0"}},
		"error":      {err: errors.New("offline")},
	} {
		m := startWithUpdates(t, u, nil)
		if strings.Contains(header(m), "available") {
			t.Errorf("%s: header = %q", name, header(m))
		}
		if m.status.Error {
			t.Errorf("%s: update check failures must stay silent", name)
		}
	}
}

func TestNotifyUsesDesktopOrTerminal(t *testing.T) {
	var desktop []string
	m := New(Options{Title: "infra", Notify: func(title, body string) error {
		desktop = append(desktop, title+": "+body)
		return nil
	}}, theme.Default())
	for _, msg := range run(m.notify(section.NotifyMsg{Title: "Deploy", Body: "done"})) {
		if raw, ok := msg.(tea.RawMsg); ok && fmt.Sprint(raw.Msg) != "\a" {
			t.Errorf("with a desktop notifier only the bell is sent, got %q", raw.Msg)
		}
	}
	if len(desktop) != 1 || desktop[0] != "Deploy: done" {
		t.Errorf("desktop = %v", desktop)
	}

	m = New(Options{Title: "infra"}, theme.Default())
	msgs := run(m.notify(section.NotifyMsg{Title: "Deploy", Body: "done\nnow"}))
	if len(msgs) != 1 {
		t.Fatalf("msgs = %v", msgs)
	}
	if raw := fmt.Sprint(msgs[0].(tea.RawMsg).Msg); raw != "\a\x1b]9;Deploy: donenow\x07" {
		t.Errorf("without a notifier the terminal gets OSC 9 (control characters removed): %q", raw)
	}

	m = New(Options{Title: "infra", NoNotifications: true}, theme.Default())
	if m.notify(section.NotifyMsg{Title: "x"}) != nil {
		t.Error("notifications can be turned off")
	}
}

type progressSection struct{ fakeSection }

func (p *progressSection) Progress() *tea.ProgressBar {
	return tea.NewProgressBar(tea.ProgressBarDefault, 40)
}

func TestProgressOfActiveSection(t *testing.T) {
	m := New(Options{Title: "infra"}, theme.Default(), &progressSection{fakeSection{title: "A"}}, &fakeSection{title: "B"})
	m = drive(m, tea.WindowSizeMsg{Width: 100, Height: 30})
	if p := m.View().ProgressBar; p == nil || p.Value != 40 {
		t.Errorf("progress = %+v", p)
	}
	m = drive(m, tea.KeyPressMsg{Code: tea.KeyDown})
	if m.View().ProgressBar != nil {
		t.Error("only the active section's progress is shown")
	}
}
