package section

import (
	"fmt"
	"slices"
	"sync"

	tea "charm.land/bubbletea/v2"

	"infra/internal/domain"
	"infra/internal/tui/theme"
)

// Bookmarks are the favorites and recents of the current profile and region.
// They are satisfied by *prefs.Scoped.
type Bookmarks interface {
	IsFavorite(b domain.Bookmark) bool
	ToggleFavorite(b domain.Bookmark) (bool, error)
	Visit(b domain.Bookmark) error
	Forget(b domain.Bookmark) error
	Favorites() []domain.Bookmark
	Recents() []domain.Bookmark
}

// Prefs remembers small values between runs: Get/Set are per profile and
// region, GetGlobal/SetGlobal are shared. Satisfied by *prefs.Scoped.
type Prefs interface {
	Get(key string) string
	Set(key, value string) error
	GetGlobal(key string) string
	SetGlobal(key, value string) error
}

// Deps is what every section receives besides its use cases.
type Deps struct {
	Theme     theme.Theme
	Bookmarks Bookmarks
	Prefs     Prefs
	// ReadOnly hides nothing but blocks changes early, with a message; the
	// use cases enforce it again.
	ReadOnly bool
}

// ToggleFavorite adds or removes b and reports it in the status bar.
func (d Deps) ToggleFavorite(b domain.Bookmark) tea.Cmd {
	fav, err := d.Bookmarks.ToggleFavorite(b)
	if err != nil {
		return Error(fmt.Errorf("saving favorites: %w", err))
	}
	if fav {
		return Status("★ Added " + b.Label + " to favorites")
	}
	return Status("Removed " + b.Label + " from favorites")
}

// Visit records b as recently used. Failing to save a recent is not worth
// interrupting the user, so errors are dropped.
func (d Deps) Visit(b domain.Bookmark) { _ = d.Bookmarks.Visit(b) }

// Star returns the marker shown before favorites in lists.
func (d Deps) Star(b domain.Bookmark) string {
	if d.Bookmarks.IsFavorite(b) {
		return "★ "
	}
	return ""
}

// BlockWrite returns a command reporting that action is not allowed, or
// nil when changes are allowed.
func (d Deps) BlockWrite(action string) tea.Cmd {
	if !d.ReadOnly {
		return nil
	}
	return Error(fmt.Errorf("%s: %w", action, domain.ErrReadOnly))
}

// ConfirmMsg asks the root to confirm an action; Yes runs only if the user
// agrees.
type ConfirmMsg struct {
	Title string
	Body  string
	Yes   tea.Cmd
}

// Confirm returns a command that asks for confirmation before running yes.
func Confirm(title, body string, yes tea.Cmd) tea.Cmd {
	return func() tea.Msg { return ConfirmMsg{Title: title, Body: body, Yes: yes} }
}

// RevealMsg is broadcast to show a bookmarked resource; the section that
// owns its kind selects it, loading its list first if needed.
type RevealMsg struct {
	Bookmark domain.Bookmark
}

// MemoryDeps returns Deps backed by memory, for tests and as a fallback
// when the preferences file cannot be read.
func MemoryDeps(th theme.Theme) Deps {
	m := &memory{values: map[string]string{}}
	return Deps{Theme: th, Bookmarks: m, Prefs: m}
}

type memory struct {
	mu        sync.Mutex
	favorites []domain.Bookmark
	recents   []domain.Bookmark
	values    map[string]string
}

func (m *memory) IsFavorite(b domain.Bookmark) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return slices.ContainsFunc(m.favorites, b.Same)
}

func (m *memory) ToggleFavorite(b domain.Bookmark) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if i := slices.IndexFunc(m.favorites, b.Same); i >= 0 {
		m.favorites = slices.Delete(m.favorites, i, i+1)
		return false, nil
	}
	m.favorites = append(m.favorites, b)
	return true, nil
}

func (m *memory) Visit(b domain.Bookmark) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.recents = append([]domain.Bookmark{b}, slices.DeleteFunc(m.recents, b.Same)...)
	return nil
}

func (m *memory) Forget(b domain.Bookmark) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.recents = slices.DeleteFunc(m.recents, b.Same)
	return nil
}

func (m *memory) Favorites() []domain.Bookmark {
	m.mu.Lock()
	defer m.mu.Unlock()
	return slices.Clone(m.favorites)
}

func (m *memory) Recents() []domain.Bookmark {
	m.mu.Lock()
	defer m.mu.Unlock()
	return slices.Clone(m.recents)
}

func (m *memory) Get(key string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.values["s:"+key]
}

func (m *memory) Set(key, value string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.values["s:"+key] = value
	return nil
}

func (m *memory) GetGlobal(key string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.values["g:"+key]
}

func (m *memory) SetGlobal(key, value string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.values["g:"+key] = value
	return nil
}
