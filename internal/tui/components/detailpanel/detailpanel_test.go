package detailpanel

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"infra/internal/app"
	"infra/internal/domain"
	"infra/internal/tui/section"
	"infra/internal/tui/theme"
)

type recorder struct {
	windows []domain.MetricWindow
	err     error
}

func (r *recorder) fetch(_ context.Context, w domain.MetricWindow) ([]domain.Series, error) {
	r.windows = append(r.windows, w)
	if r.err != nil {
		return nil, r.err
	}
	now := time.Now()
	return []domain.Series{
		{Label: "CPU", Unit: domain.UnitPercent, Points: []domain.Point{{Time: now.Add(-time.Minute), Value: 80}}},
		{Label: "Memory", Unit: domain.UnitPercent, Points: []domain.Point{{Time: now.Add(-time.Minute), Value: 30}}},
	}, nil
}

func open(t *testing.T, r *recorder, width int) (*Model, tea.Cmd) {
	t.Helper()
	m := New(theme.Default())
	m.SetSize(width, 40)
	cmd := m.Open("HEADER", r.fetch, "enable something")
	return m, m.Update(cmd())
}

func TestLoadsAndLaysOutCharts(t *testing.T) {
	r := &recorder{}
	m, tick := open(t, r, 100)
	out := ansi.Strip(m.View())
	if !strings.HasPrefix(out, "HEADER") || !strings.Contains(out, "now 80%") || !strings.Contains(out, "now 30%") {
		t.Fatalf("view:\n%s", out)
	}
	// Wide panels put two charts side by side.
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "CPU") && !strings.Contains(line, "Memory") {
			t.Errorf("charts should share a row at width 100: %q", line)
		}
	}
	if tick == nil {
		t.Error("a refresh should be scheduled")
	}
	if r.windows[0] != app.MetricWindows[app.DefaultMetricWindow] {
		t.Errorf("window = %v", r.windows[0])
	}
}

func TestWindowKeyCyclesRange(t *testing.T) {
	r := &recorder{}
	m, _ := open(t, r, 60)
	cmd, ok := m.HandleKey(tea.KeyPressMsg{Code: 't', Text: "t"})
	if !ok {
		t.Fatal("t should be handled")
	}
	m.Update(cmd())
	if got := r.windows[len(r.windows)-1]; got != app.MetricWindows[app.DefaultMetricWindow+1] {
		t.Errorf("window = %v", got)
	}
	if _, ok := m.HandleKey(tea.KeyPressMsg{Code: 'c', Text: "c"}); ok {
		t.Error("other keys belong to the section")
	}
}

func TestTickRefreshesOnlyWhileOpen(t *testing.T) {
	r := &recorder{}
	m, _ := open(t, r, 60)
	tick := tickMsg{id: m.id, seq: m.seq}
	if cmd := m.Update(tick); cmd == nil {
		t.Fatal("an open panel should refresh on tick")
	}
	m.Close()
	if cmd := m.Update(tickMsg{id: m.id, seq: m.seq}); cmd != nil {
		t.Error("a closed panel must not refresh")
	}
	other := New(theme.Default())
	if cmd := other.Update(tick); cmd != nil {
		t.Error("ticks of another panel must be ignored")
	}
}

func TestErrorsAlertOnceAndKeepRetrying(t *testing.T) {
	r := &recorder{err: errors.New("AccessDenied")}
	m := New(theme.Default())
	m.SetSize(60, 20)
	cmd := m.Update(m.Open("H", r.fetch, "")())
	var status bool
	for _, msg := range runBatch(cmd) {
		if _, ok := msg.(section.StatusMsg); ok {
			status = true
		}
	}
	if !status || !strings.Contains(m.View(), "AccessDenied") {
		t.Error("the first failure should be reported and shown")
	}
	refresh := m.load(true)
	if cmd := m.Update(refresh()); cmd == nil {
		t.Error("a failed refresh should still schedule the next one")
	}
}

func TestEmptyShowsHint(t *testing.T) {
	m := New(theme.Default())
	m.SetSize(60, 20)
	m.Update(m.Open("H", func(context.Context, domain.MetricWindow) ([]domain.Series, error) {
		return []domain.Series{{Label: "CPU"}}, nil
	}, "enable insights")())
	if !strings.Contains(m.View(), "enable insights") {
		t.Errorf("view:\n%s", m.View())
	}
}

func runBatch(cmd tea.Cmd) []tea.Msg {
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
				out = append(out, runBatch(c)...)
			}
			return out
		}
		return []tea.Msg{msg}
	case <-time.After(50 * time.Millisecond):
		return nil
	}
}
