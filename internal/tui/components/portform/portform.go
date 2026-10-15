// Package portform is the form that collects the ports of a tunnel.
package portform

import (
	"errors"
	"strconv"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"infra/internal/tui/theme"
)

const (
	fieldRemote = iota
	fieldLocal
)

const defaultHint = "The tunnel runs in the background; follow or close it in Sessions."

// Model holds the remote and local port inputs.
type Model struct {
	inputs      [2]textinput.Model
	focus       int
	err         error
	next        key.Binding
	fixedRemote int // when > 0 the remote port is not editable
	hint        string
}

// New returns an empty form.
func New() Model {
	var f Model
	for i := range f.inputs {
		in := textinput.New()
		in.CharLimit = 5
		in.SetWidth(10)
		f.inputs[i] = in
	}
	f.inputs[fieldRemote].Prompt = "Remote port: "
	f.inputs[fieldRemote].Placeholder = "5432"
	f.inputs[fieldLocal].Prompt = "Local port:  "
	f.inputs[fieldLocal].Placeholder = "same as remote"
	f.next = key.NewBinding(key.WithKeys("tab", "shift+tab", "up", "down"), key.WithHelp("tab", "next field"))
	f.hint = defaultHint
	return f
}

// Reset clears the form, makes both ports editable and focuses the first field.
func (f *Model) Reset() tea.Cmd {
	for i := range f.inputs {
		f.inputs[i].SetValue("")
	}
	f.err = nil
	f.fixedRemote = 0
	f.hint = defaultHint
	f.inputs[fieldLocal].Placeholder = "same as remote"
	return f.setFocus(fieldRemote)
}

// ResetFixedRemote clears the form for a known remote port (e.g. a cache
// endpoint): only the local port is asked, defaulting to the remote one.
func (f *Model) ResetFixedRemote(remote int, hint string) tea.Cmd {
	f.Reset() // its focus command is replaced by the local field's below
	f.fixedRemote = remote
	if hint != "" {
		f.hint = hint
	}
	f.inputs[fieldLocal].Placeholder = strconv.Itoa(remote)
	return f.setFocus(fieldLocal)
}

func (f *Model) setFocus(idx int) tea.Cmd {
	f.focus = idx
	var cmd tea.Cmd
	for i := range f.inputs {
		if i == idx {
			cmd = f.inputs[i].Focus()
		} else {
			f.inputs[i].Blur()
		}
	}
	return cmd
}

// Update switches fields on tab/arrows and edits the focused input.
func (f *Model) Update(msg tea.Msg) tea.Cmd {
	if k, ok := msg.(tea.KeyPressMsg); ok {
		if key.Matches(k, f.next) && f.fixedRemote > 0 {
			return nil
		}
		if key.Matches(k, f.next) {
			return f.setFocus((f.focus + 1) % len(f.inputs))
		}
		f.err = nil
	}
	var cmd tea.Cmd
	f.inputs[f.focus], cmd = f.inputs[f.focus].Update(msg)
	return cmd
}

// SetError shows err under the inputs.
func (f *Model) SetError(err error) { f.err = err }

// Err returns the error being shown.
func (f *Model) Err() error { return f.err }

// NextField is the binding that moves between inputs, for help.
func (f *Model) NextField() key.Binding { return f.next }

// Ports parses the inputs; an empty local port reuses the remote port.
func (f *Model) Ports() (local, remote int, err error) {
	remoteStr := strings.TrimSpace(f.inputs[fieldRemote].Value())
	if f.fixedRemote > 0 {
		remoteStr = strconv.Itoa(f.fixedRemote)
	}
	localStr := strings.TrimSpace(f.inputs[fieldLocal].Value())
	if remoteStr == "" {
		return 0, 0, errors.New("remote port is required")
	}
	if localStr == "" {
		localStr = remoteStr
	}
	if remote, err = strconv.Atoi(remoteStr); err != nil {
		return 0, 0, errors.New("remote port must be a number")
	}
	if local, err = strconv.Atoi(localStr); err != nil {
		return 0, 0, errors.New("local port must be a number")
	}
	return local, remote, nil
}

// View renders a heading, the inputs, the error and a hint.
func (f Model) View(th theme.Theme, title, subtitle string) string {
	remote := f.inputs[fieldRemote].View()
	if f.fixedRemote > 0 {
		remote = "Remote port: " + strconv.Itoa(f.fixedRemote)
	}
	lines := []string{
		th.Title.Render(title) + " " + th.Muted.Render(subtitle),
		"",
		remote,
		f.inputs[fieldLocal].View(),
	}
	if f.err != nil {
		lines = append(lines, "", th.StatusError.Render(f.err.Error()))
	}
	lines = append(lines, "", th.Muted.Render(f.hint))
	return strings.Join(lines, "\n")
}
