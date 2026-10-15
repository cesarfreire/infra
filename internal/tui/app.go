// Package tui is the terminal user interface: a menu on the left and the
// selected section on the right.
package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"infra/internal/domain"
	"infra/internal/tui/components/picker"
	"infra/internal/tui/section"
	"infra/internal/tui/theme"
)

const updateTimeout = 10 * time.Second

const (
	menuWidth    = 24
	headerHeight = 1
	footerHeight = 2 // status line + help line
	// menuItemsTop is the row of the first menu item inside the menu panel
	// content (after the title and a blank line).
	menuItemsTop = 2
)

type focusArea int

const (
	focusMenu focusArea = iota
	focusContent
)

// Options configures the root model.
type Options struct {
	Title string
	// Workspace enables the profile and region selectors; the sections then
	// come from Workspace.Open instead of New's arguments.
	Workspace Workspace
	Profile   string // initial profile
	Region    string // initial region
	// Updates, when set, is checked once in the background at startup.
	Updates UpdateChecker
	// OpenURL opens a link in the browser (the release page).
	OpenURL func(url string) error
}

// UpdateChecker reports whether a newer version of the app was released.
// It is satisfied by *app.UpdateChecker.
type UpdateChecker interface {
	Check(ctx context.Context) (domain.Release, bool, error)
}

// updateMsg carries a newer release found at startup.
type updateMsg struct {
	release domain.Release
}

// Model is the root Bubble Tea model.
type Model struct {
	opts        Options
	sections    []section.Section
	initialized []bool
	active      int
	focus       focusArea
	width       int
	height      int
	status      section.StatusMsg
	keys        keyMap
	help        help.Model
	theme       theme.Theme

	// Cloud context.
	workspace   Workspace
	profile     string
	region      string
	profiles    []domain.Profile // as of the last profile picker
	seq         int              // incremented on every switch
	switching   bool
	verifying   bool
	identity    domain.Identity
	identityErr error
	readOnly    bool
	update      *domain.Release // newer release, when there is one

	// Dialog drawn over the screen.
	dialog   dialogKind
	picker   *picker.Model
	alert    section.AlertMsg
	alertSSO bool // the alert offers `aws sso login`
	confirm  section.ConfirmMsg
}

// New builds the root model with sections listed in menu order. With
// opts.Workspace set, sections are opened from it instead.
func New(opts Options, th theme.Theme, sections ...section.Section) Model {
	h := help.New()
	h.Styles.ShortKey = th.Selected
	h.Styles.ShortDesc = th.Muted
	h.Styles.ShortSeparator = th.Muted
	m := Model{
		opts:        opts,
		sections:    sections,
		initialized: make([]bool, len(sections)),
		keys:        defaultKeyMap(),
		help:        h,
		theme:       th,
		workspace:   opts.Workspace,
		profile:     opts.Profile,
		region:      opts.Region,
	}
	if m.workspace != nil {
		m.seq, m.switching = 1, true
	}
	return m
}

func (m Model) Init() tea.Cmd {
	if m.workspace != nil {
		return tea.Batch(m.openCmd(m.seq, m.profile, m.region), m.checkUpdates())
	}
	if len(m.sections) == 0 {
		return nil
	}
	cmds := []tea.Cmd{m.sections[0].Init()}
	m.initialized[0] = true
	for _, s := range m.sections {
		if l, ok := s.(section.Listener); ok {
			cmds = append(cmds, l.Listen())
		}
	}
	return tea.Batch(cmds...)
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.resize()
		return m, nil
	case tea.KeyPressMsg:
		return m.handleKey(msg)
	case tea.PasteMsg:
		if m.focus == focusContent && len(m.sections) > 0 {
			return m.updateCurrent(msg)
		}
		return m, nil
	case tea.MouseClickMsg:
		return m.handleClick(msg)
	case section.StatusMsg:
		m.status = msg
		return m, nil
	case section.AlertMsg:
		m.showAlert(msg)
		return m, nil
	case section.ConfirmMsg:
		if m.dialog == dialogNone {
			m.dialog, m.confirm = dialogConfirm, msg
		}
		return m, nil
	case loginDoneMsg:
		return m.handleLoginDone(msg)
	case section.NavigateMsg:
		return m.navigate(msg.Title)
	case updateMsg:
		r := msg.release
		m.update = &r
		return m, nil
	case envOpenedMsg:
		return m.handleEnvOpened(msg)
	case identityMsg:
		return m.handleIdentity(msg)
	}
	if m.picker != nil {
		_, cmd := m.picker.Update(msg) // cursor blink
		mm, cmd2 := m.broadcast(msg)
		return mm, tea.Batch(cmd, cmd2)
	}
	return m.broadcast(msg)
}

func (m Model) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if m.dialog != dialogNone {
		return m.handleDialogKey(msg)
	}
	mode := section.ModeNormal
	if m.focus == focusContent && len(m.sections) > 0 {
		mode = m.current().InputMode()
	}
	// Context selectors work everywhere except inside an attached terminal,
	// where ctrl+p/ctrl+r belong to the remote shell.
	if m.workspace != nil && mode != section.ModeRaw {
		switch {
		case key.Matches(msg, m.keys.Profile):
			return m, m.openProfilePicker()
		case key.Matches(msg, m.keys.Region):
			return m, m.openRegionPicker()
		}
	}
	if len(m.sections) == 0 {
		if key.Matches(msg, m.keys.Quit, m.keys.ForceQuit) {
			return m, tea.Quit
		}
		return m, nil
	}
	if mode != section.ModeRaw && key.Matches(msg, m.keys.ForceQuit) {
		return m, tea.Quit
	}

	if m.focus == focusContent {
		if mode == section.ModeNormal {
			switch {
			case key.Matches(msg, m.keys.Quit):
				return m, tea.Quit
			case key.Matches(msg, m.keys.Back):
				m.focusMenu()
				return m, nil
			}
		}
		return m.updateCurrent(msg)
	}

	switch {
	case key.Matches(msg, m.keys.Quit):
		return m, tea.Quit
	case key.Matches(msg, m.keys.Up):
		return m, m.selectSection(m.active - 1)
	case key.Matches(msg, m.keys.Down):
		return m, m.selectSection(m.active + 1)
	case key.Matches(msg, m.keys.Open):
		m.focusContent()
	}
	return m, nil
}

func (m Model) navigate(title string) (tea.Model, tea.Cmd) {
	for i, s := range m.sections {
		if s.Title() == title {
			cmd := m.selectSection(i) // blurs the previous section
			m.focus = focusContent
			m.current().Focus()
			return m, cmd
		}
	}
	return m, nil
}

func (m Model) handleClick(msg tea.MouseClickMsg) (tea.Model, tea.Cmd) {
	if msg.Button != tea.MouseLeft {
		return m, nil
	}
	if m.dialog != dialogNone {
		return m.handleDialogClick(msg)
	}
	if msg.Y < headerHeight {
		switch m.headerTargetAt(msg.X) {
		case targetProfile:
			return m, m.openProfilePicker()
		case targetRegion:
			return m, m.openRegionPicker()
		case targetUpdate:
			return m, m.openRelease()
		}
		return m, nil
	}
	if len(m.sections) == 0 {
		return m, nil
	}
	frameX, frameY := m.theme.Panel.GetFrameSize()
	// Panels start below the header; content starts after the top-left frame.
	contentY := msg.Y - headerHeight - frameY/2

	if msg.X < menuWidth {
		if idx := contentY - menuItemsTop; idx >= 0 && idx < len(m.sections) {
			cmd := m.selectSection(idx)
			m.focusMenu()
			return m, cmd
		}
		return m, nil
	}

	m.focusContent()
	rel := msg
	rel.X = msg.X - menuWidth - frameX/2
	rel.Y = contentY
	return m.updateCurrent(rel)
}

// broadcast forwards non-input messages (async results, spinner ticks...) to
// every section so background work completes even when not displayed.
func (m Model) broadcast(msg tea.Msg) (tea.Model, tea.Cmd) {
	cmds := make([]tea.Cmd, 0, len(m.sections))
	for i, s := range m.sections {
		updated, cmd := s.Update(msg)
		m.sections[i] = updated
		cmds = append(cmds, cmd)
	}
	return m, tea.Batch(cmds...)
}

func (m Model) updateCurrent(msg tea.Msg) (tea.Model, tea.Cmd) {
	updated, cmd := m.current().Update(msg)
	m.sections[m.active] = updated
	return m, cmd
}

func (m *Model) current() section.Section { return m.sections[m.active] }

func (m *Model) selectSection(idx int) tea.Cmd {
	if idx < 0 || idx >= len(m.sections) || idx == m.active {
		return nil
	}
	if m.focus == focusContent {
		m.current().Blur()
	}
	m.active = idx
	if m.initialized[idx] {
		return nil
	}
	m.initialized[idx] = true
	return m.sections[idx].Init()
}

func (m *Model) focusMenu() {
	if m.focus == focusContent {
		m.current().Blur()
	}
	m.focus = focusMenu
}

func (m *Model) focusContent() {
	if m.focus == focusMenu {
		m.current().Focus()
	}
	m.focus = focusContent
}

func (m *Model) resize() {
	m.help.SetWidth(m.width)
	w, h := m.contentSize()
	for _, s := range m.sections {
		s.SetSize(w, h)
	}
}

func (m Model) bodyHeight() int {
	return max(0, m.height-headerHeight-footerHeight)
}

// contentSize is the space available inside the right panel frame.
func (m Model) contentSize() (int, int) {
	frameX, frameY := m.theme.Panel.GetFrameSize()
	return max(0, m.width-menuWidth-frameX), max(0, m.bodyHeight()-frameY)
}

func (m Model) View() tea.View {
	v := tea.NewView(m.render())
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	v.WindowTitle = m.opts.Title
	v.Cursor = m.cursor()
	return v
}

// checkUpdates asks for a newer release in the background. Failures (no
// network, rate limits) are not worth bothering the user, so they are dropped.
func (m Model) checkUpdates() tea.Cmd {
	checker := m.opts.Updates
	if checker == nil {
		return nil
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), updateTimeout)
		defer cancel()
		r, newer, err := checker.Check(ctx)
		if err != nil || !newer {
			return nil
		}
		return updateMsg{release: r}
	}
}

func (m Model) openRelease() tea.Cmd {
	if m.update == nil || m.opts.OpenURL == nil {
		return nil
	}
	url, open := m.update.URL, m.opts.OpenURL
	return func() tea.Msg {
		if err := open(url); err != nil {
			return section.StatusMsg{Text: fmt.Sprintf("%v (%s)", err, url), Error: true}
		}
		return section.StatusMsg{Text: "Opened " + url}
	}
}

// cursor translates the focused section's cursor to screen coordinates.
func (m Model) cursor() *tea.Cursor {
	if m.dialog != dialogNone || m.focus != focusContent || len(m.sections) == 0 || m.width == 0 {
		return nil
	}
	cp, ok := m.current().(section.CursorProvider)
	if !ok {
		return nil
	}
	c := cp.Cursor()
	if c == nil {
		return nil
	}
	frameX, frameY := m.theme.Panel.GetFrameSize()
	c.X += menuWidth + frameX/2
	c.Y += headerHeight + frameY/2
	return c
}

func (m Model) render() string {
	if m.width == 0 || m.height == 0 {
		return ""
	}
	body := lipgloss.JoinHorizontal(lipgloss.Top, m.renderMenu(), m.renderContent())
	screen := lipgloss.JoinVertical(lipgloss.Left, m.renderHeader(), body, m.renderStatus(), m.renderHelp())
	return m.overlayDialog(screen)
}

func (m Model) renderMenu() string {
	style := m.theme.Panel
	if m.focus == focusMenu {
		style = m.theme.PanelFocused
	}
	itemWidth := menuWidth - style.GetHorizontalFrameSize()

	lines := []string{m.theme.MenuTitle.Render("RESOURCES"), ""}
	for i, s := range m.sections {
		item := m.theme.MenuItem
		if i == m.active {
			item = m.theme.MenuActive
		}
		lines = append(lines, item.Width(itemWidth).MaxWidth(itemWidth).Render(s.Title()))
	}
	return style.Width(menuWidth).Height(m.bodyHeight()).Render(strings.Join(lines, "\n"))
}

func (m Model) renderContent() string {
	style := m.theme.Panel
	if m.focus == focusContent {
		style = m.theme.PanelFocused
	}
	content := m.theme.Muted.Render("Connecting…")
	if len(m.sections) > 0 {
		content = m.current().View()
	}
	w, h := m.contentSize()
	content = lipgloss.NewStyle().MaxWidth(w).MaxHeight(h).Render(content)
	return style.Width(m.width - menuWidth).Height(m.bodyHeight()).Render(content)
}

func (m Model) renderStatus() string {
	if m.status.Text == "" {
		return ""
	}
	style := m.theme.StatusInfo
	if m.status.Error {
		style = m.theme.StatusError
	}
	return style.MaxWidth(m.width).Render(" " + m.status.Text)
}

func (m Model) renderHelp() string {
	var b bindings
	switch {
	case m.dialog == dialogAlert && m.alertSSO:
		b = bindings{m.keys.Login, m.keys.CloseAlert}
	case m.dialog == dialogAlert:
		b = bindings{m.keys.CloseAlert}
	case m.dialog == dialogConfirm:
		b = bindings{m.keys.Yes, m.keys.No}
	case m.dialog != dialogNone:
		b = bindings(m.picker.Help())
	case m.focus == focusMenu || len(m.sections) == 0:
		b = bindings{m.keys.Up, m.keys.Down, m.keys.Open}
		if m.workspace != nil {
			b = append(b, m.keys.Profile, m.keys.Region)
		}
		b = append(b, m.keys.Quit)
	default:
		b = bindings(m.current().Help())
		if m.current().InputMode() == section.ModeNormal {
			b = append(b, m.keys.Back)
			if m.workspace != nil {
				b = append(b, m.keys.Profile, m.keys.Region)
			}
			b = append(b, m.keys.Quit)
		}
	}
	return " " + m.help.View(b)
}
