// Package detailpanel is the scrollable details view of a resource: its
// fields followed by monitoring charts that load asynchronously and refresh
// every minute while the panel is open.
package detailpanel

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"infra/internal/app"
	"infra/internal/domain"
	"infra/internal/tui/components/chart"
	"infra/internal/tui/section"
	"infra/internal/tui/theme"
)

const (
	refreshEvery  = time.Minute
	fetchTimeout  = 20 * time.Second
	chartHeight   = 6
	minChartWidth = 40
	chartGap      = 3
)

// Fetch reads the series of the resource being shown.
type Fetch func(ctx context.Context, w domain.MetricWindow) ([]domain.Series, error)

// ids tells apart the panels of different sections: their messages are
// broadcast to every section.
var ids atomic.Int64

type loadedMsg struct {
	id, seq int64
	series  []domain.Series
	err     error
	refresh bool
}

type tickMsg struct{ id, seq int64 }

// Model is the panel state. Use New.
type Model struct {
	id    int64
	seq   int64 // bumped on every load and on Close; stale messages are dropped
	theme theme.Theme
	vp    viewport.Model
	keys  struct{ window, refresh, scroll key.Binding }
	now   func() time.Time

	open    bool
	header  string
	fetch   Fetch
	hint    string
	window  int
	series  []domain.Series
	loading bool
	err     error
	updated time.Time
	width   int
	prefs   section.Prefs // remembers the time range; may be nil
}

const windowPref = "metrics.window"

// New returns a closed panel.
func New(th theme.Theme) *Model {
	m := &Model{id: ids.Add(1), theme: th, vp: viewport.New(), now: time.Now, window: app.DefaultMetricWindow}
	m.keys.window = key.NewBinding(key.WithKeys("t"), key.WithHelp("t", "time range"))
	m.keys.refresh = key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "refresh"))
	m.keys.scroll = key.NewBinding(key.WithKeys("up", "down", "pgup", "pgdown", "home", "end"), key.WithHelp("↑↓", "scroll"))
	return m
}

// WithPrefs makes the panel remember the chosen time range between runs.
func (m *Model) WithPrefs(p section.Prefs) *Model {
	m.prefs = p
	if p == nil {
		return m
	}
	saved := p.GetGlobal(windowPref)
	for i, w := range app.MetricWindows {
		if w.Label == saved {
			m.window = i
		}
	}
	return m
}

// Open shows header (the rendered fields) and starts loading metrics with
// fetch; hint explains an empty result (e.g. a feature to enable).
func (m *Model) Open(header string, fetch Fetch, hint string) tea.Cmd {
	m.open = true
	m.header, m.fetch, m.hint = header, fetch, hint
	m.series, m.err = nil, nil
	m.vp.GotoTop()
	return m.load(false)
}

// SetHeader replaces the fields shown above the charts (e.g. a resource
// re-read while the panel is open), keeping the scroll position.
func (m *Model) SetHeader(header string) {
	m.header = header
	m.render()
}

// IsOpen reports whether the panel is showing a resource.
func (m *Model) IsOpen() bool { return m.open }

// Close stops refreshing. Call it whenever the details view is left.
func (m *Model) Close() {
	m.open = false
	m.seq++
}

// SetSize sets the size of the panel.
func (m *Model) SetSize(width, height int) {
	m.width = width
	m.vp.SetWidth(width)
	m.vp.SetHeight(max(1, height))
	m.render()
}

func (m *Model) load(refresh bool) tea.Cmd {
	m.seq++
	m.loading = true
	m.render()
	id, seq, fetch, w := m.id, m.seq, m.fetch, app.MetricWindows[m.window]
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), fetchTimeout)
		defer cancel()
		series, err := fetch(ctx, w)
		return loadedMsg{id: id, seq: seq, series: series, err: err, refresh: refresh}
	}
}

// Update handles the panel's async messages.
func (m *Model) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case loadedMsg:
		if msg.id != m.id || msg.seq != m.seq || !m.open {
			return nil
		}
		m.loading = false
		id, seq := m.id, m.seq
		tick := tea.Tick(refreshEvery, func(time.Time) tea.Msg { return tickMsg{id: id, seq: seq} })
		if msg.err != nil {
			m.err = msg.err
			m.render()
			if msg.refresh {
				return tick // already reported; keep the charts and retry later
			}
			return tea.Batch(section.Error(msg.err), tick)
		}
		m.series, m.err, m.updated = msg.series, nil, m.now()
		m.render()
		return tick
	case tickMsg:
		if msg.id == m.id && msg.seq == m.seq && m.open {
			return m.load(true)
		}
	}
	return nil
}

// HandleKey handles the panel's keys and reports whether it used msg.
func (m *Model) HandleKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	switch {
	case key.Matches(msg, m.keys.window):
		m.window = (m.window + 1) % len(app.MetricWindows)
		m.series = nil
		if m.prefs != nil {
			_ = m.prefs.SetGlobal(windowPref, app.MetricWindows[m.window].Label) // best effort
		}
		return m.load(false), true
	case key.Matches(msg, m.keys.refresh):
		return m.load(false), true
	case key.Matches(msg, m.keys.scroll):
		var cmd tea.Cmd
		m.vp, cmd = m.vp.Update(msg)
		return cmd, true
	}
	return nil, false
}

// Help returns the panel's bindings.
func (m *Model) Help() []key.Binding {
	return []key.Binding{m.keys.scroll, m.keys.window, m.keys.refresh}
}

func (m *Model) View() string { return m.vp.View() }

func (m *Model) render() {
	offset := m.vp.YOffset()
	m.vp.SetContent(m.header + "\n" + m.monitoring())
	m.vp.SetYOffset(offset)
}

func (m *Model) monitoring() string {
	th := m.theme
	w := app.MetricWindows[m.window]
	status := ""
	switch {
	case m.loading:
		status = " · loading…"
	case !m.updated.IsZero():
		status = " · updated " + m.updated.Format("15:04:05")
	}
	head := th.Title.Render("Monitoring") + " " +
		th.Muted.Render(fmt.Sprintf("last %s, %s points%s · t: time range", w.Label, period(w.Period), status))

	var body string
	switch {
	case m.series == nil && m.err != nil:
		body = th.StatusError.Render(m.err.Error())
	case m.series == nil:
		body = th.Muted.Render("Loading metrics…")
	case allEmpty(m.series):
		hint := m.hint
		if hint == "" {
			hint = "No data points in this time range."
		}
		body = th.Muted.Render(hint)
	default:
		body = m.charts(w)
	}
	return head + "\n\n" + body
}

// charts lays the charts out in two columns when they fit.
func (m *Model) charts(w domain.MetricWindow) string {
	cols := 1
	if m.width >= 2*minChartWidth+chartGap {
		cols = 2
	}
	width := (m.width - (cols-1)*chartGap) / cols
	end := m.updated
	if end.IsZero() {
		end = m.now()
	}
	opts := chart.Options{Width: width, Height: chartHeight, Start: end.Add(-w.Range), End: end, Period: w.Period}

	var rows []string
	for i := 0; i < len(m.series); i += cols {
		var cells []string
		for j := i; j < min(i+cols, len(m.series)); j++ {
			if j > i {
				cells = append(cells, strings.Repeat(" ", chartGap))
			}
			cells = append(cells, lipgloss.NewStyle().Width(width).Render(chart.Render(m.theme, m.series[j], opts)))
		}
		rows = append(rows, lipgloss.JoinHorizontal(lipgloss.Top, cells...))
	}
	return strings.Join(rows, "\n\n")
}

func allEmpty(series []domain.Series) bool {
	for _, s := range series {
		if len(s.Points) > 0 {
			return false
		}
	}
	return true
}

func period(d time.Duration) string {
	if d >= time.Hour {
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	return fmt.Sprintf("%dm", int(d.Minutes()))
}
