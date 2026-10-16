// Package prefs persists what the user chose between runs (state.json,
// written by the app) and reads what the user configures (settings.json,
// edited by hand). Both live in the user config dir (~/.config/infra).
package prefs

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"sync"

	"infra/internal/domain"
)

const maxRecents = 20

// Settings are edited by the user in settings.json.
type Settings struct {
	// ReadOnlyProfiles blocks every change (start/stop, deploys, item
	// edits...) for matching profiles. Patterns use path.Match syntax,
	// e.g. "prod", "*-prod".
	ReadOnlyProfiles []string `json:"readOnlyProfiles,omitempty"`
	// UpdateCheck turns the check for new versions at startup on or off
	// (default on).
	UpdateCheck *bool `json:"updateCheck,omitempty"`
	// Notifications turns desktop notifications and the terminal bell on
	// or off (default on), e.g. when a deployment finishes.
	Notifications *bool `json:"notifications,omitempty"`
}

// scope holds the state of one profile and region.
type scope struct {
	Favorites []domain.Bookmark `json:"favorites,omitempty"`
	Recents   []domain.Bookmark `json:"recents,omitempty"`
	Values    map[string]string `json:"values,omitempty"`
}

type state struct {
	Profile string            `json:"profile,omitempty"`
	Region  string            `json:"region,omitempty"`
	Values  map[string]string `json:"values,omitempty"` // global preferences
	Scopes  map[string]*scope `json:"scopes,omitempty"` // by "profile/region"
}

// Store is safe for concurrent use. Every change is saved immediately.
type Store struct {
	dir      string
	mu       sync.Mutex
	state    state
	settings Settings
}

// DefaultDir is ~/.config/infra (or the OS equivalent).
func DefaultDir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "infra"), nil
}

// Open loads the files in dir. Missing files mean defaults.
func Open(dir string) (*Store, error) {
	s := &Store{dir: dir}
	if err := readJSON(filepath.Join(dir, "state.json"), &s.state); err != nil {
		return nil, fmt.Errorf("reading state: %w", err)
	}
	if err := readJSON(filepath.Join(dir, "settings.json"), &s.settings); err != nil {
		return nil, fmt.Errorf("reading settings: %w", err)
	}
	return s, nil
}

func readJSON(p string, v any) error {
	b, err := os.ReadFile(p)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := json.Unmarshal(b, v); err != nil {
		return fmt.Errorf("%s: %w", p, err)
	}
	return nil
}

// save writes state.json atomically. Callers hold s.mu.
func (s *Store) save() error {
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(s.state, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(s.dir, "state-*.json")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return os.Rename(tmp.Name(), filepath.Join(s.dir, "state.json"))
}

// LastContext returns the profile and region used last time.
func (s *Store) LastContext() (profile, region string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state.Profile, s.state.Region
}

// SetLastContext remembers the profile and region in use.
func (s *Store) SetLastContext(profile, region string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state.Profile == profile && s.state.Region == region {
		return nil
	}
	s.state.Profile, s.state.Region = profile, region
	return s.save()
}

// UpdateCheckEnabled reports whether settings.json allows the update check.
func (s *Store) UpdateCheckEnabled() bool {
	return s.settings.UpdateCheck == nil || *s.settings.UpdateCheck
}

// NotificationsEnabled reports whether settings.json allows notifications.
func (s *Store) NotificationsEnabled() bool {
	return s.settings.Notifications == nil || *s.settings.Notifications
}

// GetGlobal returns a value shared by every profile.
func (s *Store) GetGlobal(key string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state.Values[key]
}

// SetGlobal stores a value shared by every profile.
func (s *Store) SetGlobal(key, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state.Values == nil {
		s.state.Values = map[string]string{}
	}
	if s.state.Values[key] == value {
		return nil
	}
	s.state.Values[key] = value
	return s.save()
}

// ReadOnly reports whether settings.json marks profile as read-only.
func (s *Store) ReadOnly(profile string) bool {
	name := profile
	if name == "" {
		name = "default"
	}
	for _, pattern := range s.settings.ReadOnlyProfiles {
		if ok, _ := path.Match(pattern, name); ok {
			return true
		}
	}
	return false
}

// Scope returns the preferences of a profile and region.
func (s *Store) Scope(profile, region string) *Scoped {
	if profile == "" {
		profile = "default"
	}
	return &Scoped{store: s, key: profile + "/" + region}
}

// Scoped is the view of the store for one profile and region. Bookmarks and
// most values are per account and region; Global values are shared.
type Scoped struct {
	store *Store
	key   string
}

func (c *Scoped) scope() *scope {
	st := &c.store.state
	if st.Scopes == nil {
		st.Scopes = map[string]*scope{}
	}
	sc := st.Scopes[c.key]
	if sc == nil {
		sc = &scope{}
		st.Scopes[c.key] = sc
	}
	return sc
}

// Favorites returns the favorites, oldest first.
func (c *Scoped) Favorites() []domain.Bookmark {
	c.store.mu.Lock()
	defer c.store.mu.Unlock()
	return slices.Clone(c.scope().Favorites)
}

// Recents returns the recently visited resources, newest first.
func (c *Scoped) Recents() []domain.Bookmark {
	c.store.mu.Lock()
	defer c.store.mu.Unlock()
	return slices.Clone(c.scope().Recents)
}

// IsFavorite reports whether b is a favorite.
func (c *Scoped) IsFavorite(b domain.Bookmark) bool {
	c.store.mu.Lock()
	defer c.store.mu.Unlock()
	return slices.ContainsFunc(c.scope().Favorites, b.Same)
}

// ToggleFavorite adds or removes b and reports whether it is now a favorite.
func (c *Scoped) ToggleFavorite(b domain.Bookmark) (bool, error) {
	c.store.mu.Lock()
	defer c.store.mu.Unlock()
	sc := c.scope()
	if i := slices.IndexFunc(sc.Favorites, b.Same); i >= 0 {
		sc.Favorites = slices.Delete(sc.Favorites, i, i+1)
		return false, c.store.save()
	}
	sc.Favorites = append(sc.Favorites, b)
	return true, c.store.save()
}

// Visit records b as the most recent resource.
func (c *Scoped) Visit(b domain.Bookmark) error {
	c.store.mu.Lock()
	defer c.store.mu.Unlock()
	sc := c.scope()
	sc.Recents = slices.DeleteFunc(sc.Recents, b.Same)
	sc.Recents = append([]domain.Bookmark{b}, sc.Recents...)
	if len(sc.Recents) > maxRecents {
		sc.Recents = sc.Recents[:maxRecents]
	}
	return c.store.save()
}

// Forget removes b from the recents.
func (c *Scoped) Forget(b domain.Bookmark) error {
	c.store.mu.Lock()
	defer c.store.mu.Unlock()
	sc := c.scope()
	sc.Recents = slices.DeleteFunc(sc.Recents, b.Same)
	return c.store.save()
}

// Get returns a value of this profile and region ("" when unset).
func (c *Scoped) Get(key string) string {
	c.store.mu.Lock()
	defer c.store.mu.Unlock()
	return c.scope().Values[key]
}

// Set stores a value of this profile and region.
func (c *Scoped) Set(key, value string) error {
	c.store.mu.Lock()
	defer c.store.mu.Unlock()
	sc := c.scope()
	if sc.Values == nil {
		sc.Values = map[string]string{}
	}
	if sc.Values[key] == value {
		return nil
	}
	sc.Values[key] = value
	return c.store.save()
}

// GetGlobal returns a value shared by every profile.
func (c *Scoped) GetGlobal(key string) string { return c.store.GetGlobal(key) }

// SetGlobal stores a value shared by every profile.
func (c *Scoped) SetGlobal(key, value string) error { return c.store.SetGlobal(key, value) }
