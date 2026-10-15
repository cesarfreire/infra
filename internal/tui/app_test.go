package tui

import (
	"strings"
	"testing"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"infra/internal/tui/section"
	"infra/internal/tui/theme"
)

type fakeSection struct {
	title    string
	inits    int
	focused  bool
	mode     section.InputMode
	received []tea.Msg
	width    int
	listened bool
	cursor   *tea.Cursor
}

func (f *fakeSection) Title() string { return f.title }
func (f *fakeSection) Init() tea.Cmd { f.inits++; return nil }
func (f *fakeSection) Update(msg tea.Msg) (section.Section, tea.Cmd) {
	f.received = append(f.received, msg)
	return f, nil
}
func (f *fakeSection) View() string                 { return "content of " + f.title }
func (f *fakeSection) SetSize(w, _ int)             { f.width = w }
func (f *fakeSection) Focus()                       { f.focused = true }
func (f *fakeSection) Blur()                        { f.focused = false }
func (f *fakeSection) Help() []key.Binding          { return nil }
func (f *fakeSection) InputMode() section.InputMode { return f.mode }
func (f *fakeSection) Listen() tea.Cmd              { f.listened = true; return nil }
func (f *fakeSection) Cursor() *tea.Cursor {
	if f.cursor == nil {
		return nil
	}
	c := *f.cursor
	return &c
}

func press(k string) tea.KeyPressMsg {
	switch k {
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "down":
		return tea.KeyPressMsg{Code: tea.KeyDown}
	case "up":
		return tea.KeyPressMsg{Code: tea.KeyUp}
	}
	return tea.KeyPressMsg{Code: rune(k[0]), Text: k}
}

func newTestModel() (Model, []*fakeSection) {
	a, b := &fakeSection{title: "A"}, &fakeSection{title: "B"}
	m := New(Options{Title: "infra"}, theme.Default(), a, b)
	m.Init()
	next, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	return next.(Model), []*fakeSection{a, b}
}

func send(t *testing.T, m Model, msgs ...tea.Msg) (Model, tea.Cmd) {
	t.Helper()
	var cmd tea.Cmd
	var next tea.Model = m
	for _, msg := range msgs {
		next, cmd = next.(Model).Update(msg)
	}
	return next.(Model), cmd
}

func isQuit(cmd tea.Cmd) bool {
	if cmd == nil {
		return false
	}
	_, ok := cmd().(tea.QuitMsg)
	return ok
}

func TestMenuNavigationInitsSectionsLazily(t *testing.T) {
	m, s := newTestModel()
	if s[0].inits != 1 || s[1].inits != 0 {
		t.Fatalf("inits = %d,%d; want 1,0", s[0].inits, s[1].inits)
	}
	m, _ = send(t, m, press("down"), press("up"), press("down"))
	if m.active != 1 || s[1].inits != 1 {
		t.Errorf("active = %d, B inits = %d; want 1, 1", m.active, s[1].inits)
	}
}

func TestFocusContentAndBack(t *testing.T) {
	m, s := newTestModel()
	m, _ = send(t, m, press("enter"))
	if m.focus != focusContent || !s[0].focused {
		t.Fatal("enter should focus the content panel")
	}
	m, _ = send(t, m, press("x"))
	if len(s[0].received) == 0 {
		t.Error("keys should be forwarded to the focused section")
	}
	m, _ = send(t, m, press("esc"))
	if m.focus != focusMenu || s[0].focused {
		t.Error("esc should return focus to the menu")
	}
}

func TestCapturingSectionReceivesEscAndQ(t *testing.T) {
	m, s := newTestModel()
	s[0].mode = section.ModeCapture
	m, _ = send(t, m, press("enter"))
	m, cmd := send(t, m, press("esc"), press("q"))
	if m.focus != focusContent || isQuit(cmd) || len(s[0].received) != 2 {
		t.Errorf("capturing section should receive esc and q; focus=%v received=%d", m.focus, len(s[0].received))
	}
}

func TestQuit(t *testing.T) {
	m, _ := newTestModel()
	if _, cmd := send(t, m, press("q")); !isQuit(cmd) {
		t.Error("q in menu should quit")
	}
	if _, cmd := send(t, m, tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}); !isQuit(cmd) {
		t.Error("ctrl+c should quit")
	}
}

func TestMouseClickSelectsMenuItem(t *testing.T) {
	m, s := newTestModel()
	// header (1) + top border (1) + menu title and blank line (2) → B is row 5.
	m, _ = send(t, m, tea.MouseClickMsg{X: 3, Y: 5, Button: tea.MouseLeft})
	if m.active != 1 || s[1].inits != 1 {
		t.Errorf("click should select B; active = %d", m.active)
	}
	m, _ = send(t, m, tea.MouseClickMsg{X: 50, Y: 10, Button: tea.MouseLeft})
	if m.focus != focusContent {
		t.Error("click on the content panel should focus it")
	}
}

func TestNonInputMessagesAreBroadcast(t *testing.T) {
	type asyncResult struct{}
	m, s := newTestModel()
	send(t, m, asyncResult{})
	for _, f := range s {
		if len(f.received) != 1 {
			t.Errorf("section %s received %d messages, want 1", f.title, len(f.received))
		}
	}
}

func TestStatusMsgIsShown(t *testing.T) {
	m, _ := newTestModel()
	m, _ = send(t, m, section.StatusMsg{Text: "hello"})
	if !strings.Contains(m.render(), "hello") {
		t.Error("status text should be rendered")
	}
}

func TestRenderFitsWindow(t *testing.T) {
	m, _ := newTestModel()
	out := m.render()
	if got := lipgloss.Height(out); got > 30 {
		t.Errorf("rendered height = %d, want <= 30", got)
	}
	if got := lipgloss.Width(out); got > 100 {
		t.Errorf("rendered width = %d, want <= 100", got)
	}
	if !strings.Contains(out, "content of A") {
		t.Error("active section content should be rendered")
	}
}

func TestRawModeForwardsCtrlC(t *testing.T) {
	m, s := newTestModel()
	m, _ = send(t, m, press("enter"))
	s[0].mode = section.ModeRaw
	_, cmd := send(t, m, tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	if isQuit(cmd) || len(s[0].received) != 1 {
		t.Error("ctrl+c should go to a section in raw mode")
	}
}

func TestListenersStartOnInit(t *testing.T) {
	_, s := newTestModel()
	if !s[0].listened || !s[1].listened {
		t.Error("Listen should be called for every section at startup")
	}
}

func TestNavigateMsgFocusesSection(t *testing.T) {
	m, s := newTestModel()
	m, _ = send(t, m, section.NavigateMsg{Title: "B"})
	if m.active != 1 || m.focus != focusContent || !s[1].focused {
		t.Errorf("active = %d, focus = %v; want B focused", m.active, m.focus)
	}
}

func TestPasteGoesToFocusedSection(t *testing.T) {
	m, s := newTestModel()
	send(t, m, press("enter"), tea.PasteMsg{Content: "x"})
	if _, ok := s[0].received[len(s[0].received)-1].(tea.PasteMsg); !ok {
		t.Error("paste should reach the focused section")
	}
}

func TestCursorIsTranslatedToScreen(t *testing.T) {
	m, s := newTestModel()
	s[0].cursor = tea.NewCursor(0, 0)
	if m.View().Cursor != nil {
		t.Error("cursor should be hidden while the menu is focused")
	}
	m, _ = send(t, m, press("enter"))
	c := m.View().Cursor
	// menu width + left border and padding; header + top border.
	if c == nil || c.X != menuWidth+2 || c.Y != headerHeight+1 {
		t.Errorf("cursor = %+v, want (%d,%d)", c, menuWidth+2, headerHeight+1)
	}
}
