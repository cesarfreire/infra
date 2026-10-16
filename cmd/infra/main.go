// Command infra is a terminal UI to operate cloud infrastructure.
// It is the composition root: the only place that knows concrete adapters
// (see workspace.go, which rebuilds them when the profile or region changes).
package main

import (
	"cmp"
	"flag"
	"fmt"
	"os"

	tea "charm.land/bubbletea/v2"

	"infra/internal/adapter/aws"
	"infra/internal/adapter/browser"
	"infra/internal/adapter/github"
	"infra/internal/adapter/notify"
	"infra/internal/app"
	"infra/internal/prefs"
	"infra/internal/session"
	"infra/internal/tui"
	"infra/internal/tui/sections/sessions"
	"infra/internal/tui/theme"
)

// Set at build time by GoReleaser (see .goreleaser.yaml). repository
// ("owner/name" on GitHub) enables the check for new versions; local
// builds leave it empty.
var (
	version    = "dev"
	commit     = ""
	date       = ""
	repository = ""
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "infra:", err)
		os.Exit(1)
	}
}

func run() error {
	profile := flag.String("profile", "", "initial AWS profile (default: last used, then $AWS_PROFILE); switch with ctrl+p")
	region := flag.String("region", "", "initial AWS region (default: last used, then the profile's); switch with ctrl+r")
	readOnly := flag.Bool("read-only", false, "block every change, whatever settings.json says")
	ssmShell := flag.String("ssm-shell", aws.DefaultShell,
		"command run by Connect via AWS-StartInteractiveCommand; empty uses the default SSM session document")
	ecsShell := flag.String("ecs-shell", aws.DefaultContainerShell, "command run by ECS Exec when connecting to a container")
	noUpdateCheck := flag.Bool("no-update-check", false, "do not check for a newer version at startup")
	showVersion := flag.Bool("version", false, "print the version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Println("infra", version, commit, date)
		return nil
	}

	store, err := openPrefs()
	if err != nil {
		// Not fatal: the app works without remembering anything.
		fmt.Fprintln(os.Stderr, "infra: preferences not loaded:", err)
	}
	initialProfile, initialRegion := *profile, *region
	if store != nil && initialProfile == "" && initialRegion == "" {
		initialProfile, initialRegion = store.LastContext()
	}
	initialProfile = cmp.Or(initialProfile, os.Getenv("AWS_PROFILE"))

	// Every shell and tunnel runs inside the TUI; none outlives it.
	sessionMgr := session.NewManager()
	defer sessionMgr.CloseAll()

	th := theme.Default()
	opener := browser.New()
	ws := &workspace{
		profiles:        aws.NewProfileStore(),
		prefs:           store,
		shells:          aws.Shells{Instance: *ssmShell, Container: *ecsShell},
		opener:          opener,
		sessions:        sessionMgr,
		sessionsSection: sessions.New(sessionMgr, th),
		theme:           th,
		forceReadOnly:   *readOnly,
	}

	opts := tui.Options{Title: "infra", Workspace: ws, Profile: initialProfile, Region: initialRegion, OpenURL: opener.Open}
	opts.NoNotifications = store != nil && !store.NotificationsEnabled()
	if desktop := notify.New(); desktop.Available() {
		opts.Notify = desktop.Notify
	}
	if repository != "" && !*noUpdateCheck && (store == nil || store.UpdateCheckEnabled()) {
		deps := app.UpdateDeps{Source: github.NewReleases(repository, version), Current: version}
		if store != nil {
			deps.Store = store
		}
		opts.Updates = app.NewUpdateChecker(deps)
	}
	root := tui.New(opts, th)
	_, err = tea.NewProgram(root).Run()
	return err
}

func openPrefs() (*prefs.Store, error) {
	dir, err := prefs.DefaultDir()
	if err != nil {
		return nil, err
	}
	return prefs.Open(dir)
}
