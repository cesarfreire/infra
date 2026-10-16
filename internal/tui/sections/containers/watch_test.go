package containers

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"infra/internal/domain"
	"infra/internal/tui/section"
)

var (
	t0      = time.Now().Add(-5 * time.Minute)
	exit137 = 137
)

// rollingOut: the new deployment has one task running and one starting;
// the old one has one task draining and one already stopped by OOM.
func rollingOut() domain.Rollout {
	return domain.Rollout{
		Service: domain.Service{Name: "api", Cluster: "prod", Desired: 2, Running: 3, Pending: 1,
			Deployments: []domain.Deployment{
				{ID: "ecs-svc/new", Status: "PRIMARY", RolloutState: "IN_PROGRESS", TaskDefinition: "api:8", Desired: 2, Running: 1, Pending: 1, CreatedAt: t0},
				{ID: "ecs-svc/old", Status: "ACTIVE", RolloutState: "COMPLETED", TaskDefinition: "api:7", Desired: 0, Running: 2},
			},
			Events: []domain.ServiceEvent{{Time: t0, Message: "(service api) has started 2 tasks"}},
		},
		Tasks: []domain.Task{
			{ID: "new11111aaaa", StartedBy: "ecs-svc/new", LastStatus: "RUNNING", DesiredStatus: "RUNNING", Health: "HEALTHY", StartedAt: t0},
			{ID: "new22222bbbb", StartedBy: "ecs-svc/new", LastStatus: "PROVISIONING", DesiredStatus: "RUNNING"},
			{ID: "old11111cccc", StartedBy: "ecs-svc/old", LastStatus: "RUNNING", DesiredStatus: "STOPPED",
				StoppedReason: "Scaling activity initiated by (deployment ecs-svc/new)"},
			{ID: "old22222dddd", StartedBy: "ecs-svc/old", LastStatus: "STOPPED", DesiredStatus: "STOPPED", StoppedAt: t0,
				StoppedReason: "Essential container in task exited",
				Containers:    []domain.Container{{Name: "app", ExitCode: &exit137, Reason: "OutOfMemoryError"}}},
		},
	}
}

// completed: only the new deployment is left, with both tasks running.
func completed() domain.Rollout {
	r := rollingOut()
	r.Service.Deployments = []domain.Deployment{{ID: "ecs-svc/new", Status: "PRIMARY", RolloutState: "COMPLETED",
		TaskDefinition: "api:8", Desired: 2, Running: 2, CreatedAt: t0}}
	r.Tasks = []domain.Task{
		r.Tasks[0],
		{ID: "new22222bbbb", StartedBy: "ecs-svc/new", LastStatus: "RUNNING", DesiredStatus: "RUNNING", Health: "HEALTHY", StartedAt: time.Now()},
	}
	return r
}

func openWatchOn(t *testing.T, rollouts ...domain.Rollout) (*Model, *fakeService) {
	t.Helper()
	m, svc, _ := newModel(t)
	svc.rollouts = rollouts
	m.SetSize(120, 50)
	send(m, press("enter")) // services of prod
	send(m, typeRune('w'))
	if m.view != viewWatch {
		t.Fatalf("w should open the deployment view, view = %v", m.view)
	}
	return m, svc
}

func TestWatchGroupsTasksByDeployment(t *testing.T) {
	m, _ := openWatchOn(t, rollingOut())
	view := ansi.Strip(m.View())
	for _, want := range []string{
		"Deployment · api", "rolling out",
		"PRIMARY", "api:8", "1/2 running", "1 pending",
		"new11111", "HEALTHY", "new22222", "PROVISIONING",
		"api:7", "old11111", "RUNNING → STOPPED", "Scaling activity",
		"old22222", "app exited with code 137 (OutOfMemoryError)",
		"has started 2 tasks",
	} {
		if !strings.Contains(view, want) {
			t.Errorf("view missing %q:\n%s", want, view)
		}
	}
	// The new deployment's tasks come before the old ones.
	if strings.Index(view, "new22222") > strings.Index(view, "old11111") {
		t.Error("tasks should be grouped under their deployment")
	}
}

func TestWatchRefreshMarksChangesAndCompletion(t *testing.T) {
	m, svc := openWatchOn(t, rollingOut(), completed())
	out := send(m, watchTickMsg{seq: m.watch.seq})
	if svc.rolloutCalls != 2 {
		t.Fatalf("a tick should read again; calls = %d", svc.rolloutCalls)
	}
	view := ansi.Strip(m.View())
	if !strings.Contains(view, "› ● new22222") {
		t.Errorf("the task that changed should be marked:\n%s", view)
	}
	if strings.Contains(view, "› ● new11111") {
		t.Error("unchanged tasks must not be marked")
	}
	if !strings.Contains(view, "Deployment completed in") || !strings.Contains(view, "steady") {
		t.Errorf("completion should be announced:\n%s", view)
	}
	var announced bool
	for _, msg := range out {
		if st, ok := msg.(section.StatusMsg); ok && strings.Contains(st.Text, "completed") {
			announced = true
		}
	}
	if !announced {
		t.Errorf("a status message should announce the completion: %v", out)
	}
}

func TestWatchShowsFailure(t *testing.T) {
	failed := rollingOut()
	failed.Service.Deployments[0].RolloutState = "FAILED"
	failed.Service.Deployments[0].RolloutReason = "ECS deployment circuit breaker: tasks failed to start."
	m, _ := openWatchOn(t, failed)
	if view := ansi.Strip(m.View()); !strings.Contains(view, "Deployment failed: ECS deployment circuit breaker") {
		t.Errorf("view:\n%s", view)
	}
}

func TestWatchPauseStopsRefreshing(t *testing.T) {
	m, svc := openWatchOn(t, rollingOut())
	seq := m.watch.seq
	send(m, typeRune('p'))
	send(m, watchTickMsg{seq: seq})
	send(m, watchTickMsg{seq: m.watch.seq})
	if svc.rolloutCalls != 1 {
		t.Errorf("paused: calls = %d, want 1", svc.rolloutCalls)
	}
	if !strings.Contains(ansi.Strip(m.View()), "paused") {
		t.Error("the header should say paused")
	}
	send(m, typeRune('p'))
	if svc.rolloutCalls != 2 {
		t.Errorf("resuming should read right away; calls = %d", svc.rolloutCalls)
	}
}

func TestWatchIgnoresStaleResultsAndStopsOnLeave(t *testing.T) {
	m, svc := openWatchOn(t, rollingOut())
	seq := m.watch.seq
	stale := completed()
	send(m, rolloutMsg{seq: seq - 1, rollout: stale, at: time.Now()})
	if strings.Contains(ansi.Strip(m.View()), "completed in") {
		t.Error("results of an older loop must be ignored")
	}
	send(m, press("esc"))
	send(m, watchTickMsg{seq: seq})
	if m.view != viewList || svc.rolloutCalls != 1 {
		t.Errorf("leaving must stop the loop; view %v, calls %d", m.view, svc.rolloutCalls)
	}
}

func TestDeployOpensWatch(t *testing.T) {
	m, svc, _ := newModel(t)
	svc.rollouts = []domain.Rollout{rollingOut()}
	send(m, press("enter"))
	c := confirmIn(t, asAny(send(m, press("D"))))
	send(m, c.Yes())
	if m.view != viewWatch || svc.rolloutCalls == 0 {
		t.Errorf("after a deploy the deployment view should open; view %v", m.view)
	}
}

func TestSince(t *testing.T) {
	base := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	for d, want := range map[time.Duration]string{
		45 * time.Second:               "45s",
		3*time.Minute + 10*time.Second: "3m10s",
		2*time.Hour + 5*time.Minute:    "2h05m",
	} {
		if got := since(base, base.Add(d)); got != want {
			t.Errorf("since(%v) = %q, want %q", d, got, want)
		}
	}
}

// withExtras adds load balancer health and task definitions to a snapshot.
func withExtras(r domain.Rollout) domain.Rollout {
	r.Tasks[0].PrivateIP = "10.0.1.5"
	r.Tasks[0].Containers = []domain.Container{{Name: "app", ImageDigest: "sha256:0123456789abcdef0123"}}
	r.Tasks[1].PrivateIP = "10.0.1.6"
	r.TargetGroups = []domain.TargetGroup{{Name: "api-tg", Targets: []domain.Target{
		{ID: "10.0.1.5", State: "healthy"}, {ID: "10.0.1.6", State: "initial"},
	}}}
	r.Definitions = map[string]domain.TaskDefinition{
		"api:8": {Family: "api", Revision: 8, Containers: []domain.ContainerDefinition{{Name: "app", Image: "repo/api:v42"}}},
	}
	r.Problems = []string{"task definition api:7: AccessDenied"}
	return r
}

func TestWatchShowsLoadBalancerAndImage(t *testing.T) {
	m, _ := openWatchOn(t, withExtras(rollingOut()))
	view := ansi.Strip(m.View())
	for _, want := range []string{
		"load balancer api-tg  1 healthy · 1 initial",
		"image repo/api:v42 @sha256:0123456789ab",
		"lb healthy", "lb initial",
		"⚠ task definition api:7: AccessDenied",
	} {
		if !strings.Contains(view, want) {
			t.Errorf("view missing %q:\n%s", want, view)
		}
	}
}

func TestWatchDiffAndRollback(t *testing.T) {
	m, svc := openWatchOn(t, rollingOut())
	send(m, typeRune('v'))
	view := ansi.Strip(m.View())
	if !strings.Contains(view, "Changes api:7 → api:8") || !strings.Contains(view, "repo/api:v41  →  repo/api:v42") {
		t.Fatalf("v should show the diff:\n%s", view)
	}
	send(m, press("esc"))
	if m.view != viewWatch || m.watch.showDiff {
		t.Fatal("esc should close the diff, not leave the deployment view")
	}

	c := confirmIn(t, asAny(send(m, typeRune('R'))))
	if !strings.Contains(c.Body, "Deploy api:7") || !strings.Contains(c.Body, "from repo/api:v42 to repo/api:v41") {
		t.Errorf("body = %q", c.Body)
	}
	send(m, c.Yes())
	if len(svc.changes) != 1 || svc.changes[0] != "rollback api to api:7" || m.view != viewWatch {
		t.Errorf("changes = %v, view = %v", svc.changes, m.view)
	}
}

func TestWatchNotifiesAndReportsProgress(t *testing.T) {
	m, _ := openWatchOn(t, rollingOut(), completed())
	if p := m.Progress(); p == nil || p.Value != 50 {
		t.Errorf("progress while rolling out = %+v, want 50%% (1 of 2)", p)
	}
	out := send(m, watchTickMsg{seq: m.watch.seq})
	var notified bool
	for _, msg := range out {
		if n, ok := msg.(section.NotifyMsg); ok && strings.Contains(n.Title, "completed") {
			notified = true
		}
	}
	if !notified {
		t.Errorf("completion should notify: %v", out)
	}
	if m.Progress() != nil {
		t.Error("a steady service shows no progress")
	}
}

func TestReadOnlyBlocksRollback(t *testing.T) {
	m, svc := openWatchOn(t, rollingOut())
	m.deps.ReadOnly = true
	out := send(m, typeRune('R'))
	for _, msg := range out {
		if _, ok := msg.(section.ConfirmMsg); ok {
			t.Fatal("no confirmation when read-only")
		}
	}
	if len(svc.changes) != 0 {
		t.Error("nothing should change")
	}
}

func TestSearchItemsClustersAndServices(t *testing.T) {
	m, _, _ := newModel(t)
	send(m, press("enter")) // loads the services of prod
	var kinds []string
	for _, it := range m.SearchItems() {
		kinds = append(kinds, string(it.Bookmark.Kind)+":"+it.Bookmark.Label)
	}
	got := strings.Join(kinds, ",")
	if got != "ecs-cluster:prod,ecs-cluster:staging,ecs-service:prod/api,ecs-service:prod/web" {
		t.Errorf("items = %s", got)
	}
}
