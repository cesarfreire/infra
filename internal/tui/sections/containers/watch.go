package containers

import (
	"context"
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"infra/internal/app"
	"infra/internal/domain"
	"infra/internal/tui/section"
)

// watchEvery is how often the deployment view reads the service and tasks.
const watchEvery = 3 * time.Second

const maxWatchEvents = 8

type watchTickMsg struct{ seq int }

type rolloutMsg struct {
	seq     int
	rollout domain.Rollout
	err     error
	at      time.Time
}

// revisionMsg carries the previous revision and the diff to it, for the
// diff view or to confirm a rollback.
type revisionMsg struct {
	seq      int
	change   app.RevisionChange
	err      error
	rollback bool
}

// watchState is the deployment view: the latest snapshot and what changed
// since the one before.
type watchState struct {
	seq      int // bumped to stop the refresh loop (leave, pause, refresh now)
	service  domain.Service
	rollout  *domain.Rollout
	err      error
	at       time.Time
	paused   bool
	previous map[string]string // task ID → state, from the last snapshot
	changed  map[string]bool   // tasks whose state changed in the last snapshot
	outcome  string            // "completed in 3m10s", "failed: ..."
	failed   bool
	showDiff bool
	diff     *app.RevisionChange
	diffErr  error
}

type watchKeys struct {
	Pause    key.Binding
	Refresh  key.Binding
	Scroll   key.Binding
	Diff     key.Binding
	Rollback key.Binding
}

func defaultWatchKeys() watchKeys {
	return watchKeys{
		Pause:    key.NewBinding(key.WithKeys("p", "space"), key.WithHelp("p", "pause")),
		Refresh:  key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "refresh now")),
		Scroll:   key.NewBinding(key.WithKeys("up", "down", "pgup", "pgdown", "home", "end"), key.WithHelp("↑↓", "scroll")),
		Diff:     key.NewBinding(key.WithKeys("v"), key.WithHelp("v", "changes")),
		Rollback: key.NewBinding(key.WithKeys("R"), key.WithHelp("R", "roll back")),
	}
}

// openWatch shows the deployment view of s and starts refreshing it.
func (m *Model) openWatch(s domain.Service) tea.Cmd {
	m.deps.Visit(serviceBookmark(s))
	m.leaveView()
	m.view = viewWatch
	m.watch = watchState{seq: m.watch.seq + 1, service: s}
	m.watchVP.GotoTop()
	m.renderWatch()
	return m.readRollout()
}

// readRollout reads a snapshot; its result schedules the next one.
func (m *Model) readRollout() tea.Cmd {
	svc, seq, s := m.svc, m.watch.seq, m.watch.service
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), loadTimeout)
		defer cancel()
		r, err := svc.Rollout(ctx, s)
		return rolloutMsg{seq: seq, rollout: r, err: err, at: time.Now()}
	}
}

// restartWatch reads now, dropping the pending refresh so only one loop runs.
func (m *Model) restartWatch() tea.Cmd {
	m.watch.seq++
	return m.readRollout()
}

func (m *Model) handleRollout(msg rolloutMsg) tea.Cmd {
	w := &m.watch
	if msg.seq != w.seq || m.view != viewWatch {
		return nil
	}
	seq := w.seq
	next := tea.Tick(watchEvery, func(time.Time) tea.Msg { return watchTickMsg{seq: seq} })
	if msg.err != nil {
		first := w.rollout == nil
		w.err = msg.err
		m.renderWatch()
		if first {
			return tea.Batch(section.Error(msg.err), next)
		}
		return next // keep the last snapshot on screen and retry
	}

	r := msg.rollout
	states := make(map[string]string, len(r.Tasks))
	changed := map[string]bool{}
	for _, t := range r.Tasks {
		states[t.ID] = taskState(t)
		if w.previous != nil && w.previous[t.ID] != states[t.ID] {
			changed[t.ID] = true
		}
	}
	var status tea.Cmd
	wasRolling := w.rollout != nil && w.rollout.Service.RollingOut()
	if primary, ok := primaryDeployment(r.Service); ok {
		title := "Deployment of " + r.Service.Name
		switch {
		case primary.RolloutState == "FAILED" && !w.failed:
			w.failed = true
			w.outcome = "failed: " + primary.RolloutReason
			status = tea.Batch(
				section.Error(fmt.Errorf("deployment of %s failed: %s", r.Service.Name, primary.RolloutReason)),
				section.Notify(title+" failed", primary.RolloutReason))
		case wasRolling && !r.Service.RollingOut():
			w.outcome = "completed in " + since(primary.CreatedAt, msg.at)
			status = tea.Batch(
				section.Status(fmt.Sprintf("%s %s", title, w.outcome)),
				section.Notify(title+" completed", fmt.Sprintf("%s in %s, %s", r.Service.Cluster, since(primary.CreatedAt, msg.at), primary.TaskDefinition)))
		}
	}
	w.rollout, w.err, w.at = &r, nil, msg.at
	w.previous, w.changed = states, changed
	m.renderWatch()
	if w.paused {
		return status
	}
	return tea.Batch(next, status)
}

func (m *Model) handleWatchKey(msg tea.KeyPressMsg) tea.Cmd {
	k := m.watchKeys
	switch {
	case key.Matches(msg, k.Pause):
		m.watch.paused = !m.watch.paused
		m.renderWatch()
		if m.watch.paused {
			m.watch.seq++ // stops the loop
			return section.Status("Paused; press p to resume")
		}
		return m.restartWatch()
	case key.Matches(msg, k.Refresh):
		return m.restartWatch()
	case key.Matches(msg, m.keys.Deploy):
		return m.confirmDeploy(m.watch.service)
	case key.Matches(msg, k.Diff):
		m.watch.showDiff = !m.watch.showDiff
		m.renderWatch()
		if m.watch.showDiff && m.watch.diff == nil {
			return m.loadRevision(false)
		}
		return nil
	case key.Matches(msg, k.Rollback):
		if cmd := m.deps.BlockWrite("roll back " + m.watch.service.Name); cmd != nil {
			return cmd
		}
		return m.loadRevision(true)
	case key.Matches(msg, k.Scroll):
		var cmd tea.Cmd
		m.watchVP, cmd = m.watchVP.Update(msg)
		return cmd
	}
	return nil
}

func (m *Model) watchHelp() []key.Binding {
	k := m.watchKeys
	if m.watch.showDiff {
		return []key.Binding{k.Scroll, k.Rollback, key.NewBinding(key.WithKeys("v", "esc"), key.WithHelp("v/esc", "close"))}
	}
	pause := k.Pause
	if m.watch.paused {
		pause = key.NewBinding(key.WithKeys("p"), key.WithHelp("p", "resume"))
	}
	return []key.Binding{pause, k.Refresh, k.Diff, k.Rollback, m.keys.Deploy, k.Scroll, m.keys.Back}
}

// loadRevision reads the previous revision: to show the diff, or to
// confirm a rollback to it.
func (m *Model) loadRevision(rollback bool) tea.Cmd {
	svc, seq := m.svc, m.watch.seq
	s := m.watch.service
	if m.watch.rollout != nil {
		s = m.watch.rollout.Service
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), loadTimeout)
		defer cancel()
		change, err := svc.PreviousRevision(ctx, s)
		return revisionMsg{seq: seq, change: change, err: err, rollback: rollback}
	}
}

func (m *Model) handleRevision(msg revisionMsg) tea.Cmd {
	// The loop's seq changes on pause and refresh; the view is what matters.
	if m.view != viewWatch {
		return nil
	}
	if msg.err != nil {
		m.watch.diffErr = msg.err
		m.renderWatch()
		return section.Error(msg.err)
	}
	m.watch.diff, m.watch.diffErr = &msg.change, nil
	m.renderWatch()
	if !msg.rollback {
		return nil
	}
	return m.confirmRollback(msg.change)
}

// confirmRollback asks before deploying the previous revision, showing what
// it changes back.
func (m *Model) confirmRollback(change app.RevisionChange) tea.Cmd {
	s := m.watch.service
	svc, target := m.svc, change.From
	body := fmt.Sprintf("Deploy %s to %s, replacing %s?", target.Name(), s.Name, change.To.Name())
	for _, c := range change.Changes {
		if strings.HasSuffix(c.Field, ".image") {
			body += fmt.Sprintf(" %s goes back from %s to %s.", strings.TrimSuffix(c.Field, ".image"), c.New, c.Old)
		}
	}
	if len(change.Changes) == 0 {
		body += " Both revisions are identical: with a mutable image tag (e.g. latest) the rollback runs the same image again."
	}
	return section.Confirm("Roll back "+s.Name, body, func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), loadTimeout)
		defer cancel()
		if err := svc.Rollback(ctx, s, target); err != nil {
			return changedMsg{err: err}
		}
		return changedMsg{text: fmt.Sprintf("Rolling %s back to %s", s.Name, target.Name()), deployed: &s}
	})
}

// Progress shows the rollout in the terminal's progress indicator (e.g.
// the Windows Terminal tab and taskbar icon).
func (m *Model) Progress() *tea.ProgressBar {
	if m.view != viewWatch || m.watch.rollout == nil {
		return nil
	}
	if m.watch.failed {
		return tea.NewProgressBar(tea.ProgressBarError, 100)
	}
	primary, ok := primaryDeployment(m.watch.rollout.Service)
	if !ok || !m.watch.rollout.Service.RollingOut() {
		return nil
	}
	return tea.NewProgressBar(tea.ProgressBarDefault, min(100, primary.Running*100/max(1, primary.Desired)))
}

// renderWatch rebuilds the view, keeping the scroll position.
func (m *Model) renderWatch() {
	offset := m.watchVP.YOffset()
	m.watchVP.SetContent(m.watchContent())
	m.watchVP.SetYOffset(offset)
}

func (m *Model) watchContent() string {
	if m.watch.showDiff {
		return m.diffContent()
	}
	th, w := m.theme, m.watch
	s := w.service
	if w.rollout != nil {
		s = w.rollout.Service
	}

	var state string
	switch {
	case w.failed:
		state = th.StatusError.Render("● failed")
	case w.rollout == nil:
		state = th.Muted.Render("loading…")
	case s.RollingOut():
		state = th.Warning.Render("● rolling out")
	default:
		state = th.StatusInfo.Render("● steady")
	}
	refresh := fmt.Sprintf("every %s", watchEvery)
	if w.paused {
		refresh = th.Warning.Render("paused")
	}
	if !w.at.IsZero() {
		refresh = "updated " + w.at.Format("15:04:05") + " · " + refresh
	}
	lines := []string{
		th.Title.Render("Deployment · "+s.Name) + " " + th.Muted.Render(s.Cluster) + "  " + state + "  " + th.Muted.Render(refresh),
		th.Muted.Render(fmt.Sprintf("desired %d · running %d · pending %d", s.Desired, s.Running, s.Pending)),
	}
	if w.outcome != "" {
		style := th.StatusInfo
		if w.failed {
			style = th.StatusError
		}
		lines = append(lines, "", style.Bold(true).Render("Deployment "+w.outcome))
	}
	if w.err != nil {
		lines = append(lines, "", th.StatusError.Render(w.err.Error()))
	}
	if w.rollout == nil {
		return strings.Join(lines, "\n")
	}
	for _, g := range w.rollout.TargetGroups {
		lines = append(lines, th.Muted.Render("load balancer ")+th.Label.Render(g.Name)+"  "+targetSummary(m, g))
	}
	for _, p := range w.rollout.Problems {
		lines = append(lines, th.Muted.Render("⚠ "+p))
	}

	for _, d := range s.Deployments {
		lines = append(lines, "", m.deploymentLine(d))
		if image, digest := w.rollout.Image(d); image != "" {
			line := "    " + th.Muted.Render("image ") + image
			if digest != "" {
				line += th.Muted.Render(" @" + shortDigest(digest))
			}
			lines = append(lines, line)
		}
		tasks := w.rollout.TasksOf(d)
		if len(tasks) == 0 {
			lines = append(lines, th.Muted.Render("    no tasks yet"))
		}
		for _, t := range tasks {
			lines = append(lines, m.taskLines(t)...)
		}
	}
	if orphans := w.rollout.Orphans(); len(orphans) > 0 {
		lines = append(lines, "", th.Label.Render("Earlier deployments"))
		for _, t := range orphans {
			lines = append(lines, m.taskLines(t)...)
		}
	}

	lines = append(lines, "", th.Title.Render("Events"))
	events := s.Events[:min(len(s.Events), maxWatchEvents)]
	if len(events) == 0 {
		lines = append(lines, th.Muted.Render("No events."))
	}
	wrap := lipgloss.NewStyle().Width(max(20, m.width-12))
	for _, e := range events {
		msg := strings.Split(wrap.Render(e.Message), "\n")
		lines = append(lines, th.Muted.Render(e.Time.Local().Format("15:04:05"))+"  "+msg[0])
		for _, more := range msg[1:] {
			lines = append(lines, "          "+more)
		}
	}
	return strings.Join(lines, "\n")
}

func (m *Model) deploymentLine(d domain.Deployment) string {
	th := m.theme
	if d.Status != "PRIMARY" {
		// Older deployments have a desired count of 0 while ECS drains them.
		line := th.Label.Render(fmt.Sprintf("%-8s", d.Status)) + " " + th.Title.Render(d.TaskDefinition) + " " +
			th.Warning.Render("draining") + fmt.Sprintf("  %d running", d.Running)
		if d.Pending > 0 {
			line += fmt.Sprintf(" · %d pending", d.Pending)
		}
		return line
	}
	style := th.Muted
	switch d.RolloutState {
	case "IN_PROGRESS":
		style = th.Warning
	case "FAILED":
		style = th.StatusError
	case "COMPLETED":
		style = th.StatusInfo
	}
	line := th.Label.Render(fmt.Sprintf("%-8s", d.Status)) + " " + th.Title.Render(d.TaskDefinition) + " " +
		style.Render(strings.ToLower(strings.ReplaceAll(d.RolloutState, "_", " "))) + "  " +
		progressBar(th, d.Running, d.Desired, 16) + fmt.Sprintf(" %d/%d running", d.Running, d.Desired)
	if d.Pending > 0 {
		line += fmt.Sprintf(" · %d pending", d.Pending)
	}
	if d.Failed > 0 {
		line += th.StatusError.Render(fmt.Sprintf(" · %d failed", d.Failed))
	}
	if !d.CreatedAt.IsZero() {
		line += th.Muted.Render(" · started " + since(d.CreatedAt, m.watch.at) + " ago")
	}
	return line
}

// taskLines renders a task: state icon, ID, status, health and age, plus
// why it stopped and the exit codes of its containers.
func (m *Model) taskLines(t domain.Task) []string {
	th := m.theme
	mark := "  "
	if m.watch.changed[t.ID] {
		mark = th.Selected.Render("› ")
	}
	icon, style := "●", th.StatusInfo
	status := t.LastStatus
	switch {
	case t.LastStatus == "STOPPED":
		icon, style = "○", th.Muted
	case t.Stopping():
		icon, style = "◑", th.Warning
		status += " → STOPPED"
	case t.LastStatus != "RUNNING":
		icon, style = "◐", th.Warning
	case t.Health == "UNHEALTHY":
		style = th.StatusError
	}
	when := ""
	switch {
	case !t.StoppedAt.IsZero():
		when = "stopped " + since(t.StoppedAt, m.watch.at) + " ago"
	case !t.StartedAt.IsZero():
		when = "up " + since(t.StartedAt, m.watch.at)
	}
	health := t.Health
	if health == "" || health == "UNKNOWN" {
		health = "-"
	}
	lb := ""
	if m.watch.rollout != nil {
		if target, _, ok := m.watch.rollout.TargetOf(t); ok {
			lb = "lb " + targetStyle(m, target.State).Render(target.State)
		}
	}
	line := fmt.Sprintf("  %s%s %s  %s %s %s %s",
		mark, style.Render(icon), t.ID[:min(8, len(t.ID))],
		style.Render(fmt.Sprintf("%-22s", status)), fmt.Sprintf("%-10s", health),
		lipgloss.NewStyle().Width(18).Render(lb), th.Muted.Render(fmt.Sprintf("%-16s %s", when, t.PrivateIP)))
	lines := []string{line}

	if t.StoppedReason != "" && (t.Stopping() || t.LastStatus == "STOPPED") {
		lines = append(lines, th.Muted.Render("        "+t.StoppedReason))
	}
	for _, c := range t.Containers {
		if c.ExitCode != nil && *c.ExitCode != 0 {
			reason := ""
			if c.Reason != "" {
				reason = " (" + c.Reason + ")"
			}
			lines = append(lines, th.StatusError.Render(fmt.Sprintf("        %s exited with code %d%s", c.Name, *c.ExitCode, reason)))
		}
	}
	return lines
}

// diffContent lists what changed from the previous revision to the current one.
func (m *Model) diffContent() string {
	th, w := m.theme, m.watch
	switch {
	case w.diffErr != nil:
		return th.StatusError.Render(w.diffErr.Error())
	case w.diff == nil:
		return th.Muted.Render("Reading the task definitions…")
	}
	d := w.diff
	lines := []string{
		th.Title.Render("Changes "+d.From.Name()+" → "+d.To.Name()) + "  " + th.Muted.Render("R rolls back to "+d.From.Name()),
		"",
	}
	if len(d.Changes) == 0 {
		lines = append(lines, th.Muted.Render("No differences. With a mutable image tag (e.g. latest) the image itself may still differ."))
	}
	width := 0
	for _, c := range d.Changes {
		width = max(width, len(c.Field))
	}
	for _, c := range d.Changes {
		var change string
		switch {
		case c.Old == "":
			change = th.StatusInfo.Render("+ " + c.New)
		case c.New == "":
			change = th.StatusError.Render("- " + c.Old)
		default:
			change = th.StatusError.Render(c.Old) + th.Muted.Render("  →  ") + th.StatusInfo.Render(c.New)
		}
		lines = append(lines, th.Label.Render(fmt.Sprintf("%-*s", width+2, c.Field))+change)
	}
	lines = append(lines, "", th.Muted.Render("Secrets are compared by where they come from, never by value."))
	return strings.Join(lines, "\n")
}

func targetSummary(m *Model, g domain.TargetGroup) string {
	counts := map[string]int{}
	var order []string
	for _, t := range g.Targets {
		if counts[t.State] == 0 {
			order = append(order, t.State)
		}
		counts[t.State]++
	}
	if len(order) == 0 {
		return m.theme.Muted.Render("no targets")
	}
	parts := make([]string, len(order))
	for i, state := range order {
		parts[i] = targetStyle(m, state).Render(fmt.Sprintf("%d %s", counts[state], state))
	}
	return strings.Join(parts, m.theme.Muted.Render(" · "))
}

func targetStyle(m *Model, state string) lipgloss.Style {
	switch state {
	case "healthy":
		return m.theme.StatusInfo
	case "unhealthy":
		return m.theme.StatusError
	case "initial", "draining":
		return m.theme.Warning
	}
	return m.theme.Muted
}

// shortDigest keeps "sha256:" and 12 hex characters, like docker does.
func shortDigest(digest string) string {
	algo, hex, ok := strings.Cut(digest, ":")
	if !ok || len(hex) <= 12 {
		return digest
	}
	return algo + ":" + hex[:12]
}

func taskState(t domain.Task) string {
	return t.LastStatus + "/" + t.DesiredStatus + "/" + t.Health
}

func primaryDeployment(s domain.Service) (domain.Deployment, bool) {
	for _, d := range s.Deployments {
		if d.Status == "PRIMARY" {
			return d, true
		}
	}
	return domain.Deployment{}, false
}

// since renders a short duration, e.g. "45s", "3m10s", "2h5m".
func since(from, to time.Time) string {
	if from.IsZero() || to.IsZero() {
		return "-"
	}
	d := to.Sub(from).Round(time.Second)
	switch {
	case d < 0:
		return "0s"
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm%02ds", int(d.Minutes()), int(d.Seconds())%60)
	}
	return fmt.Sprintf("%dh%02dm", int(d.Hours()), int(d.Minutes())%60)
}
