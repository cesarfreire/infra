package config

import (
	"context"
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

type fakeService struct{ reads []string }

func (f *fakeService) Parameters(context.Context) ([]domain.Parameter, error) {
	return []domain.Parameter{
		{Name: "/prod/api/url", Type: domain.ParameterString, Version: 2},
		{Name: "/prod/db/password", Type: domain.ParameterSecureString, Version: 5},
		{Name: "/prod/hosts", Type: domain.ParameterStringList},
	}, nil
}

func (f *fakeService) ParameterValue(_ context.Context, p domain.Parameter) (string, error) {
	f.reads = append(f.reads, p.Name)
	switch p.Name {
	case "/prod/db/password":
		return "s3cr3t", nil
	case "/prod/hosts":
		return "a.example,b.example", nil
	}
	return "https://api.example.com", nil
}

func (f *fakeService) Secrets(context.Context) ([]domain.Secret, error) {
	return []domain.Secret{{Name: "prod/api", ARN: "arn:prod/api", RotationEnabled: true}}, nil
}

func (f *fakeService) SecretValue(_ context.Context, s domain.Secret) (domain.SecretValue, error) {
	f.reads = append(f.reads, s.Name)
	return domain.SecretValue{Text: `{"user":"api","password":"x"}`}, nil
}

func (f *fakeService) OpenParameterConsole(domain.Parameter) (string, error) { return "", nil }
func (f *fakeService) OpenSecretConsole(domain.Secret) (string, error)       { return "", nil }

func press(k string) tea.KeyPressMsg {
	switch k {
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "down":
		return tea.KeyPressMsg{Code: tea.KeyDown}
	case "tab":
		return tea.KeyPressMsg{Code: tea.KeyTab}
	}
	return tea.KeyPressMsg{Code: rune(k[0]), Text: k}
}

// send runs commands like the runtime; messages for the root are returned.
func send(m *Model, msgs ...tea.Msg) []tea.Msg {
	var out []tea.Msg
	for len(msgs) > 0 {
		msg := msgs[0]
		msgs = msgs[1:]
		_, cmd := m.Update(msg)
		for _, r := range run(cmd) {
			switch r.(type) {
			case section.StatusMsg, section.AlertMsg, section.ConfirmMsg:
				out = append(out, r)
			default:
				msgs = append(msgs, r)
			}
		}
	}
	return out
}

func run(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	done := make(chan tea.Msg, 1)
	go func() { done <- cmd() }()
	select {
	case msg := <-done:
		if b, ok := msg.(tea.BatchMsg); ok {
			var out []tea.Msg
			for _, c := range b {
				out = append(out, run(c)...)
			}
			return out
		}
		if msg == nil || strings.Contains(fmt.Sprintf("%T", msg), "Tick") {
			return nil
		}
		return []tea.Msg{msg}
	case <-time.After(100 * time.Millisecond):
		return nil
	}
}

func newModel(t *testing.T) (*Model, *fakeService) {
	t.Helper()
	svc := &fakeService{}
	m := New(svc, section.MemoryDeps(theme.Default()))
	m.SetSize(100, 30)
	m.Focus()
	send(m, run(m.Init())...)
	return m, svc
}

func confirmIn(t *testing.T, out []tea.Msg) section.ConfirmMsg {
	t.Helper()
	for _, msg := range out {
		if c, ok := msg.(section.ConfirmMsg); ok {
			return c
		}
	}
	t.Fatalf("expected a confirmation in %v", out)
	return section.ConfirmMsg{}
}

func TestPlainParameterShowsValue(t *testing.T) {
	m, svc := newModel(t)
	send(m, press("enter"))
	if view := ansi.Strip(m.View()); !strings.Contains(view, "https://api.example.com") || len(svc.reads) != 1 {
		t.Errorf("a String parameter is shown right away:\n%s", view)
	}
}

func TestSecureParameterNeedsConfirmedReveal(t *testing.T) {
	m, svc := newModel(t)
	send(m, press("down"))
	send(m, press("enter"))
	view := ansi.Strip(m.View())
	if strings.Contains(view, "s3cr3t") || !strings.Contains(view, "press v to reveal") || len(svc.reads) != 0 {
		t.Fatalf("a SecureString must stay hidden:\n%s", view)
	}
	c := confirmIn(t, send(m, press("v")))
	if !strings.Contains(c.Body, "/prod/db/password") || len(svc.reads) != 0 {
		t.Fatal("nothing is read before confirming")
	}
	send(m, c.Yes())
	if !strings.Contains(ansi.Strip(m.View()), "s3cr3t") {
		t.Errorf("confirmed: the value should show:\n%s", m.View())
	}
	send(m, press("esc"))
	if m.value != "" {
		t.Error("leaving the details must forget the value")
	}
}

func TestRevealAfterLeavingIsDropped(t *testing.T) {
	m, svc := newModel(t)
	send(m, press("down"))
	send(m, press("enter"))
	c := confirmIn(t, send(m, press("v")))
	send(m, press("esc")) // left before confirming
	send(m, c.Yes())
	if len(svc.reads) != 0 || m.value != "" {
		t.Errorf("a stale confirmation must not read the value; reads = %v", svc.reads)
	}
}

func TestStringListIsSplit(t *testing.T) {
	m, _ := newModel(t)
	send(m, press("down"), press("down"))
	send(m, press("enter"))
	var lines []string
	for _, l := range strings.Split(ansi.Strip(m.View()), "\n") {
		lines = append(lines, strings.TrimRight(l, " "))
	}
	if view := strings.Join(lines, "\n"); !strings.Contains(view, "a.example\nb.example") {
		t.Errorf("view:\n%s", view)
	}
}

func TestSecretsTabAndJSON(t *testing.T) {
	m, svc := newModel(t)
	send(m, press("tab"))
	if m.source != sourceSecrets || !strings.Contains(ansi.Strip(m.View()), "prod/api") {
		t.Fatalf("tab should show the secrets:\n%s", m.View())
	}
	send(m, press("enter"))
	if len(svc.reads) != 0 {
		t.Fatal("secrets are never read without a reveal")
	}
	send(m, confirmIn(t, send(m, press("v"))).Yes())
	if view := ansi.Strip(m.View()); !strings.Contains(view, `"user": "api"`) {
		t.Errorf("JSON secrets are indented:\n%s", view)
	}
}

func TestFavoriteAndReveal(t *testing.T) {
	m, _ := newModel(t)
	send(m, press("*"))
	if !strings.Contains(m.View(), "★ /prod/api/url") {
		t.Errorf("expected a star:\n%s", m.View())
	}
	send(m, section.RevealMsg{Bookmark: domain.Bookmark{Kind: domain.KindSecret, ID: "prod/api"}})
	if s, ok := m.secrets.Selected(); m.source != sourceSecrets || !ok || s.Name != "prod/api" {
		t.Errorf("reveal should open the secrets tab on prod/api; source %v, selected %v", m.source, s)
	}
}

func TestSearchItemsIncludeSecretsOnceLoaded(t *testing.T) {
	m, _ := newModel(t)
	if got := len(m.SearchItems()); got != 3 {
		t.Fatalf("parameters only: %d", got)
	}
	send(m, press("tab"))
	items := m.SearchItems()
	if len(items) != 4 || items[3].Bookmark.Kind != domain.KindSecret {
		t.Errorf("items = %+v", items)
	}
}
