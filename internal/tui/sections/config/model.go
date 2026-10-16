// Package config is the menu section for configuration: parameters (SSM
// Parameter Store) and secrets (Secrets Manager). Encrypted values are
// shown only after an explicit, confirmed reveal, and never kept.
package config

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"infra/internal/domain"
	"infra/internal/filter"
	"infra/internal/tui/components/details"
	"infra/internal/tui/components/listview"
	"infra/internal/tui/section"
	"infra/internal/tui/theme"
)

// Title is the menu label of this section.
const Title = "Config"

const loadTimeout = 30 * time.Second

// Service is the subset of use cases this section needs. It is satisfied by
// *app.ConfigService and by fakes in tests.
type Service interface {
	Parameters(ctx context.Context) ([]domain.Parameter, error)
	ParameterValue(ctx context.Context, p domain.Parameter) (string, error)
	Secrets(ctx context.Context) ([]domain.Secret, error)
	SecretValue(ctx context.Context, s domain.Secret) (domain.SecretValue, error)
	OpenParameterConsole(p domain.Parameter) (string, error)
	OpenSecretConsole(s domain.Secret) (string, error)
}

type source int

const (
	sourceParameters source = iota
	sourceSecrets
)

type valueState int

const (
	valueHidden valueState = iota
	valueLoading
	valueShown
	valueFailed
)

type paramsLoadedMsg struct {
	items []domain.Parameter
	err   error
}

type secretsLoadedMsg struct {
	items []domain.Secret
	err   error
}

type valueMsg struct {
	seq  int
	text string
	err  error
}

// revealMsg is sent once the user confirmed revealing a value.
type revealMsg struct{ seq int }

type keyMap struct {
	Switch   key.Binding
	Details  key.Binding
	Reveal   key.Binding
	Console  key.Binding
	Favorite key.Binding
	Refresh  key.Binding
	Back     key.Binding
	Scroll   key.Binding
}

// Model implements section.Section.
type Model struct {
	svc   Service
	deps  section.Deps
	theme theme.Theme
	keys  keyMap

	params  *listview.Model[domain.Parameter]
	secrets *listview.Model[domain.Secret]
	viewer  viewport.Model

	source        source
	secretsLoaded bool
	details       bool
	param         domain.Parameter
	secret        domain.Secret
	value         string // never kept after leaving the details
	valueState    valueState
	valueErr      error
	seq           int // drops values read for details already left
	width         int
}

// New creates the Config section.
func New(svc Service, deps section.Deps) *Model {
	th := deps.Theme
	m := &Model{
		svc:     svc,
		deps:    deps,
		theme:   th,
		params:  listview.New("Parameters", "parameters", paramColumns, paramFields, "name, type:secure, path:/prod", th),
		secrets: listview.New("Secrets", "secrets", secretColumns, secretFields, "name, rotation:on, tag:team=core", th),
		viewer:  viewport.New(),
		keys: keyMap{
			Switch:   key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "parameters/secrets")),
			Details:  key.NewBinding(key.WithKeys("enter", "d"), key.WithHelp("enter", "details")),
			Reveal:   key.NewBinding(key.WithKeys("v"), key.WithHelp("v", "reveal value")),
			Console:  key.NewBinding(key.WithKeys("o"), key.WithHelp("o", "console")),
			Favorite: key.NewBinding(key.WithKeys("*"), key.WithHelp("*", "favorite")),
			Refresh:  key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "refresh")),
			Back:     key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
			Scroll:   key.NewBinding(key.WithKeys("up", "down", "pgup", "pgdown"), key.WithHelp("↑↓", "scroll")),
		},
	}
	m.params.SetMarker(func(p domain.Parameter) string { return deps.Star(paramBookmark(p.Name)) })
	m.secrets.SetMarker(func(s domain.Secret) string { return deps.Star(secretBookmark(s.Name)) })
	return m
}

func paramBookmark(name string) domain.Bookmark {
	return domain.Bookmark{Kind: domain.KindParameter, ID: name, Label: name}
}

func secretBookmark(name string) domain.Bookmark {
	return domain.Bookmark{Kind: domain.KindSecret, ID: name, Label: name}
}

func (m *Model) Title() string { return Title }
func (m *Model) Init() tea.Cmd { return m.loadParams() }

func (m *Model) Focus() {
	m.params.Focus()
	m.secrets.Focus()
}

func (m *Model) Blur() {
	m.params.Blur()
	m.secrets.Blur()
}

func (m *Model) SetSize(width, height int) {
	m.width = width
	m.params.SetSize(width, height-1) // the source tabs take a line
	m.secrets.SetSize(width, height-1)
	m.viewer.SetWidth(width)
	m.viewer.SetHeight(max(1, height))
	if m.details {
		m.renderDetails()
	}
}

func (m *Model) InputMode() section.InputMode {
	if !m.details && !m.currentCapturing() {
		return section.ModeNormal
	}
	return section.ModeCapture
}

func (m *Model) currentCapturing() bool {
	if m.source == sourceSecrets {
		return m.secrets.Capturing()
	}
	return m.params.Capturing()
}

func (m *Model) loadParams() tea.Cmd {
	svc := m.svc
	return tea.Batch(m.params.StartLoading(), func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), loadTimeout)
		defer cancel()
		items, err := svc.Parameters(ctx)
		return paramsLoadedMsg{items: items, err: err}
	})
}

func (m *Model) loadSecrets() tea.Cmd {
	m.secretsLoaded = true
	svc := m.svc
	return tea.Batch(m.secrets.StartLoading(), func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), loadTimeout)
		defer cancel()
		items, err := svc.Secrets(ctx)
		return secretsLoadedMsg{items: items, err: err}
	})
}

func (m *Model) Update(msg tea.Msg) (section.Section, tea.Cmd) {
	switch msg := msg.(type) {
	case paramsLoadedMsg:
		if msg.err != nil {
			m.params.SetError(msg.err)
			return m, section.Error(msg.err)
		}
		m.params.SetItems(msg.items)
		return m, section.Status(fmt.Sprintf("Loaded %d parameters", len(msg.items)))
	case secretsLoadedMsg:
		if msg.err != nil {
			m.secrets.SetError(msg.err)
			return m, section.Error(msg.err)
		}
		m.secrets.SetItems(msg.items)
		return m, section.Status(fmt.Sprintf("Loaded %d secrets", len(msg.items)))
	case revealMsg:
		if msg.seq == m.seq && m.details {
			return m, m.readValue()
		}
		return m, nil
	case valueMsg:
		if msg.seq != m.seq || !m.details {
			return m, nil // left the details meanwhile: drop the value
		}
		if msg.err != nil {
			m.valueState, m.valueErr = valueFailed, msg.err
			m.renderDetails()
			return m, section.Error(msg.err)
		}
		m.value, m.valueState = msg.text, valueShown
		m.renderDetails()
		return m, nil
	case section.RevealMsg:
		return m, m.reveal(msg.Bookmark)
	case tea.KeyPressMsg:
		return m, m.handleKey(msg)
	}
	return m, tea.Batch(m.params.Update(msg), m.secrets.Update(msg))
}

// reveal shows a bookmarked parameter or secret in its list.
func (m *Model) reveal(b domain.Bookmark) tea.Cmd {
	switch b.Kind {
	case domain.KindParameter:
		m.closeDetails()
		m.source = sourceParameters
		m.params.Reveal(func(p domain.Parameter) bool { return p.Name == b.ID })
	case domain.KindSecret:
		m.closeDetails()
		m.source = sourceSecrets
		var cmd tea.Cmd
		if !m.secretsLoaded {
			cmd = m.loadSecrets()
		}
		m.secrets.Reveal(func(s domain.Secret) bool { return s.Name == b.ID })
		return cmd
	}
	return nil
}

func (m *Model) handleKey(msg tea.KeyPressMsg) tea.Cmd {
	if m.details {
		return m.handleDetailsKey(msg)
	}
	if m.source == sourceSecrets {
		if m.secrets.Filtering() {
			return m.secrets.Update(msg)
		}
	} else if m.params.Filtering() {
		return m.params.Update(msg)
	}

	switch {
	case key.Matches(msg, m.keys.Switch):
		if m.source == sourceParameters {
			m.source = sourceSecrets
			if !m.secretsLoaded {
				return m.loadSecrets()
			}
			return nil
		}
		m.source = sourceParameters
		return nil
	case key.Matches(msg, m.keys.Refresh):
		if m.source == sourceSecrets {
			return m.loadSecrets()
		}
		return m.loadParams()
	case key.Matches(msg, m.keys.Back):
		if m.source == sourceSecrets {
			return m.secrets.Update(msg) // clears an applied filter
		}
		return m.params.Update(msg)
	}

	if m.source == sourceSecrets {
		if s, ok := m.secrets.Selected(); ok {
			switch {
			case key.Matches(msg, m.keys.Favorite):
				cmd := m.deps.ToggleFavorite(secretBookmark(s.Name))
				m.secrets.Refresh()
				return cmd
			case key.Matches(msg, m.keys.Details):
				return m.openSecret(s)
			case key.Matches(msg, m.keys.Console):
				return m.openConsole(s.Name, func() (string, error) { return m.svc.OpenSecretConsole(s) })
			}
		}
		return m.secrets.Update(msg)
	}
	if p, ok := m.params.Selected(); ok {
		switch {
		case key.Matches(msg, m.keys.Favorite):
			cmd := m.deps.ToggleFavorite(paramBookmark(p.Name))
			m.params.Refresh()
			return cmd
		case key.Matches(msg, m.keys.Details):
			return m.openParam(p)
		case key.Matches(msg, m.keys.Console):
			return m.openConsole(p.Name, func() (string, error) { return m.svc.OpenParameterConsole(p) })
		}
	}
	return m.params.Update(msg)
}

func (m *Model) handleDetailsKey(msg tea.KeyPressMsg) tea.Cmd {
	switch {
	case key.Matches(msg, m.keys.Back):
		m.closeDetails()
		return nil
	case key.Matches(msg, m.keys.Reveal):
		if m.valueState == valueShown || m.valueState == valueLoading {
			return nil
		}
		return m.confirmReveal()
	case key.Matches(msg, m.keys.Console):
		if m.source == sourceSecrets {
			s := m.secret
			return m.openConsole(s.Name, func() (string, error) { return m.svc.OpenSecretConsole(s) })
		}
		p := m.param
		return m.openConsole(p.Name, func() (string, error) { return m.svc.OpenParameterConsole(p) })
	}
	var cmd tea.Cmd
	m.viewer, cmd = m.viewer.Update(msg)
	return cmd
}

func (m *Model) openParam(p domain.Parameter) tea.Cmd {
	m.deps.Visit(paramBookmark(p.Name))
	m.openDetails()
	m.param = p
	m.renderDetails()
	if p.Secure() {
		return nil // revealed only on request
	}
	return m.readValue()
}

func (m *Model) openSecret(s domain.Secret) tea.Cmd {
	m.deps.Visit(secretBookmark(s.Name))
	m.openDetails()
	m.secret = s
	m.renderDetails()
	return nil
}

func (m *Model) openDetails() {
	m.details = true
	m.seq++
	m.value, m.valueState, m.valueErr = "", valueHidden, nil
	m.viewer.GotoTop()
}

// closeDetails forgets the value, so it is not kept in memory.
func (m *Model) closeDetails() {
	m.details = false
	m.seq++
	m.value, m.valueState, m.valueErr = "", valueHidden, nil
	m.viewer.SetContent("")
}

func (m *Model) confirmReveal() tea.Cmd {
	name := m.param.Name
	what := "decrypted value of parameter"
	if m.source == sourceSecrets {
		name, what = m.secret.Name, "value of secret"
	}
	seq := m.seq
	return section.Confirm("Reveal value", fmt.Sprintf("Show the %s %s on screen?", what, name),
		func() tea.Msg { return revealMsg{seq: seq} })
}

// readValue reads the value of the parameter or secret in the details.
func (m *Model) readValue() tea.Cmd {
	m.valueState = valueLoading
	m.renderDetails()
	svc, seq := m.svc, m.seq
	if m.source == sourceSecrets {
		s := m.secret
		return func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), loadTimeout)
			defer cancel()
			v, err := svc.SecretValue(ctx, s)
			text := v.Text
			if text == "" && len(v.Binary) > 0 {
				text = fmt.Sprintf("<binary, %d bytes>", len(v.Binary))
			}
			return valueMsg{seq: seq, text: text, err: err}
		}
	}
	p := m.param
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), loadTimeout)
		defer cancel()
		v, err := svc.ParameterValue(ctx, p)
		return valueMsg{seq: seq, text: v, err: err}
	}
}

func (m *Model) openConsole(name string, open func() (string, error)) tea.Cmd {
	return func() tea.Msg {
		url, err := open()
		if err != nil {
			return section.StatusMsg{Text: fmt.Sprintf("%v (%s)", err, url), Error: true}
		}
		return section.StatusMsg{Text: "Opened console for " + name}
	}
}

func (m *Model) Help() []key.Binding {
	k := m.keys
	if m.details {
		b := []key.Binding{k.Scroll, k.Console, k.Back}
		if m.valueState == valueHidden || m.valueState == valueFailed {
			b = append([]key.Binding{k.Reveal}, b...)
		}
		return b
	}
	list := m.params.Help
	filtering := m.params.Filtering()
	if m.source == sourceSecrets {
		list, filtering = m.secrets.Help, m.secrets.Filtering()
	}
	if filtering {
		return list()
	}
	return append([]key.Binding{k.Details, k.Switch, k.Console, k.Favorite, k.Refresh}, list()...)
}

func (m *Model) View() string {
	if m.details {
		return m.viewer.View()
	}
	th := m.theme
	tab := func(label string, active bool) string {
		if active {
			return th.MenuActive.Render(" " + label + " ")
		}
		return th.Muted.Render(" " + label + " ")
	}
	tabs := tab("Parameters", m.source == sourceParameters) + " " + tab("Secrets", m.source == sourceSecrets) +
		th.Muted.Render("   tab to switch")
	if m.source == sourceSecrets {
		return tabs + "\n" + m.secrets.View()
	}
	return tabs + "\n" + m.params.View()
}

func (m *Model) renderDetails() {
	var head string
	if m.source == sourceSecrets {
		s := m.secret
		head = details.Render(m.theme, s.Name, "secret", []details.Field{
			{Label: "Description", Value: s.Description},
			{Label: "Rotation", Value: onOff(s.RotationEnabled)},
			{Label: "Last changed", Value: when(s.LastChanged)},
			{Label: "Last rotated", Value: when(s.LastRotated)},
			{Label: "Last accessed", Value: when(s.LastAccessed)},
			{Label: "KMS key", Value: s.KMSKeyID},
			{Label: "ARN", Value: s.ARN},
		}, details.Section{Title: "Tags", Fields: details.Tags(s.Tags)})
	} else {
		p := m.param
		head = details.Render(m.theme, p.Name, "parameter", []details.Field{
			{Label: "Type", Value: p.Type},
			{Label: "Version", Value: strconv.FormatInt(p.Version, 10)},
			{Label: "Description", Value: p.Description},
			{Label: "Tier", Value: p.Tier},
			{Label: "Last modified", Value: when(p.LastModified)},
			{Label: "Modified by", Value: p.ModifiedBy},
			{Label: "KMS key", Value: p.KeyID},
			{Label: "ARN", Value: p.ARN},
		})
	}
	m.viewer.SetContent(head + "\n" + m.theme.Title.Render("Value") + "\n" + m.valueView())
}

func (m *Model) valueView() string {
	th := m.theme
	switch m.valueState {
	case valueLoading:
		return th.Muted.Render("Reading…")
	case valueFailed:
		return th.StatusError.Render(m.valueErr.Error())
	case valueHidden:
		return "••••••••  " + th.Muted.Render("press v to reveal")
	}
	text := m.value
	var buf bytes.Buffer
	if json.Indent(&buf, []byte(text), "", "  ") == nil {
		text = buf.String()
	} else if m.source == sourceParameters && m.param.Type == domain.ParameterStringList {
		text = strings.ReplaceAll(text, ",", "\n")
	}
	return lipgloss.NewStyle().Width(max(20, m.width)).Render(text)
}

func onOff(b bool) string {
	if b {
		return "on"
	}
	return "off"
}

func when(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Local().Format(time.DateTime)
}

var paramColumns = []listview.Column[domain.Parameter]{
	{Title: "Name", Value: func(p domain.Parameter) string { return p.Name }},
	{Title: "Type", Width: 12, Value: func(p domain.Parameter) string { return p.Type }},
	{Title: "Version", Width: 7, Value: func(p domain.Parameter) string { return strconv.FormatInt(p.Version, 10) }},
	{Title: "Modified", Width: 19, Value: func(p domain.Parameter) string { return when(p.LastModified) }},
	{Title: "Description", Width: 30, Value: func(p domain.Parameter) string { return p.Description }},
}

func paramFields(p domain.Parameter) []filter.Field {
	return []filter.Field{
		{Name: "name", Value: p.Name},
		{Name: "path", Value: p.Name},
		{Name: "type", Value: p.Type},
		{Name: "description", Value: p.Description},
	}
}

var secretColumns = []listview.Column[domain.Secret]{
	{Title: "Name", Value: func(s domain.Secret) string { return s.Name }},
	{Title: "Rotation", Width: 8, Value: func(s domain.Secret) string { return onOff(s.RotationEnabled) }},
	{Title: "Changed", Width: 19, Value: func(s domain.Secret) string { return when(s.LastChanged) }},
	{Title: "Description", Width: 30, Value: func(s domain.Secret) string { return s.Description }},
}

func secretFields(s domain.Secret) []filter.Field {
	fields := []filter.Field{
		{Name: "name", Value: s.Name},
		{Name: "rotation", Value: onOff(s.RotationEnabled)},
		{Name: "description", Value: s.Description},
	}
	for k, v := range s.Tags {
		fields = append(fields, filter.Field{Name: "tag", Value: k + "=" + v})
	}
	return fields
}
