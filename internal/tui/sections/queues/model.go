// Package queues is the menu section for message queues (SQS): backlog at a
// glance, details with charts, and peeking at messages.
package queues

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"

	"infra/internal/domain"
	"infra/internal/filter"
	"infra/internal/tui/components/detailpanel"
	"infra/internal/tui/components/details"
	"infra/internal/tui/components/listview"
	"infra/internal/tui/section"
	"infra/internal/tui/theme"
)

// Title is the menu label of this section.
const Title = "Queues"

const loadTimeout = 30 * time.Second

// Service is the subset of use cases this section needs. It is satisfied by
// *app.QueueService and by fakes in tests.
type Service interface {
	List(ctx context.Context) ([]domain.Queue, error)
	Peek(ctx context.Context, q domain.Queue) ([]domain.Message, error)
	Metrics(ctx context.Context, q domain.Queue, w domain.MetricWindow) ([]domain.Series, error)
	OpenConsole(q domain.Queue) (string, error)
}

type viewState int

const (
	viewList viewState = iota
	viewDetails
	viewMessages
	viewMessage
)

type queuesLoadedMsg struct {
	items []domain.Queue
	err   error
}

type peekedMsg struct {
	queue string
	msgs  []domain.Message
	err   error
}

type keyMap struct {
	Details  key.Binding
	Peek     key.Binding
	Open     key.Binding
	Console  key.Binding
	Favorite key.Binding
	Refresh  key.Binding
	Back     key.Binding
	Scroll   key.Binding
}

// Model implements section.Section.
type Model struct {
	svc   Service
	deps  section.Deps
	theme theme.Theme
	keys  keyMap

	list     *listview.Model[domain.Queue]
	messages *listview.Model[domain.Message]
	details  *detailpanel.Model
	viewer   viewport.Model

	view  viewState
	queue domain.Queue
}

// New creates the Queues section.
func New(svc Service, deps section.Deps) *Model {
	th := deps.Theme
	m := &Model{
		svc:      svc,
		deps:     deps,
		theme:    th,
		list:     listview.New("SQS queues", "queues", queueColumns, queueFields, queuePlaceholder, th),
		messages: listview.New("Messages", "messages", messageColumns, messageFields, "body or id", th),
		details:  detailpanel.New(th).WithPrefs(deps.Prefs),
		viewer:   viewport.New(),
		keys: keyMap{
			Details:  key.NewBinding(key.WithKeys("enter", "d"), key.WithHelp("enter/d", "details")),
			Peek:     key.NewBinding(key.WithKeys("m"), key.WithHelp("m", "peek messages")),
			Open:     key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "view message")),
			Console:  key.NewBinding(key.WithKeys("o"), key.WithHelp("o", "console")),
			Favorite: key.NewBinding(key.WithKeys("*"), key.WithHelp("*", "favorite")),
			Refresh:  key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "refresh")),
			Back:     key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
			Scroll:   key.NewBinding(key.WithKeys("up", "down", "pgup", "pgdown"), key.WithHelp("↑↓", "scroll")),
		},
	}
	m.list.SetMarker(func(q domain.Queue) string { return deps.Star(bookmark(q)) })
	return m
}

func bookmark(q domain.Queue) domain.Bookmark {
	return domain.Bookmark{Kind: domain.KindQueue, ID: q.Name, Label: q.Name}
}

func (m *Model) Title() string { return Title }
func (m *Model) Init() tea.Cmd { return m.load() }

func (m *Model) Focus() {
	m.list.Focus()
	m.messages.Focus()
}

func (m *Model) Blur() {
	m.list.Blur()
	m.messages.Blur()
}

func (m *Model) SetSize(width, height int) {
	m.list.SetSize(width, height)
	m.messages.SetSize(width, height)
	m.details.SetSize(width, height)
	m.viewer.SetWidth(width)
	m.viewer.SetHeight(max(1, height-2))
}

func (m *Model) InputMode() section.InputMode {
	if m.view == viewList && !m.list.Capturing() {
		return section.ModeNormal
	}
	return section.ModeCapture
}

func (m *Model) load() tea.Cmd {
	svc := m.svc
	return tea.Batch(m.list.StartLoading(), func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), loadTimeout)
		defer cancel()
		items, err := svc.List(ctx)
		return queuesLoadedMsg{items: items, err: err}
	})
}

func (m *Model) Update(msg tea.Msg) (section.Section, tea.Cmd) {
	switch msg := msg.(type) {
	case queuesLoadedMsg:
		if msg.err != nil {
			m.list.SetError(msg.err)
			return m, section.Error(msg.err)
		}
		m.list.SetItems(msg.items)
		return m, section.Status(fmt.Sprintf("Loaded %d queues", len(msg.items)))
	case peekedMsg:
		if msg.queue != m.queue.Name || m.view != viewMessages {
			return m, nil
		}
		if msg.err != nil {
			m.messages.SetError(msg.err)
			return m, section.Error(msg.err)
		}
		m.messages.SetItems(msg.msgs)
		return m, section.Status(fmt.Sprintf("Peeked %d messages; they are visible to consumers again in ~1s", len(msg.msgs)))
	case startPeekMsg:
		return m, m.peek(msg.queue)
	case section.RevealMsg:
		if msg.Bookmark.Kind == domain.KindQueue {
			m.details.Close()
			m.view = viewList
			m.list.Reveal(func(q domain.Queue) bool { return q.Name == msg.Bookmark.ID })
		}
		return m, nil
	case tea.KeyPressMsg:
		return m, m.handleKey(msg)
	}
	return m, tea.Batch(m.list.Update(msg), m.messages.Update(msg), m.details.Update(msg))
}

func (m *Model) handleKey(msg tea.KeyPressMsg) tea.Cmd {
	switch m.view {
	case viewDetails:
		if cmd, ok := m.details.HandleKey(msg); ok {
			return cmd
		}
		switch {
		case key.Matches(msg, m.keys.Back):
			m.details.Close()
			m.view = viewList
		case key.Matches(msg, m.keys.Peek):
			m.details.Close()
			return m.confirmPeek(m.queue)
		case key.Matches(msg, m.keys.Console):
			return m.openConsole(m.queue)
		}
		return nil
	case viewMessages:
		if m.messages.Filtering() {
			return m.messages.Update(msg)
		}
		switch {
		case key.Matches(msg, m.keys.Back):
			if !m.messages.Query().Empty() {
				return m.messages.Update(msg)
			}
			m.view = viewList
			return nil
		case key.Matches(msg, m.keys.Open):
			if msg, ok := m.messages.Selected(); ok {
				m.viewer.SetContent(m.messageView(msg))
				m.viewer.GotoTop()
				m.view = viewMessage
			}
			return nil
		case key.Matches(msg, m.keys.Peek):
			return m.confirmPeek(m.queue)
		}
		return m.messages.Update(msg)
	case viewMessage:
		if key.Matches(msg, m.keys.Back) {
			m.view = viewMessages
			return nil
		}
		var cmd tea.Cmd
		m.viewer, cmd = m.viewer.Update(msg)
		return cmd
	}

	if m.list.Filtering() {
		return m.list.Update(msg)
	}
	if key.Matches(msg, m.keys.Refresh) {
		return m.load()
	}
	if q, ok := m.list.Selected(); ok {
		switch {
		case key.Matches(msg, m.keys.Favorite):
			cmd := m.deps.ToggleFavorite(bookmark(q))
			m.list.Refresh()
			return cmd
		case key.Matches(msg, m.keys.Details):
			return m.openDetails(q)
		case key.Matches(msg, m.keys.Peek):
			return m.confirmPeek(q)
		case key.Matches(msg, m.keys.Console):
			return m.openConsole(q)
		}
	}
	return m.list.Update(msg)
}

func (m *Model) openDetails(q domain.Queue) tea.Cmd {
	m.deps.Visit(bookmark(q))
	m.queue = q
	m.view = viewDetails
	svc := m.svc
	return m.details.Open(m.detailsHeader(q), func(ctx context.Context, w domain.MetricWindow) ([]domain.Series, error) {
		return svc.Metrics(ctx, q, w)
	}, "")
}

// confirmPeek warns that peeking counts as a receive before doing it.
func (m *Model) confirmPeek(q domain.Queue) tea.Cmd {
	if cmd := m.deps.BlockWrite("peek messages of " + q.Name); cmd != nil {
		return cmd
	}
	body := fmt.Sprintf("Receive up to 10 messages of %s without deleting them? Each one's receive count goes up "+
		"and it is hidden from consumers for about a second.", q.Name)
	if q.DeadLetterQueue != "" {
		body += fmt.Sprintf(" Messages received %d times move to %s.", q.MaxReceiveCount, q.DeadLetterQueue)
	}
	return section.Confirm("Peek messages", body, func() tea.Msg { return startPeekMsg{queue: q} })
}

// startPeekMsg runs the peek once confirmed, switching to the messages view.
type startPeekMsg struct{ queue domain.Queue }

func (m *Model) peek(q domain.Queue) tea.Cmd {
	m.deps.Visit(bookmark(q))
	m.queue = q
	m.view = viewMessages
	m.messages.Reset()
	m.messages.SetTitle(q.Name + " › messages")
	svc := m.svc
	return tea.Batch(m.messages.StartLoading(), func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), loadTimeout)
		defer cancel()
		msgs, err := svc.Peek(ctx, q)
		return peekedMsg{queue: q.Name, msgs: msgs, err: err}
	})
}

func (m *Model) openConsole(q domain.Queue) tea.Cmd {
	svc := m.svc
	return func() tea.Msg {
		url, err := svc.OpenConsole(q)
		if err != nil {
			return section.StatusMsg{Text: fmt.Sprintf("%v (%s)", err, url), Error: true}
		}
		return section.StatusMsg{Text: "Opened console for " + q.Name}
	}
}

func (m *Model) Help() []key.Binding {
	k := m.keys
	switch m.view {
	case viewDetails:
		return append(m.details.Help(), k.Peek, k.Console, k.Back)
	case viewMessages:
		if m.messages.Filtering() {
			return m.messages.Help()
		}
		return append([]key.Binding{k.Open, k.Peek, k.Back}, m.messages.Help()...)
	case viewMessage:
		return []key.Binding{k.Scroll, k.Back}
	}
	if m.list.Filtering() {
		return m.list.Help()
	}
	return append([]key.Binding{k.Details, k.Peek, k.Console, k.Favorite, k.Refresh}, m.list.Help()...)
}

func (m *Model) View() string {
	switch m.view {
	case viewDetails:
		return m.details.View()
	case viewMessages:
		return m.messages.View()
	case viewMessage:
		return m.theme.Title.Render(m.queue.Name) + " " + m.theme.Muted.Render("message") + "\n\n" + m.viewer.View()
	}
	return m.list.View()
}

func (m *Model) detailsHeader(q domain.Queue) string {
	dlq := "none"
	if q.DeadLetterQueue != "" {
		dlq = fmt.Sprintf("%s (after %d receives)", q.DeadLetterQueue, q.MaxReceiveCount)
	}
	created := ""
	if !q.CreatedAt.IsZero() {
		created = q.CreatedAt.Local().Format(time.DateTime)
	}
	return details.Render(m.theme, q.Name, "queue details", []details.Field{
		{Label: "Type", Value: queueType(q)},
		{Label: "Messages", Value: fmt.Sprintf("%d visible, %d in flight, %d delayed", q.Visible, q.InFlight, q.Delayed)},
		{Label: "Dead-letter queue", Value: dlq},
		{Label: "Visibility timeout", Value: q.VisibilityTimeout.String()},
		{Label: "Retention", Value: q.Retention.String()},
		{Label: "Encrypted", Value: strconv.FormatBool(q.Encrypted)},
		{Label: "Created", Value: created},
		{Label: "URL", Value: q.URL},
		{Label: "ARN", Value: q.ARN},
	})
}

// messageView shows a message body (indented when it is JSON) and its
// attributes.
func (m *Model) messageView(msg domain.Message) string {
	body := msg.Body
	var buf bytes.Buffer
	if json.Indent(&buf, []byte(body), "", "  ") == nil {
		body = buf.String()
	}
	fields := []details.Field{
		{Label: "ID", Value: msg.ID},
		{Label: "Sent", Value: msg.SentAt.Local().Format(time.DateTime)},
		{Label: "Receives", Value: strconv.Itoa(msg.ReceiveCount)},
		{Label: "Group", Value: msg.GroupID},
	}
	var attrs []details.Field
	for _, k := range slices.Sorted(maps.Keys(msg.Attributes)) {
		attrs = append(attrs, details.Field{Label: k, Value: msg.Attributes[k]})
	}
	return details.Render(m.theme, "Message", "", fields, details.Section{Title: "Attributes", Fields: attrs}) +
		"\n" + m.theme.Title.Render("Body") + "\n" + body
}

func queueType(q domain.Queue) string {
	if q.FIFO {
		return "FIFO"
	}
	return "standard"
}

var queueColumns = []listview.Column[domain.Queue]{
	{Title: "Queue", Value: func(q domain.Queue) string { return q.Name }},
	{Title: "Type", Width: 8, Value: queueType},
	{Title: "Visible", Width: 8, Value: func(q domain.Queue) string { return strconv.Itoa(q.Visible) }},
	{Title: "In flight", Width: 9, Value: func(q domain.Queue) string { return strconv.Itoa(q.InFlight) }},
	{Title: "Delayed", Width: 8, Value: func(q domain.Queue) string { return strconv.Itoa(q.Delayed) }},
	{Title: "Dead-letter queue", Width: 28, Value: func(q domain.Queue) string { return q.DeadLetterQueue }},
}

const queuePlaceholder = "name, type:fifo, dlq:orders"

func queueFields(q domain.Queue) []filter.Field {
	return []filter.Field{
		{Name: "name", Value: q.Name},
		{Name: "type", Value: queueType(q)},
		{Name: "dlq", Value: q.DeadLetterQueue},
	}
}

var messageColumns = []listview.Column[domain.Message]{
	{Title: "Body", Value: func(m domain.Message) string { return strings.Join(strings.Fields(m.Body), " ") }},
	{Title: "Sent", Width: 19, Value: func(m domain.Message) string { return m.SentAt.Local().Format(time.DateTime) }},
	{Title: "Receives", Width: 8, Value: func(m domain.Message) string { return strconv.Itoa(m.ReceiveCount) }},
	{Title: "ID", Width: 36, Value: func(m domain.Message) string { return m.ID }},
}

func messageFields(m domain.Message) []filter.Field {
	return []filter.Field{{Name: "body", Value: m.Body}, {Name: "id", Value: m.ID}, {Name: "group", Value: m.GroupID}}
}
