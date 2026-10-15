// Package session runs interactive processes (SSM shells, tunnels) inside a
// pseudo-terminal and keeps their screen in a VT emulator, so the TUI can
// render them in a panel instead of handing over the real terminal.
package session

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"

	"github.com/charmbracelet/x/vt"
)

// Kind tells what a session is used for.
type Kind string

const (
	KindShell       Kind = "shell"
	KindPortForward Kind = "port-forward"
)

// Session is a process attached to a pseudo-terminal.
type Session struct {
	ID        int
	Kind      Kind
	Title     string
	StartedAt time.Time

	cmd  *exec.Cmd
	pty  ptyFile
	term *vt.SafeEmulator
	done chan struct{}
	// drained is closed when all process output has been read.
	drained chan struct{}

	mu        sync.Mutex // guards the fields below and pty Close/Setsize
	err       error
	endedAt   time.Time
	ptyClosed bool
}

// pty is the subset of *os.File used, so tests can fake it.
type ptyFile interface {
	io.ReadWriteCloser
}

func newSession(id int, kind Kind, title string, cmd *exec.Cmd, p ptyFile, cols, rows int, notify func()) *Session {
	s := &Session{
		ID:        id,
		Kind:      kind,
		Title:     title,
		StartedAt: time.Now(),
		cmd:       cmd,
		pty:       p,
		term:      vt.NewSafeEmulator(cols, rows),
		done:      make(chan struct{}),
		drained:   make(chan struct{}),
	}
	go s.pumpOutput(notify)
	go s.pumpInput()
	go s.wait(notify)
	return s
}

// pumpOutput feeds process output into the emulator.
func (s *Session) pumpOutput(notify func()) {
	defer close(s.drained)
	buf := make([]byte, 32*1024)
	for {
		n, err := s.pty.Read(buf)
		if n > 0 {
			_, _ = s.term.Write(buf[:n])
			notify()
		}
		if err != nil {
			return
		}
	}
}

// pumpInput forwards keys and terminal replies from the emulator to the process.
func (s *Session) pumpInput() {
	_, _ = io.Copy(s.pty, s.term)
}

func (s *Session) wait(notify func()) {
	err := s.cmd.Wait()
	s.mu.Lock()
	s.err = err
	s.endedAt = time.Now()
	s.mu.Unlock()
	// Let the last output reach the screen. A leftover child holding the
	// pty open would block the read forever, hence the timeout.
	select {
	case <-s.drained:
	case <-time.After(500 * time.Millisecond):
	}
	s.mu.Lock()
	s.ptyClosed = true
	_ = s.pty.Close()
	s.mu.Unlock()
	// Closing the input pipe ends pumpInput. Emulator.Close is avoided: it
	// races with the blocked Read in vt v0.1.0.
	if pw, ok := s.term.InputPipe().(*io.PipeWriter); ok {
		_ = pw.Close()
	}
	close(s.done)
	notify()
}

// Running reports whether the process is still alive.
func (s *Session) Running() bool {
	select {
	case <-s.done:
		return false
	default:
		return true
	}
}

// Done is closed when the process exits.
func (s *Session) Done() <-chan struct{} { return s.done }

// Err returns the exit error once the process has ended.
func (s *Session) Err() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.err
}

// Render returns the current screen with ANSI styles.
func (s *Session) Render() string { return s.term.Render() }

// Cursor returns the cursor position and whether it is visible.
func (s *Session) Cursor() (x, y int, visible bool) {
	pos := s.term.CursorPosition()
	return pos.X, pos.Y, !s.term.CursorHidden()
}

// SendKey encodes a key press for the process (honoring terminal modes).
func (s *Session) SendKey(k vt.KeyPressEvent) {
	if s.Running() {
		s.term.SendKey(k)
	}
}

// Paste sends text, bracketed if the process enabled bracketed paste.
func (s *Session) Paste(text string) {
	if s.Running() {
		s.term.Paste(text)
	}
}

// Resize changes the screen and pseudo-terminal size.
func (s *Session) Resize(cols, rows int) {
	if cols <= 0 || rows <= 0 || (cols == s.term.Width() && rows == s.term.Height()) {
		return
	}
	s.term.Resize(cols, rows)
	s.mu.Lock()
	defer s.mu.Unlock()
	if f, ok := s.pty.(*os.File); ok && !s.ptyClosed {
		_ = setSize(f, cols, rows)
	}
}

// Close terminates the process and its children (e.g. the AWS CLI and the
// session-manager-plugin it spawns). It is a no-op if it already exited.
func (s *Session) Close() error {
	if !s.Running() || s.cmd.Process == nil {
		return nil
	}
	// The pty makes the process a session leader, so its pid is the group id.
	if err := syscall.Kill(-s.cmd.Process.Pid, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
		return err
	}
	return nil
}
