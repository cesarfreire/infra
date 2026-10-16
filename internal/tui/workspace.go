package tui

import (
	"context"
	"errors"
	"fmt"
	"time"

	tea "charm.land/bubbletea/v2"

	"infra/internal/domain"
	"infra/internal/tui/components/picker"
	"infra/internal/tui/section"
)

const verifyTimeout = 15 * time.Second

// Workspace switches the cloud context (profile and region). It is
// implemented by the composition root, which knows the concrete adapters.
type Workspace interface {
	Profiles() ([]domain.Profile, error)
	Regions() []domain.Region
	// Open builds the sections for profile and region. It must not block on
	// the network; Environment.Verify does. An empty profile means the
	// provider's default credential chain; an empty region means the
	// profile's region, and domain.ErrNoRegion if it has none.
	Open(profile, region string) (Environment, error)
	// Login starts `aws sso login` for profile as a session shown inside
	// the TUI.
	Login(profile string) (Login, error)
}

// Login is a running SSO login.
type Login struct {
	SessionID int
	// Wait blocks until the login ends and returns its error.
	Wait func() error
}

type loginDoneMsg struct {
	profile string
	err     error
}

// Environment is the result of opening a workspace.
type Environment struct {
	Profile  string
	Region   string
	Sections []section.Section
	// Verify checks the credentials and returns who they belong to.
	Verify func(ctx context.Context) (domain.Identity, error)
	// ReadOnly marks a profile whose resources must not be changed.
	ReadOnly bool
}

// envOpenedMsg and identityMsg carry the sequence number of the switch that
// produced them, so results of an older switch are dropped.
type envOpenedMsg struct {
	seq     int
	profile string
	env     Environment
	err     error
}

type identityMsg struct {
	seq int
	id  domain.Identity
	err error
}

func (m Model) openCmd(seq int, profile, region string) tea.Cmd {
	ws := m.workspace
	return func() tea.Msg {
		env, err := ws.Open(profile, region)
		return envOpenedMsg{seq: seq, profile: profile, env: env, err: err}
	}
}

// switchTo opens a new environment; the current one stays until it succeeds.
func (m *Model) switchTo(profile, region string) tea.Cmd {
	m.seq++
	m.switching = true
	return m.openCmd(m.seq, profile, region)
}

func (m Model) handleEnvOpened(msg envOpenedMsg) (tea.Model, tea.Cmd) {
	if msg.seq != m.seq {
		return m, nil
	}
	m.switching = false
	if errors.Is(msg.err, domain.ErrNoRegion) {
		m.profile = msg.profile
		cmd := m.openRegionPicker()
		return m, tea.Batch(cmd, section.Status("Choose a region for profile "+profileLabel(msg.profile)))
	}
	if msg.err != nil {
		return m, section.Error(msg.err)
	}
	return m, m.applyEnvironment(msg.env)
}

// applyEnvironment replaces the sections. Sections reused from the previous
// environment (e.g. Sessions) keep their state and are not re-initialized.
func (m *Model) applyEnvironment(env Environment) tea.Cmd {
	previous := map[section.Section]bool{}
	for _, s := range m.sections {
		previous[s] = true
	}
	if m.focus == focusContent && len(m.sections) > 0 {
		m.current().Blur()
	}

	m.sections = env.Sections
	m.initialized = make([]bool, len(env.Sections))
	m.active = min(m.active, max(0, len(env.Sections)-1))
	m.focus = focusMenu
	m.profile, m.region = env.Profile, env.Region
	m.readOnly = env.ReadOnly
	m.identity, m.identityErr = domain.Identity{}, nil
	m.verifying = env.Verify != nil
	m.resize()

	var cmds []tea.Cmd
	for i, s := range m.sections {
		if previous[s] {
			m.initialized[i] = true
			continue
		}
		if l, ok := s.(section.Listener); ok {
			cmds = append(cmds, l.Listen())
		}
	}
	if len(m.sections) > 0 && !m.initialized[m.active] {
		m.initialized[m.active] = true
		cmds = append(cmds, m.current().Init())
	}
	if env.Verify != nil {
		seq, verify := m.seq, env.Verify
		cmds = append(cmds, func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), verifyTimeout)
			defer cancel()
			id, err := verify(ctx)
			return identityMsg{seq: seq, id: id, err: err}
		})
	}
	cmds = append(cmds, section.Status(fmt.Sprintf("Using profile %s in %s", profileLabel(m.profile), m.region)))
	return tea.Batch(cmds...)
}

func (m Model) handleIdentity(msg identityMsg) (tea.Model, tea.Cmd) {
	if msg.seq != m.seq {
		return m, nil
	}
	m.verifying = false
	m.identity, m.identityErr = msg.id, msg.err
	if msg.err != nil {
		return m, section.Error(msg.err)
	}
	return m, nil
}

func (m *Model) openProfilePicker() tea.Cmd {
	profiles, err := m.workspace.Profiles()
	if err != nil {
		return section.Error(fmt.Errorf("reading AWS profiles: %w", err))
	}
	if len(profiles) == 0 {
		return section.Error(errors.New("no profiles found in the AWS CLI config files"))
	}
	m.profiles = profiles
	items := make([]picker.Item, len(profiles))
	for i, p := range profiles {
		detail := p.Region
		if p.SSO {
			detail += " · sso"
		}
		items[i] = picker.Item{Value: p.Name, Label: p.Name, Detail: detail}
	}
	return m.openPicker(dialogProfile, "Select profile", items, m.profile)
}

func (m *Model) openRegionPicker() tea.Cmd {
	regions := m.workspace.Regions()
	items := make([]picker.Item, len(regions))
	for i, r := range regions {
		items[i] = picker.Item{Value: r.Code, Label: r.Code, Detail: r.Name}
	}
	return m.openPicker(dialogRegion, "Select region", items, m.region)
}

// choose applies a picker selection.
func (m *Model) choose(kind dialogKind, value string) tea.Cmd {
	if kind == dialogSearch {
		return m.chooseSearch(value)
	}
	if kind == dialogRegion {
		return m.switchTo(m.profile, value)
	}
	// A profile brings its own default region; otherwise keep the current one.
	region := m.region
	for _, p := range m.profiles {
		if p.Name == value && p.Region != "" {
			region = p.Region
		}
	}
	return m.switchTo(value, region)
}

// isSSO reports whether profile is configured for IAM Identity Center.
func (m *Model) isSSO(profile string) bool {
	profiles, err := m.workspace.Profiles()
	if err != nil {
		return false
	}
	name := profileLabel(profile)
	for _, p := range profiles {
		if p.Name == name {
			return p.SSO
		}
	}
	return false
}

// login runs `aws sso login` in a session, then re-opens the environment
// so the new credentials are verified.
func (m *Model) login() tea.Cmd {
	profile, ws := m.profile, m.workspace
	l, err := ws.Login(profile)
	if err != nil {
		return section.Error(fmt.Errorf("starting aws sso login: %w", err))
	}
	return tea.Batch(
		func() tea.Msg { return section.OpenSessionMsg{ID: l.SessionID} },
		func() tea.Msg { return loginDoneMsg{profile: profile, err: l.Wait()} },
	)
}

func (m Model) handleLoginDone(msg loginDoneMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		return m, section.Error(fmt.Errorf("aws sso login: %w", msg.err))
	}
	if msg.profile != m.profile {
		return m, nil // the user switched profile meanwhile
	}
	return m, tea.Batch(section.Status("Logged in; checking credentials…"), m.switchTo(m.profile, m.region))
}

func profileLabel(profile string) string {
	if profile == "" {
		return "default"
	}
	return profile
}
