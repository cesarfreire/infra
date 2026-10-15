package session

import (
	"os"
	"os/exec"
	"slices"
	"sync"

	"github.com/creack/pty"

	"infra/internal/app"
)

// startFunc starts cmd attached to a new pseudo-terminal.
type startFunc func(cmd *exec.Cmd, cols, rows int) (*os.File, error)

// Manager owns every running session. It is safe for concurrent use.
type Manager struct {
	mu       sync.Mutex
	sessions []*Session
	nextID   int
	cols     int
	rows     int
	updates  chan struct{}
	start    startFunc
}

// NewManager returns an empty manager.
func NewManager() *Manager {
	return &Manager{
		nextID:  1,
		cols:    80,
		rows:    24,
		updates: make(chan struct{}, 1),
		start:   startPTY,
	}
}

func startPTY(cmd *exec.Cmd, cols, rows int) (*os.File, error) {
	return pty.StartWithSize(cmd, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)})
}

func setSize(f *os.File, cols, rows int) error {
	return pty.Setsize(f, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)})
}

// Updates receives a value whenever any session produces output or exits.
// Notifications are coalesced: one pending value stands for many changes.
func (m *Manager) Updates() <-chan struct{} { return m.updates }

func (m *Manager) notify() {
	select {
	case m.updates <- struct{}{}:
	default:
	}
}

// SetSize sets the screen size of new and existing sessions, which are all
// shown in the same panel.
func (m *Manager) SetSize(cols, rows int) {
	m.mu.Lock()
	m.cols, m.rows = max(1, cols), max(1, rows)
	sessions := slices.Clone(m.sessions)
	m.mu.Unlock()
	for _, s := range sessions {
		s.Resize(cols, rows)
	}
}

// Start runs c in a new session.
func (m *Manager) Start(kind Kind, title string, c app.Command) (*Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	cmd := exec.Command(c.Name, c.Args...)
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")
	f, err := m.start(cmd, m.cols, m.rows)
	if err != nil {
		return nil, err
	}
	s := newSession(m.nextID, kind, title, cmd, f, m.cols, m.rows, m.notify)
	m.nextID++
	m.sessions = append(m.sessions, s)
	return s, nil
}

// List returns the sessions in creation order.
func (m *Manager) List() []*Session {
	m.mu.Lock()
	defer m.mu.Unlock()
	return slices.Clone(m.sessions)
}

// Get returns the session with id, or nil.
func (m *Manager) Get(id int) *Session {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, s := range m.sessions {
		if s.ID == id {
			return s
		}
	}
	return nil
}

// Remove terminates the session and forgets it.
func (m *Manager) Remove(id int) error {
	m.mu.Lock()
	idx := slices.IndexFunc(m.sessions, func(s *Session) bool { return s.ID == id })
	if idx < 0 {
		m.mu.Unlock()
		return nil
	}
	s := m.sessions[idx]
	m.sessions = slices.Delete(m.sessions, idx, idx+1)
	m.mu.Unlock()
	m.notify()
	return s.Close()
}

// CloseAll terminates every session; call it before the program exits.
func (m *Manager) CloseAll() {
	for _, s := range m.List() {
		_ = s.Close()
	}
}
