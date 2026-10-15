package tui

import (
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"infra/internal/tui/components/picker"
	"infra/internal/tui/section"
)

type dialogKind int

const (
	dialogNone dialogKind = iota
	dialogProfile
	dialogRegion
	dialogAlert
	dialogConfirm
)

const maxDialogWidth = 72

func (m *Model) openPicker(kind dialogKind, title string, items []picker.Item, current string) tea.Cmd {
	m.dialog = kind
	m.picker = picker.New(title, items, current, max(3, min(12, m.height-12)))
	return m.picker.Focus()
}

func (m *Model) showAlert(msg section.AlertMsg) {
	// A selector the user is using wins; the alert would hide it.
	if m.dialog == dialogProfile || m.dialog == dialogRegion {
		m.status = section.StatusMsg{Text: msg.Err.Error(), Error: true}
		return
	}
	m.dialog = dialogAlert
	m.alert = msg
	m.alertSSO = msg.Kind == section.AlertUnauthenticated && m.workspace != nil && m.isSSO(m.profile)
}

func (m *Model) closeDialog() {
	m.dialog = dialogNone
	m.picker = nil
}

func (m Model) handleDialogKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if key.Matches(msg, m.keys.ForceQuit) {
		return m, tea.Quit
	}
	switch m.dialog {
	case dialogAlert:
		switch {
		case m.alertSSO && key.Matches(msg, m.keys.Login):
			m.closeDialog()
			return m, m.login()
		case key.Matches(msg, m.keys.CloseAlert):
			m.closeDialog()
		}
		return m, nil
	case dialogConfirm:
		switch {
		case key.Matches(msg, m.keys.Yes):
			yes := m.confirm.Yes
			m.closeDialog()
			return m, yes
		case key.Matches(msg, m.keys.No):
			m.closeDialog()
		}
		return m, nil
	}
	result, cmd := m.picker.Update(msg)
	switch result {
	case picker.Cancelled:
		m.closeDialog()
	case picker.Chosen:
		item, _ := m.picker.Selected()
		kind := m.dialog
		m.closeDialog()
		return m, m.choose(kind, item.Value)
	}
	return m, cmd
}

func (m Model) handleDialogClick(msg tea.MouseClickMsg) (tea.Model, tea.Cmd) {
	if m.dialog == dialogAlert || m.dialog == dialogConfirm {
		m.closeDialog() // a click never confirms
		return m, nil
	}
	x, y, w, h := m.dialogRect()
	if msg.X < x || msg.X >= x+w || msg.Y < y || msg.Y >= y+h {
		m.closeDialog() // click outside cancels
		return m, nil
	}
	_, frameY := m.theme.Dialog.GetFrameSize()
	if m.picker.Click(msg.Y - y - frameY/2) {
		item, _ := m.picker.Selected()
		kind := m.dialog
		m.closeDialog()
		return m, m.choose(kind, item.Value)
	}
	return m, nil
}

func (m Model) dialogWidth() int { return max(20, min(maxDialogWidth, m.width-4)) }

// dialogRect returns the dialog's position and size on screen (centered).
func (m Model) dialogRect() (x, y, w, h int) {
	box := m.renderDialog()
	w, h = lipgloss.Width(box), lipgloss.Height(box)
	return max(0, (m.width-w)/2), max(0, (m.height-h)/2), w, h
}

func (m Model) renderDialog() string {
	style := m.theme.Dialog
	w := m.dialogWidth()
	inner := w - style.GetHorizontalFrameSize()
	var content string
	switch m.dialog {
	case dialogAlert:
		content = m.alertView(inner)
		style = m.theme.DialogAlert
	case dialogConfirm:
		content = m.confirmView(inner)
		style = m.theme.DialogAlert
	default:
		content = m.picker.View(m.theme, inner)
	}
	return style.Width(w).Render(content)
}

func (m Model) alertView(width int) string {
	th := m.theme
	title, hint := "Permission denied",
		"The profile "+profileLabel(m.profile)+" is not allowed to perform this operation. "+
			"Check its IAM policies, or switch profile with ctrl+p."
	if m.alert.Kind == section.AlertUnauthenticated {
		title = "Credentials problem"
		hint = "The credentials of profile " + profileLabel(m.profile) + " are missing, invalid or expired. " +
			"For SSO profiles, run: aws sso login --profile " + profileLabel(m.profile)
		if m.alertSSO {
			hint = "The SSO session of profile " + profileLabel(m.profile) + " expired or was never started. " +
				"Press l to run aws sso login here; credentials are checked again when it ends."
		}
	}
	footer := "enter/esc to close"
	if m.alertSSO {
		footer = "l: aws sso login · enter/esc to close"
	}
	wrap := lipgloss.NewStyle().Width(width)
	return strings.Join([]string{
		th.StatusError.Bold(true).Render("⚠ " + title),
		"",
		wrap.Render(hint),
		"",
		wrap.Inherit(th.Muted).Render(m.alert.Err.Error()),
		"",
		th.Muted.Render(footer),
	}, "\n")
}

func (m Model) confirmView(width int) string {
	th := m.theme
	wrap := lipgloss.NewStyle().Width(width)
	return strings.Join([]string{
		th.Warning.Bold(true).Render("⚠ " + m.confirm.Title),
		"",
		wrap.Render(m.confirm.Body),
		"",
		th.Selected.Render("y") + th.Muted.Render(" confirm · ") + th.Selected.Render("n/esc") + th.Muted.Render(" cancel"),
	}, "\n")
}

// overlayDialog draws the open dialog above the rest of the screen.
func (m Model) overlayDialog(base string) string {
	if m.dialog == dialogNone {
		return base
	}
	x, y, _, _ := m.dialogRect()
	return lipgloss.NewCompositor(
		lipgloss.NewLayer(base),
		lipgloss.NewLayer(m.renderDialog()).X(x).Y(y).Z(1),
	).Render()
}
