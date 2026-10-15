package favorites

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"infra/internal/domain"
	"infra/internal/tui/section"
	"infra/internal/tui/theme"
)

var (
	web    = domain.Bookmark{Kind: domain.KindInstance, ID: "i-1", Label: "web"}
	orders = domain.Bookmark{Kind: domain.KindQueue, ID: "orders", Label: "orders"}
)

func newModel(t *testing.T) (*Model, section.Deps) {
	t.Helper()
	deps := section.MemoryDeps(theme.Default())
	m := New(deps, map[domain.ResourceKind]string{domain.KindInstance: "Instances", domain.KindQueue: "Queues"})
	m.SetSize(100, 20)
	m.Focus()
	return m, deps
}

func TestEmptyStateExplains(t *testing.T) {
	m, _ := newModel(t)
	if !strings.Contains(ansi.Strip(m.View()), "Press * on") {
		t.Errorf("view:\n%s", m.View())
	}
}

func TestListsFavoritesThenRecents(t *testing.T) {
	m, deps := newModel(t)
	deps.Bookmarks.ToggleFavorite(web)
	deps.Bookmarks.Visit(orders)
	deps.Bookmarks.Visit(web) // a favorite also visited: listed once
	view := m.View()
	if strings.Count(view, "web") != 1 || strings.Index(view, "★ web") > strings.Index(view, "orders") {
		t.Errorf("view:\n%s", view)
	}
}

func TestOpenNavigatesThenReveals(t *testing.T) {
	m, deps := newModel(t)
	deps.Bookmarks.ToggleFavorite(web)
	m.Focus()
	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	seq := sequence(t, cmd())
	if nav, ok := seq[0]().(section.NavigateMsg); !ok || nav.Title != "Instances" {
		t.Errorf("first = %#v", seq[0]())
	}
	if rev, ok := seq[1]().(section.RevealMsg); !ok || rev.Bookmark.ID != "i-1" {
		t.Errorf("second = %#v", seq[1]())
	}
}

// sequence unpacks a tea.Sequence message (its type is unexported); the
// order matters: navigate first, then reveal.
func sequence(t *testing.T, msg tea.Msg) []tea.Cmd {
	t.Helper()
	v := reflect.ValueOf(msg)
	if v.Kind() != reflect.Slice || !strings.Contains(fmt.Sprintf("%T", msg), "sequence") || v.Len() != 2 {
		t.Fatalf("expected a sequence of two commands, got %#v", msg)
	}
	return []tea.Cmd{v.Index(0).Interface().(tea.Cmd), v.Index(1).Interface().(tea.Cmd)}
}

func TestRemoveAndPromote(t *testing.T) {
	m, deps := newModel(t)
	deps.Bookmarks.Visit(orders)
	m.Focus()
	m.Update(tea.KeyPressMsg{Code: '*', Text: "*"})
	if !deps.Bookmarks.IsFavorite(orders) {
		t.Fatal("* should make the recent a favorite")
	}
	m.Update(tea.KeyPressMsg{Code: 'x', Text: "x"})
	if deps.Bookmarks.IsFavorite(orders) {
		t.Fatal("x should remove the favorite")
	}
	m.Update(tea.KeyPressMsg{Code: 'x', Text: "x"})
	if len(deps.Bookmarks.Recents()) != 0 {
		t.Error("x on a recent should forget it")
	}
}
