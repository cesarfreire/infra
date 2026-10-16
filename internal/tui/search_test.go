package tui

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

// searchSection is a section with searchable items; loaded() adds more.
type searchSection struct {
	fakeSection
	items []section.SearchItem
}

func (s *searchSection) SearchItems() []section.SearchItem { return s.items }

type itemsArrivedMsg struct{}

func (s *searchSection) Update(msg tea.Msg) (section.Section, tea.Cmd) {
	if _, ok := msg.(itemsArrivedMsg); ok && s.title == "Queues" {
		s.items = append(s.items, section.SearchItem{Bookmark: domain.Bookmark{Kind: domain.KindQueue, ID: "orders", Label: "orders"}})
	}
	_, cmd := s.fakeSection.Update(msg)
	return s, cmd // not the embedded fake: the root keeps what Update returns
}

func newSearchModel(t *testing.T) (Model, *searchSection, *searchSection) {
	t.Helper()
	instances := &searchSection{fakeSection: fakeSection{title: "Instances"}, items: []section.SearchItem{
		{Bookmark: domain.Bookmark{Kind: domain.KindInstance, ID: "i-1", Label: "web-prod"}, Detail: "i-1"},
		{Bookmark: domain.Bookmark{Kind: domain.KindInstance, ID: "i-2", Label: "bastion"}, Detail: "i-2"},
	}}
	queues := &searchSection{fakeSection: fakeSection{title: "Queues"}}
	m := New(Options{Title: "infra"}, theme.Default(), instances, queues, &fakeSection{title: "Sessions"})
	m = drive(m, tea.WindowSizeMsg{Width: 120, Height: 30})
	m = drive(m, run(m.Init())...)
	return m, instances, queues
}

func TestSearchOpensAndInitializesSections(t *testing.T) {
	m, _, queues := newSearchModel(t)
	if queues.inits != 0 {
		t.Fatal("sections are initialized lazily")
	}
	m = drive(m, ctrl('k'))
	if m.dialog != dialogSearch || queues.inits != 1 {
		t.Fatalf("ctrl+k should open the search and load unopened sections; inits = %d", queues.inits)
	}
	screen := ansi.Strip(m.render())
	if !strings.Contains(screen, "Search 2 resources") || !strings.Contains(screen, "web-prod") {
		t.Errorf("screen:\n%s", screen)
	}
	m = drive(m, itemsArrivedMsg{})
	if !strings.Contains(ansi.Strip(m.render()), "Search 3 resources") {
		t.Error("results should grow as sections load")
	}
}

func TestSearchChoosesAndNavigates(t *testing.T) {
	m, _, _ := newSearchModel(t)
	m = drive(m, ctrl('k'))
	for _, r := range "bast" {
		m = drive(m, tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	next, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = next.(Model)
	if m.dialog != dialogNone {
		t.Fatal("choosing closes the search")
	}
	msg := cmd()
	v := reflect.ValueOf(msg)
	if v.Kind() != reflect.Slice || v.Len() != 2 || !strings.Contains(fmt.Sprintf("%T", msg), "sequence") {
		t.Fatalf("expected navigate then reveal in sequence, got %#v", msg)
	}
	nav := v.Index(0).Interface().(tea.Cmd)().(section.NavigateMsg)
	rev := v.Index(1).Interface().(tea.Cmd)().(section.RevealMsg)
	if nav.Title != "Instances" || rev.Bookmark.ID != "i-2" {
		t.Errorf("navigate %q reveal %+v", nav.Title, rev.Bookmark)
	}
}

func TestSearchNotInRawMode(t *testing.T) {
	m, instances, _ := newSearchModel(t)
	m = drive(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	instances.mode = section.ModeRaw
	m = drive(m, ctrl('k'))
	if m.dialog != dialogNone {
		t.Error("ctrl+k belongs to the attached terminal")
	}
}

func TestSearchSkipsDuplicates(t *testing.T) {
	m, instances, _ := newSearchModel(t)
	instances.items = append(instances.items, instances.items[0])
	if got := len(m.searchItems()); got != 2 {
		t.Errorf("items = %d, want duplicates removed", got)
	}
}
