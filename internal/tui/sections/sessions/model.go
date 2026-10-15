// Package sessions is the menu section that lists running sessions (SSM
// shells and port forwards) and shows them as embedded terminals.
package sessions

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/vt"

	"infra/internal/session"
	"infra/internal/tui/section"
	"infra/internal/tui/theme"
)

// Title is the menu label of this section.
const Title = "Sessions"

const (
	listTop = 2 // rows above the first list item (title + blank line)
	termTop = 1 // rows above the terminal screen (title line)
)

// Manager is the subset of *session.Manager this section uses.
type Manager interface {
	List() []*session.Session
	Get(id int) *session.Session
	Remove(id int) error
	SetSize(cols, rows int)
	Updates() <-chan struct{}
}

// updateMsg means some session produced output or exited.
type updateMsg struct{}

type keyMap struct {
	Up     key.Binding
	Down   key.Binding
	Attach key.Binding
	Close  key.Binding
	Detach key.Binding
}

// Model implements section.Section, section.Listener and section.CursorProvider.
type Model struct {
	mgr   Manager
	theme theme.Theme
	keys  keyMap

	cursor   int
	attached int // session ID; 0 when showing the list
	running  map[int]bool
	focused  bool
	width    int
	height   int
}

// New creates the Sessions section.
func New(mgr Manager, th theme.Theme) *Model {
	return &Model{
		mgr:     mgr,
		theme:   th,
		running: map[int]bool{},
		keys: keyMap{
			Up:     key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "up")),
			Down:   key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "down")),
			Attach: key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "open")),
			Close:  key.NewBinding(key.WithKeys("x", "delete"), key.WithHelp("x", "close")),
			Detach: key.NewBinding(key.WithKeys("ctrl+]"), key.WithHelp("ctrl+]", "detach")),
		},
	}
}

func (m *Model) Title() string   { return Title }
func (m *Model) Init() tea.Cmd   { return nil }
func (m *Model) Focus()          { m.focused = true }
func (m *Model) Blur()           { m.focused = false }
func (m *Model) Listen() tea.Cmd { return listen(m.mgr.Updates()) }

func listen(ch <-chan struct{}) tea.Cmd {
	return func() tea.Msg {
		<-ch
		return updateMsg{}
	}
}

func (m *Model) SetSize(width, height int) {
	m.width, m.height = width, height
	m.mgr.SetSize(width, max(1, height-termTop))
}

func (m *Model) InputMode() section.InputMode {
	s := m.current()
	switch {
	case s == nil:
		return section.ModeNormal
	case s.Running():
		return section.ModeRaw
	default:
		return section.ModeCapture
	}
}

// current returns the attached session, or nil when showing the list.
func (m *Model) current() *session.Session {
	if m.attached == 0 {
		return nil
	}
	return m.mgr.Get(m.attached)
}

func (m *Model) Update(msg tea.Msg) (section.Section, tea.Cmd) {
	switch msg := msg.(type) {
	case updateMsg:
		return m, tea.Batch(m.Listen(), m.reportExits())
	case section.OpenSessionMsg:
		m.attach(msg.ID)
		return m, section.Navigate(Title)
	case tea.KeyPressMsg:
		return m, m.handleKey(msg)
	case tea.PasteMsg:
		if s := m.current(); s != nil {
			s.Paste(msg.Content)
		}
	case tea.MouseClickMsg:
		if m.attached == 0 {
			if idx := msg.Y - listTop; idx >= 0 && idx < len(m.mgr.List()) {
				m.cursor = idx
				m.attach(m.mgr.List()[idx].ID)
			}
		}
	}
	return m, nil
}

// reportExits shows a status message for sessions that ended since the
// last update.
func (m *Model) reportExits() tea.Cmd {
	var cmds []tea.Cmd
	for _, s := range m.mgr.List() {
		was, seen := m.running[s.ID]
		now := s.Running()
		m.running[s.ID] = now
		if !seen || !was || now {
			continue
		}
		if err := s.Err(); err != nil {
			cmds = append(cmds, section.Error(fmt.Errorf("%s ended: %w", s.Title, err)))
		} else {
			cmds = append(cmds, section.Status(s.Title+" ended"))
		}
	}
	return tea.Batch(cmds...)
}

func (m *Model) attach(id int) {
	if s := m.mgr.Get(id); s != nil {
		m.attached = id
		m.running[id] = s.Running()
	}
}

func (m *Model) handleKey(msg tea.KeyPressMsg) tea.Cmd {
	if s := m.current(); s != nil {
		if key.Matches(msg, m.keys.Detach) || (!s.Running() && key.Matches(msg, m.keys.Attach, key.NewBinding(key.WithKeys("esc")))) {
			m.attached = 0
			return nil
		}
		s.SendKey(vt.KeyPressEvent(msg))
		return nil
	}

	list := m.mgr.List()
	m.cursor = min(m.cursor, max(0, len(list)-1))
	switch {
	case key.Matches(msg, m.keys.Up):
		m.cursor = max(0, m.cursor-1)
	case key.Matches(msg, m.keys.Down):
		m.cursor = min(len(list)-1, m.cursor+1)
	case len(list) == 0:
	case key.Matches(msg, m.keys.Attach):
		m.attach(list[m.cursor].ID)
	case key.Matches(msg, m.keys.Close):
		s := list[m.cursor]
		if err := m.mgr.Remove(s.ID); err != nil {
			return section.Error(err)
		}
		delete(m.running, s.ID)
		return section.Status("Closed " + s.Title)
	}
	return nil
}

func (m *Model) Help() []key.Binding {
	if s := m.current(); s != nil {
		if s.Running() {
			return []key.Binding{m.keys.Detach}
		}
		return []key.Binding{key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back"))}
	}
	return []key.Binding{m.keys.Up, m.keys.Down, m.keys.Attach, m.keys.Close}
}

func (m *Model) Cursor() *tea.Cursor {
	s := m.current()
	if s == nil || !m.focused || !s.Running() {
		return nil
	}
	x, y, visible := s.Cursor()
	if !visible {
		return nil
	}
	return tea.NewCursor(x, y+termTop)
}

func (m *Model) View() string {
	if s := m.current(); s != nil {
		return m.viewTerminal(s)
	}
	return m.viewList()
}

func (m *Model) viewTerminal(s *session.Session) string {
	status := m.theme.StatusInfo.Render("running")
	hint := "ctrl+] to detach"
	if !s.Running() {
		status = m.theme.Muted.Render("ended")
		if err := s.Err(); err != nil {
			status = m.theme.StatusError.Render(err.Error())
		}
		hint = "esc to go back"
	}
	header := m.theme.Title.Render(s.Title) + " " + status + " " + m.theme.Muted.Render(hint)
	return header + "\n" + s.Render()
}

func (m *Model) viewList() string {
	list := m.mgr.List()
	var b strings.Builder
	b.WriteString(m.theme.Title.Render("Sessions") + " " + m.theme.Muted.Render(fmt.Sprintf("%d open", len(list))) + "\n\n")
	if len(list) == 0 {
		b.WriteString(m.theme.Muted.Render("No sessions. Connect (c) or port forward (p) from Instances."))
		return b.String()
	}
	for i, s := range list {
		state := m.theme.StatusInfo.Render("● running")
		if !s.Running() {
			state = m.theme.Muted.Render("○ ended  ")
		}
		line := fmt.Sprintf("%s  %-13s %s  %s", state, s.Kind, s.StartedAt.Format("15:04:05"), s.Title)
		if i == m.cursor {
			line = m.theme.Selected.Render("› ") + line
		} else {
			line = "  " + line
		}
		b.WriteString(line + "\n")
	}
	return b.String()
}
