package tui

import (
	"context"
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"infra/internal/domain"
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
