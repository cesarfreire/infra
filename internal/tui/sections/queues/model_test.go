package queues

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"infra/internal/domain"
	"infra/internal/tui/section"
	"infra/internal/tui/theme"
)

type fakeService struct{ peeks int }

func (f *fakeService) List(context.Context) ([]domain.Queue, error) {
	return []domain.Queue{
		{Name: "orders", Visible: 12, InFlight: 3, DeadLetterQueue: "orders-dlq", MaxReceiveCount: 5},
		{Name: "orders-dlq", Visible: 2},
	}, nil
}

func (f *fakeService) Peek(context.Context, domain.Queue) ([]domain.Message, error) {
	f.peeks++
	return []domain.Message{{ID: "m1", Body: `{"order":1,"items":[1,2]}`, SentAt: time.Now(), ReceiveCount: 2,
		Attributes: map[string]string{"type": "created"}}}, nil
}

func (f *fakeService) Metrics(context.Context, domain.Queue, domain.MetricWindow) ([]domain.Series, error) {
	return nil, nil
}

func (f *fakeService) OpenConsole(domain.Queue) (string, error) { return "", nil }

func press(k string) tea.KeyPressMsg {
	switch k {
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
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
	m.SetSize(120, 30)
	m.Focus()
	send(m, run(m.Init())...)
	return m, svc
}

func TestList(t *testing.T) {
	m, _ := newModel(t)
	view := m.View()
	for _, want := range []string{"orders", "12", "orders-dlq"} {
		if !strings.Contains(view, want) {
			t.Errorf("list missing %q:\n%s", want, view)
		}
	}
}

func TestPeekNeedsConfirmation(t *testing.T) {
	m, svc := newModel(t)
	out := send(m, press("m"))
	if len(out) != 1 {
		t.Fatalf("out = %v", out)
	}
	c, ok := out[0].(section.ConfirmMsg)
	if !ok || !strings.Contains(c.Body, "5 times move to orders-dlq") || svc.peeks != 0 {
		t.Fatalf("expected a confirmation mentioning the DLQ, got %#v", out[0])
	}
	send(m, c.Yes())
	if svc.peeks != 1 || m.view != viewMessages || !strings.Contains(m.View(), `{"order":1`) {
		t.Fatalf("peek should show the messages:\n%s", m.View())
	}
	send(m, press("enter"))
	view := m.View()
	if !strings.Contains(view, `"items": [`) || !strings.Contains(view, "created") {
		t.Errorf("message view should indent JSON and show attributes:\n%s", view)
	}
	send(m, press("esc"))
	send(m, press("esc"))
	if m.view != viewList {
		t.Error("esc twice should return to the queues")
	}
}

func TestReadOnlyBlocksPeek(t *testing.T) {
	m, _ := newModel(t)
	m.deps.ReadOnly = true
	out := send(m, press("m"))
	if st, ok := out[0].(section.StatusMsg); !ok || !strings.Contains(st.Text, "read-only") {
		t.Errorf("out = %v", out)
	}
}

func TestDetailsAndReveal(t *testing.T) {
	m, _ := newModel(t)
	send(m, section.RevealMsg{Bookmark: domain.Bookmark{Kind: domain.KindQueue, ID: "orders-dlq"}})
	send(m, press("d"))
	if m.view != viewDetails || !strings.Contains(m.View(), "orders-dlq") || !strings.Contains(m.View(), "2 visible") {
		t.Errorf("details:\n%s", m.View())
	}
}
