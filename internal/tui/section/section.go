// Package section defines the contract between the root TUI and the panels
// shown on the right side for each menu entry.
package section

import (
	"errors"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"infra/internal/domain"
)

// InputMode tells the root which keys it may intercept while a section is focused.
type InputMode int

const (
	// ModeNormal: the root handles esc (back to menu), q (quit) and ctrl+c.
	ModeNormal InputMode = iota
	// ModeCapture: the section is in a nested state (form, filter, dialog)
	// and handles esc and printable keys itself. ctrl+c still quits.
	ModeCapture
	// ModeRaw: every key, including ctrl+c, goes to the section (embedded
	// terminal). The section must offer its own key to leave this mode.
	ModeRaw
)

// Section is a panel opened from the left menu. New menu entries are added by
// implementing this interface and registering it in cmd/infra; the root model
// never needs to change.
type Section interface {
	// Title is the label shown in the menu and the target of NavigateMsg.
	Title() string
	// Init runs the first time the section is shown.
	Init() tea.Cmd
	// Update receives key, paste and mouse messages while the section is
	// focused (mouse coordinates are relative to the panel content) and
	// every non-input message at all times, so async results are never lost.
	Update(msg tea.Msg) (Section, tea.Cmd)
	View() string
	// SetSize sets the size available for View, excluding the panel frame.
	SetSize(width, height int)
	Focus()
	Blur()
	// Help returns the bindings shown in the footer for the current state.
	Help() []key.Binding
	InputMode() InputMode
}

// Listener is implemented by sections that must react to background events
// from program start, before they are ever shown (e.g. session output).
type Listener interface {
	Listen() tea.Cmd
}

// Searchable is implemented by sections whose resources the global search
// (ctrl+k) can find. It returns what the section has loaded so far.
type Searchable interface {
	SearchItems() []SearchItem
}

// SearchItem is a resource offered by the global search. Choosing it
// navigates to the section and reveals the bookmark.
type SearchItem struct {
	Bookmark domain.Bookmark
	Detail   string // shown dimmed and matched by the search, e.g. an ID
}

// ProgressReporter is implemented by sections that show the progress of a
// long operation in the terminal's own progress indicator (tab, taskbar).
// It is used for the active section; nil means no progress.
type ProgressReporter interface {
	Progress() *tea.ProgressBar
}

// NotifyMsg asks the root to tell the user something finished, even if they
// are looking at another window: a desktop notification and the bell.
type NotifyMsg struct {
	Title string
	Body  string
}

// Notify returns a command that emits NotifyMsg.
func Notify(title, body string) tea.Cmd {
	return func() tea.Msg { return NotifyMsg{Title: title, Body: body} }
}

// CursorProvider is implemented by sections that show a real terminal cursor.
// The position is relative to the section content; nil hides the cursor.
type CursorProvider interface {
	Cursor() *tea.Cursor
}

// StatusMsg replaces the message in the root status bar.
type StatusMsg struct {
	Text  string
	Error bool
}

// Status returns a command that shows text in the status bar.
func Status(text string) tea.Cmd {
	return func() tea.Msg { return StatusMsg{Text: text} }
}

// AlertKind classifies problems that deserve a dialog instead of a status line.
type AlertKind int

const (
	AlertAccessDenied AlertKind = iota
	AlertUnauthenticated
)

// AlertMsg asks the root to show a dialog the user must dismiss.
type AlertMsg struct {
	Kind AlertKind
	Err  error
}

// Error returns a command that reports err: permission and credential
// problems open an alert dialog, anything else goes to the status bar.
func Error(err error) tea.Cmd {
	return func() tea.Msg { return ErrorMsg(err) }
}

// ErrorMsg is the message Error emits for err.
func ErrorMsg(err error) tea.Msg {
	switch {
	case errors.Is(err, domain.ErrUnauthenticated):
		return AlertMsg{Kind: AlertUnauthenticated, Err: err}
	case errors.Is(err, domain.ErrAccessDenied):
		return AlertMsg{Kind: AlertAccessDenied, Err: err}
	}
	return StatusMsg{Text: err.Error(), Error: true}
}

// NavigateMsg asks the root to show and focus the section with Title.
type NavigateMsg struct {
	Title string
}

// Navigate returns a command that emits NavigateMsg.
func Navigate(title string) tea.Cmd {
	return func() tea.Msg { return NavigateMsg{Title: title} }
}

// OpenSessionMsg asks the sessions section to attach to session ID.
type OpenSessionMsg struct {
	ID int
}
