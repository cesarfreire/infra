# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Overview

`infra` is a keyboard-driven terminal UI (mouse clicks also work) for operating cloud infrastructure, built with the Charm v2 libraries (`charm.land/bubbletea/v2`, `bubbles/v2`, `lipgloss/v2`). Layout: a menu on the left (Favorites, Instances, Containers, Load balancers, Key-value DBs, Caches, Queues, Config, Sessions) and the selected section on the right. Only AWS is supported for now. Requires Go 1.27.2 (`go.mod` pins it; `GOTOOLCHAIN=auto` downloads it).

Runtime deps for sessions: the `aws` CLI and `session-manager-plugin` must be on `PATH`.

Hard requirement: **the app never leaves its screen**. Interactive processes (SSM shells, port forwards) run in a pseudo-terminal and are rendered inside the right panel. Never use `tea.ExecProcess` or write to stdout/stderr from child processes. Sessions are Unix-only (creack/pty plus a process-group kill).

## Commands

```sh
make build                       # bin/infra
make run ARGS="--profile dev --region sa-east-1"   # initial context; switch in the app
make test                        # go test -race ./...
make lint                        # golangci-lint run (config: .golangci.yml)
make fmt                         # golangci-lint fmt (gofmt + goimports, local prefix "infra")
make snapshot                    # goreleaser: every release archive into dist/, no publishing
go test ./internal/tui/sections/instances -run TestPortForwardForm   # single test
```

- golangci-lint must be built with Go ≥ 1.27.2: `GOTOOLCHAIN=go1.27.2 go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.14.0`. A binary built with an older Go refuses the config.
- CI (`.github/workflows/ci.yml`) runs tests with `-race`, golangci-lint v2.14.0 and `goreleaser check`. `release.yml` publishes a GitHub release with GoReleaser when a `v*` tag is pushed.
- Releases (`.goreleaser.yaml`) cover Linux and macOS only, because sessions need a Unix pty. `main.version`, `main.commit`, `main.date` and `main.repository` are set via ldflags, and `infra --version` prints the first three. `main.repository` comes from `GITHUB_REPOSITORY` (`envOrDefault` in the template). It enables the update check, so local builds never check. `goreleaser check` fails locally until the repo has a git remote.

## Architecture (clean architecture; dependencies point inward)

- `internal/domain`: provider-agnostic entities and errors. `Bookmark` (a `Kind` plus `ID` and `Parent`) identifies a resource for favorites and recents. `ErrReadOnly` and `ErrInvalidState` guard changes. Account context uses `Profile`, `Region` and `Identity`. Two error sentinels drive the alert dialogs: `ErrAccessDenied` (valid credentials, missing permission) and `ErrUnauthenticated` (credentials missing, invalid or expired). `ErrNoRegion` makes the root ask for a region. Instances use `Instance`. Containers use `Cluster` › `Service` › `Task` › `Container`; `Task.CanExec` holds the exec preconditions. ECS `Service` also carries `Deployments` (newest first) and `Events`, and `RollingOut()` reports a rollout in progress. Key-value DBs use `Table`, `Item` and `ItemPage`. `Item` values are plain Go values: numbers are `domain.Number` (the exact decimal string), and sets keep their type (`StringSet`, `NumberSet`, `BinarySet`, which marshal to JSON as arrays). `Cursor` is an opaque pagination token owned by the adapter. Queues use `Queue` and `Message`. Config uses `Parameter` (`Secure()` for `SecureString`), `Secret` and `SecretValue`. ECS task definitions use `TaskDefinition`/`ContainerDefinition`, and `DiffTaskDefinitions` returns the `Change`s between two revisions (secrets are compared by `valueFrom`, never by value). `Rollout` also carries `TargetGroups` (health), `Definitions` (by `family:revision`) and `Problems`. `TargetOf(task)` matches load balancer targets by private IP, and `Image(deployment)` returns the image and the resolved digest. `ResourceKind.Label()` names each kind. Load balancers use `LoadBalancer`, `Listener`, `TargetGroup` and `Target`. Caches use `Cache` and `Endpoint`. Monitoring uses `Series` (a list of `Point`s plus a `Unit`) over a `MetricWindow` (time range and point period).
  - `PortForward` is shared by every resource. `Target` is the provider's session target. When `RemoteHost` is set, `Target` acts as a jump host and the tunnel goes through it to `RemoteHost:RemotePort`. Imports nothing from the SDK or UI.
- `internal/app`: use cases (`InstanceService`, `ContainerService`, `KeyValueService`, `CacheService`, `QueueService`, `LoadBalancerService`) plus the **ports** they depend on (`ports.go`).
  - Each use case is built from a deps struct, e.g. `app.NewInstanceService(app.InstanceDeps{...})`.
  - **Changes** go through controller ports: `InstanceController` (start/stop/reboot), `ServiceController` (force deployment, desired count) and `ItemWriter` (put/delete item).
  - **`app.Policy{ReadOnly}`** guards every change. `CheckWrite` returns `domain.ErrReadOnly`. Use cases are the authority; the UI only checks too, to fail early.
  - Peeking SQS messages counts as a change, because it raises their receive count.
  - `ContainerService.Rollout` reads the extras in `addExtras`: the target health of every target group, through the optional `TargetHealthReader` port, and the task definition of every deployment. Failures there become `Rollout.Problems`, not errors.
    - `TaskDefinition` caches revisions, which are immutable.
    - `PreviousRevision` picks the rollback target: an older deployment with a different revision, else `family:revision-1`.
    - `Rollback` (`ServiceController.UpdateTaskDefinition`) is checked by the read-only policy.
  - `ConfigService` reads parameters and secrets. Values are read on demand and never cached.
  - Item editing: `ParseItemJSON` and `PrepareItemUpdate` refuse changes to the key attributes, and they give sets and binaries their DynamoDB types back after a JSON edit.
  - Ports are split per resource (interface segregation), with a shared `URLOpener`:

  | Resource  | Ports                                                                                  |
  |-----------|----------------------------------------------------------------------------------------|
  | Instances | `InstanceRepository`, `SessionCommandBuilder`, `ConsoleURLBuilder`                     |
  | Containers| `ContainerRepository`, `ContainerSessionBuilder`, `ContainerConsoleURLBuilder`         |
  | Key-value | `KeyValueRepository`, `KeyValueConsoleURLBuilder`                                      |
  | Caches    | `CacheRepository`, `JumpHostSessionBuilder`, `CacheConsoleURLBuilder`, plus `InstanceRepository` to list jump hosts |
  | Queues    | `QueueRepository`, `QueueConsoleURLBuilder`                                            |
  | Load balancers | `LoadBalancerRepository`, `LoadBalancerConsoleURLBuilder`                         |

  - `ValidateKeyQuery` is exported so the TUI can validate a key query before sending it.
  - `CLICommand` builds the `valkey-cli`/`redis-cli` command line. It refuses endpoints containing shell characters.
  - Caches are reached through a jump host: a running instance with the SSM agent and network access to the cache.
  - Updates (`updates.go`): `UpdateChecker.Check` compares the running version with the `ReleaseSource`'s latest one using `golang.org/x/mod/semver`. `dev` and snapshot builds never report updates. The result is cached for 24h in the global prefs (`update.*` keys); errors are not cached.
  - Monitoring (`metrics.go`):
    - Each resource type has its own metrics port (`InstanceMetricsSource`, `ContainerMetricsSource`, `TableMetricsSource`, `CacheMetricsSource`, `QueueMetricsSource`, `LoadBalancerMetricsSource`). Each use case exposes a `Metrics`/`ServiceMetrics`/`TaskMetrics` method that wraps the port's errors.
    - `MetricWindows` (index `DefaultMetricWindow` is the default) lists the time ranges the charts cycle through. Use cases never run interactive processes: they return an `app.Command` value and the UI decides how to execute it.
- `internal/adapter/aws`: implementations of the ports. EC2 uses the SDK through the `EC2API` interface, so tests pass a fake; every adapter follows this pattern (`ECSAPI`, `DynamoDBAPI`, `SQSAPI`, `ELBAPI`...). SSM shell and port forwarding build `aws ssm start-session` commands, because the interactive part lives in session-manager-plugin, which has no Go API. Connect uses the `AWS-StartInteractiveCommand` document with `--ssm-shell` (default `aws.DefaultShell`: bash -l if present, else sh), because the default SSM document opens `/bin/sh`, which has no tab completion. `--ssm-shell ""` falls back to the default document, for IAM policies that only allow the default one.
  - ECS (`ecs.go`) takes an `ECSAPI` interface. It does List* with paginators, then Describe* in batches (`chunks`: 100 clusters, 10 services, 100 tasks per call). Only running tasks are listed.
  - A container shell is `aws ecs execute-command --interactive --command <--ecs-shell>`. The default, `aws.DefaultContainerShell`, wraps the command in `sh -c` and prefers bash.
  - Container port forward reuses `PortForwardSession` with the SSM target `ecs:<cluster>_<taskID>_<containerRuntimeID>`.
  - DynamoDB (`dynamodb.go`) describes tables in parallel (errgroup, at most 8 at a time). The scan/query `Cursor` is the `LastEvaluatedKey` map. `toValue` converts attribute values without losing precision.
  - ElastiCache (`elasticache.go`) merges replication groups, standalone cache clusters and serverless caches. Members of a replication group are not listed on their own; they only supply the group's engine version.
  - A jump-host port forward uses the `AWS-StartPortForwardingSessionToRemoteHost` document. `RemoteCommandSession` uses `AWS-StartInteractiveCommand`, which is also what the instance shell uses.
  - One `SessionCommandBuilder` and one `ConsoleURLBuilder` implement the ports of every resource.
  - The write operations:
    - EC2: `Start/Stop/RebootInstances` on `InstanceRepository`.
    - ECS: `UpdateService` on `ContainerRepository`.
    - DynamoDB: `PutItem` and `DeleteItem` on `KeyValueRepository`. `PutItem` requires `attribute_exists(pk)`, so an edit never re-creates a deleted item.
  - ECS `GetService` and `toDomainService` map deployments and events, keeping the latest 30 events.
  - SQS (`sqs.go`): it reads attributes in parallel. `PeekMessages` uses `VisibilityTimeout: 1`, because the SDK omits a zero value and the queue default (often 30s) would then hide the messages from consumers.
  - SSM Parameter Store and Secrets Manager (`params.go`): `GetParameter` always decrypts, and listing never returns values.
  - ECS also has `GetTaskDefinition` (`DescribeTaskDefinition`) and `UpdateTaskDefinition`. `toDomainService` keeps the service's target group ARNs, and tasks keep `ImageDigest`, `StartedBy`, the stop details and the container exit codes.
  - ELB v2 (`elb.go`): `TargetHealth(arn)` is shared by the section and the rollout. Target health is read in parallel. Listener default actions are rendered as text.
  - CloudWatch (`cloudwatch.go`): `Metrics` implements every metrics port.
    - Which namespace, dimensions and statistic each chart uses lives in its `metric` specs. One `GetMetricData` call per resource reads them all, and errors go through `classify`.
    - `rate: true` turns a per-period Sum into a per-second value. `ratioOf` plots `100 * name / ratioOf` with a metric-math expression. ECS tasks use it (`CpuUtilized/CpuReserved` from Container Insights), so task charts are empty without enhanced observability.
    - ElastiCache replication groups report per node, so the charts use the first member.
  - `classify` (`errors.go`) wraps every SDK error a repository returns. Permission and credential problems become `domain.ErrAccessDenied`/`ErrUnauthenticated`, with the original error kept in the chain. New repositories must call it too.
  - `ProfileStore` parses `~/.aws/config` and `~/.aws/credentials` (honoring `AWS_CONFIG_FILE` and `AWS_SHARED_CREDENTIALS_FILE`) on every call.
  - `Regions()` is a static list, so the selector works even without valid credentials.
  - `IdentityChecker` calls STS `GetCallerIdentity`. It needs no IAM permission, so any failure means the credentials are unusable.
- `internal/prefs`: what is remembered between runs. It lives in `~/.config/infra` (`os.UserConfigDir`).
  - `state.json` is written by the app (atomically, mode 0600). It holds the last profile and region, global values (the chart time range) and, per profile and region, favorites, recents (at most 20) and values such as the jump host per cache.
  - `settings.json` is edited by the user: `{"readOnlyProfiles": ["prod", "*-prd"]}` (`path.Match` patterns).
  - `Store.Scope(profile, region)` returns a `*Scoped`, which satisfies `section.Bookmarks` and `section.Prefs`.
- `internal/adapter/notify`: desktop notifications. It uses `osascript` on macOS, a Windows toast via `powershell.exe` under WSL (detected with `WSL_DISTRO_NAME`/`WSL_INTEROP`, using the PowerShell AppUserModelID), and `notify-send` elsewhere. `main` sets `Options.Notify` only when `Available()`.
- `internal/adapter/github`: `Releases` implements `app.ReleaseSource` with the anonymous GitHub REST API (`/repos/{repo}/releases/latest`, 5s timeout).
- `internal/adapter/browser`: opens URLs (`wslview` first on Linux for WSL, then `xdg-open`).
- `internal/session`: `Manager` and `Session`. Each session is an `app.Command` running in a PTY (`creack/pty`), with its screen kept in a `charmbracelet/x/vt` emulator.
  - Three goroutines per session:
    - output: pty → emulator
    - input: emulator input pipe → pty
    - wait: waits for the process to exit
  - `Updates()` is a coalescing channel (cap 1) signalling "something changed".
  - `SetSize` resizes all sessions together, since they all share the same panel.
  - `Close` kills the whole process group, so `session-manager-plugin` dies too.
  - `main` calls `CloseAll` on exit.
- `internal/filter`: the generic list-filter language. Terms are combined with AND. A term is free text (matched against every field) or `field:value`. Matching is a case- and accent-insensitive substring search (`filter.Fold`, also used by the picker), e.g. `web state:running tag:env=prod`. Each section maps its entity to `[]filter.Field`. For instances that mapping is `filterFields`.
- `internal/tui`: root Bubble Tea model. It owns the layout, menu, focus (menu vs content), status bar and footer help.
  - `header.go`: the profile and region chips. Rendering and mouse hit-testing share `headerSegments()`.
  - `dialog.go`: the profile/region pickers, the alert and the confirmation, drawn over the screen with a lipgloss `Compositor`. While a dialog is open it gets every key.
    - In a confirmation, only `y` runs the action. `n`/`esc` or any click cancels.
    - In a credentials alert for an SSO profile, `l` runs `Workspace.Login` (`aws sso login` as a session) and re-opens the environment when it ends.
  - `workspace.go`: context switching. The root talks to a `Workspace` interface, and `cmd/infra/workspace.go` implements it.
    - On startup and on every switch, `Workspace.Open(profile, region)` rebuilds the whole object graph: AWS config, services, sections.
    - The root swaps the sections in, then runs `Environment.Verify` (STS) and shows the account or an alert.
    - Each switch increments `seq`, and stale results are dropped.
    - A failed open keeps the current environment.
    - Sections returned again by `Open` (the shared Sessions section) keep their state: they are neither re-initialized nor re-listened. Running sessions keep the profile they were started with.
    - Choosing a profile applies its configured region, or keeps the current one.
    - `Environment.ReadOnly` shows a "read-only" chip in the header.
    - `section.NotifyMsg` (via `section.Notify`) rings the bell. It also shows a desktop notification through `Options.Notify`, or, without a notifier, asks the terminal with OSC 9. `Options.NoNotifications` (`"notifications": false`) turns all of it off.
    - `section.ProgressReporter.Progress()` of the active section becomes `View.ProgressBar`: the OSC 9;4 terminal progress shown by Windows Terminal on its tab and taskbar.
    - `search.go` (`ctrl+k`, not in `ModeRaw`) is the global search, a `dialogSearch` picker over every `section.Searchable`.
      - Opening it initializes the searchable sections that were never opened.
      - While it is open, every broadcast refreshes the results (`picker.SetItems` keeps what was typed and the selection).
      - Choosing a result emits `NavigateMsg` + `RevealMsg`, like Favorites.
    - `Options.Updates` is checked once in `Init`. A newer release adds a right-aligned `↑ vX available` segment to the header (`targetUpdate`), which opens `Options.OpenURL` on click. Failures are silent. It is off with `--no-update-check` or `"updateCheck": false` in `settings.json`.
    - The initial context is `--profile`/`--region`, else the last one used (`state.json`), else `$AWS_PROFILE` and the profile's region.
    - Keys: `ctrl+p` (profile) and `ctrl+r` (region), or a click on the chips. They work in every mode except `ModeRaw`, where the remote shell needs them.
- `internal/tui/section`: the `Section` interface every menu entry implements, plus `StatusMsg`/`Status`/`Error` for writing to the status bar.
  - `Deps` (`deps.go`) is what every section gets besides its use cases: `Theme`, `Bookmarks`, `Prefs` and `ReadOnly`. Helpers: `ToggleFavorite`, `Visit`, `Star` and `BlockWrite`.
  - `MemoryDeps(theme)` backs tests and is the fallback when preferences can't be read.
- `internal/tui/components`: building blocks shared by sections. Use these instead of re-implementing:
  - `listview.Model[T]`: a generic table with heading and count, `/` filter, spinner, error/empty states, and columns that adapt to the width (the first `Column` stretches).
    - `SetColumns` lets the columns depend on the data; DynamoDB items use it.
    - `SetMarker` prefixes the first column (the ★ of favorites); call `Refresh` after toggling a favorite.
    - `Reveal(match)` selects an item and clears a filter that hides it. If the items aren't loaded yet, it waits for `SetItems`.
  - `actionmenu`: a keyboard- and mouse-driven menu. It is used for actions and for pickers.
  - `portform`: the remote/local port form. `ResetFixedRemote` asks only for the local port, for tunnels whose remote port is known.
  - `chart`: block-character bar charts with a Y axis and a time axis.
    - Each cell is drawn in eighths of its height.
    - Points are averaged into one slot per period and spread over the columns. Columns with no data show `·`.
    - Percent charts turn yellow at 75% and red at 90%.
  - `detailpanel`: every details view. It is a `viewport` holding the rendered fields plus a monitoring section with the charts (two columns when there is room).
    - Sections call `Open(header, fetch, emptyHint)` when entering details and `Close()` whenever they leave. Close is what stops the refresh every minute.
    - In a details view, the section offers each key to `HandleKey` first. The panel takes `t` (time range), `r` (refresh) and the scroll keys.
    - Panels have process-unique ids because their messages are broadcast to every section.
    - `WithPrefs` remembers the time range between runs. `SetHeader` replaces the fields while the panel is open; the ECS rollout view uses it.
  - `picker`: a filterable choice list, used by the root's dialogs and by the jump-host selector inside the Caches section.
  - `details.Render`: key/value detail views.
- `internal/tui/sections/<name>`: one package per menu entry. The menu order is set in `cmd/infra/workspace.go`. `placeholder` is a stub for future entries; no menu entry uses it today.
- `cmd/infra`: the composition root and the only place that knows concrete adapters.
  - `main.go` parses the flags: `--profile` and `--region` (the initial context), `--read-only`, `--ssm-shell`, `--ecs-shell` and `--version`. It also opens the prefs store.
  - `workspace.go` builds the services and sections, in menu order, for a given profile and region.
    - It scopes the prefs to that profile and region and computes read-only (flag or `settings.json`).
    - It maps each bookmark kind to the title of the section that owns it, for Favorites.

### Root ↔ section contract (read `internal/tui/section/section.go`)

- Sections are pointer types with mutating methods; `Update` returns the section itself.
- Key, paste and mouse messages go only to the focused section. Mouse coordinates are translated so they are relative to the panel content. **All other messages are broadcast to every section**, so async results (e.g. `instancesLoadedMsg`) arrive even when the section is hidden. Message types should therefore be unexported and section-specific.
- `Init` runs lazily, the first time a section is selected.
- `InputMode()` controls which keys the root may intercept:
  - `ModeNormal`: the root handles `esc` (back to menu), `q` (quit) and `ctrl+c` (quit).
  - `ModeCapture`: for nested views, forms and the filter. The section handles `esc` and `q` itself; `ctrl+c` still quits.
  - `ModeRaw`: for an attached terminal. Every key, including `ctrl+c`, goes to the section. The section exits this mode with `ctrl+]`, and clicking the menu also detaches.
- Optional interfaces:
  - `Listener.Listen()` is called for every section at startup. The Sessions section uses it to watch `Updates()` from launch, even before it is shown.
  - `CursorProvider.Cursor()` returns a cursor relative to the content; the root translates it to screen coordinates.
- Report errors with `section.Error(err)`. It opens an alert dialog for `ErrAccessDenied`/`ErrUnauthenticated` and writes to the status bar otherwise, so sections need no special handling.
- Cross-section flow uses messages, not imports:
  - `section.NavigateMsg{Title}` makes the root select and focus that section.
  - `section.OpenSessionMsg{ID}` is broadcast. The Sessions section attaches to the session and navigates to itself. Instances emits it after starting a shell.
  - `section.ConfirmMsg{Title, Body, Yes}` (via `section.Confirm`) asks the root for a y/n confirmation. `Yes` runs only on `y`. Every change goes through it, after `deps.BlockWrite` has failed fast for read-only profiles.
  - `section.RevealMsg{Bookmark}` is broadcast, in a `tea.Sequence` after `NavigateMsg`, by Favorites. The section owning the kind leaves its nested views and calls `list.Reveal`. For an ECS service it first opens the cluster's services.
- `SetSize` receives the size inside the panel frame.

### Section pattern (Instances is the simple template; Containers adds drill-down)

- A section depends on consumer-side interfaces, not on `*app.XService` directly:
  - `Service` for its use cases.
  - `SessionStarter` (satisfied by `*session.Manager`) for sessions. Tests inject fakes and a recorder.
- Operations on an item are entries in `actions()`/`taskActions()` (`actions.go`). Each entry has a label, a hotkey and a run function. The same list drives the actions menu, the hotkeys in the list and mouse clicks.
- Connect starts a shell session and emits `section.OpenSessionMsg`. Port forward starts a background session and only reports its status.
- Lists:
  - Columns, filter fields and the filter placeholder are declared next to each other (`view.go`/`columns.go`).
  - Read the selection with `list.Selected()`. Rows come from the filtered items.
  - While `list.Filtering()` is true, send every key to the list first.
  - `list.Capturing()` must make the section return `ModeCapture`, so that esc clears the filter.
- Containers specifics:
  - There is one `listview` per level (clusters › services › tasks). `level` tracks the depth, and `esc` goes up (to `tasksParent` from tasks). Only the clusters level returns `ModeNormal`.
  - `t` lists every task of the cluster.
  - Load messages carry the cluster/service they were loaded for. Stale results are ignored.
  - Tasks with more than one running container go through a container picker (`pendingAction`) before connecting or port forwarding.
- Key-value specifics (`sections/keyvalue`):
  - There are two levels: tables, then items.
  - Items come from a paginated scan (`n` loads the next page and appends it) or from a key query (`f`: a partition key plus an optional sort-key prefix).
  - `seq` drops results from an older browse.
  - `enter` on an item shows it as JSON in a `viewport`, with the key attributes first.
  - The item columns are the key attributes plus the most frequent other attributes.
- Caches specifics (`sections/caches`):
  - `c` (CLI) and `p` (port forward) first load the running instances into a jump-host picker. The last host chosen is preselected next time.
  - The port form for a TLS cache explains that clients need `--tls` and must skip hostname verification.

- Favorites and recents: a `*` key in lists calls `deps.ToggleFavorite(bookmark(x))` and then `list.Refresh()`. Opening an item (actions, drill-down, details) calls `deps.Visit`. Each section defines `bookmark(...)` next to its marker.
- Changes (Instances `S`/`X`/`B`, Containers `D`/`N`, Key-value `e`/`x`): a result message (`changedMsg`/`itemChangedMsg`) reports the outcome, and the section reloads or patches its list.
- Containers service details re-read the service every 10s (`serviceTickMsg`/`serviceReadMsg`, guarded by `svcSeq`) to follow deployments. `leaveView` stops it.
- Deployment view (`sections/containers/watch.go`, `viewWatch`). It opens with `w` on a service and automatically after a confirmed force deploy (`changedMsg.deployed`).
  - Every 3s it reads `ContainerService.Rollout`: `GetService` and `ListDeploymentTasks` in parallel. The tasks are both RUNNING and STOPPED desired status, with duplicate ARNs removed, and stopped tasks older than `RolloutStoppedWindow` are dropped.
  - Tasks are grouped under their deployment by `Task.StartedBy == Deployment.ID`; the rest go under "Earlier deployments".
  - `watchState.seq` guards the loop. Each result schedules the next tick, and pause, refresh now and leave bump `seq`, so only one loop ever runs.
  - Tasks whose `LastStatus/DesiredStatus/Health` changed since the previous snapshot are marked `›`.
  - The PRIMARY deployment drives the outcome. `FAILED` raises an error; the rollout leaving `RollingOut()` reports "completed in …". Both also send `section.Notify`.
  - The view shows, for each target group, a health summary, the `lb <state>` of each task, the image and digest of each deployment, and `Rollout.Problems`.
  - `v` toggles a diff with the previous revision (`watchState.showDiff`), and `esc` closes the diff before leaving the view. `R` loads `PreviousRevision` and confirms a rollback that lists the image changes; on success, `changedMsg.deployed` re-opens the view.
  - `Progress()` reports the primary deployment's `running/desired` as a percentage, an error state when the rollout failed, and nothing once it is steady.
- Config specifics (`sections/config`): one section with two `listview`s (`tab` switches parameters/secrets; secrets load on first use).
  - Details render into a `viewport`. `String`/`StringList` values load when opened.
  - `SecureString` values and secrets stay masked until `v`, which asks for confirmation.
  - `revealMsg`/`valueMsg` carry `seq`, so a confirmation or a read for details already left is dropped.
  - `closeDetails` forgets the value.
- Every resource section implements `section.Searchable` in its `search.go`, offering what its lists have loaded (for ECS, the clusters plus the services of the cluster opened last).

### Adding a new resource type

1. Add an entity to `domain`.
2. Add a use case and ports to `app`.
3. Add an adapter under `adapter/aws`.
4. Add a section package under `tui/sections`.
5. Register the section in `cmd/infra/workspace.go`. If it supports favorites, add a `domain.ResourceKind`, handle `RevealMsg`, and map the kind in the `owners` map.

## Gotchas

- Lipgloss v2 `Style.Width`/`Height` **include** border and padding. Use `GetFrameSize` to compute inner sizes.
- `bubbles/v2/table` sets the cursor to -1 when it receives empty rows and never restores it. `listview.refreshTable()` keeps the cursor. The table also ignores keys unless it is focused, so tests must call `Focus()`.
- `vt` v0.1.0: `Emulator.Close` races with the blocked `Read`, so never call it. Instead, end the input pump by closing `InputPipe().(*io.PipeWriter)`.
- `os.File.Fd()` (used by `pty.Setsize`) races with `Close`. Both go through `Session.mu` and the `ptyClosed` flag.
- `tea.KeyPressMsg` converts directly to `vt.KeyPressEvent`, because they share the same underlying `uv.Key`.
- `session` and `sessions` tests run real processes (`cat`, `sh`) in a real PTY.
- Test helpers that run commands must time-box them (as `run` in the `containers`, `keyvalue` and `caches` tests does). Otherwise a `tea.Tick` (the panel's refresh every minute) blocks the test.
- Section tests run the returned commands and feed load messages back. Send the key that triggers a load in its own `send` call before any key that depends on the loaded data.
- Running a textinput `Focus()` command in tests sleeps for one blink interval. The `run` helper in `internal/tui/workspace_test.go` drops commands that take longer than 100ms.
- `tea.Sequence` returns an unexported message type in v2. The favorites test unpacks it by reflection.
- Section test helpers must hand `section.*Msg` messages (status, alert, confirm, navigate) back to the test instead of feeding them to the model. A confirmation is executed by calling `confirm.Yes()` and sending its message.
- Tests drive models by sending `tea.KeyPressMsg{Code: ..., Text: ...}` and executing the returned commands. Never call AWS in tests.
