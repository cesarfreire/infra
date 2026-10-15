# infra

**English** | [Português](docs/README.pt-BR.md)

A terminal UI for operating AWS infrastructure without leaving the terminal. It lists instances, containers, load balancers, tables, caches and queues, opens shells and tunnels, and shows CloudWatch charts and ECS deployments. Everything works from the keyboard, and mouse clicks work too.

```
 infra   profile: prod ▾   region: sa-east-1 ▾  account 123456789012
╭──────────────────────╮╭──────────────────────────────────────────────────────────────────────────────────╮
│ RESOURCES            ││ Instances 4 found                                                                │
│                      ││ press / to filter                                                                │
│  Favorites           ││  Name                          ID                   State          Type          │
│  Instances           ││ ──────────────────────────────────────────────────────────────────────────────── │
│  Containers          ││  api-prod-1                    i-0a1b2c3d4e5f60718  running        m7g.large     │
│  Load balancers      ││  api-prod-2                    i-0b2c3d4e5f6071829  running        m7g.large     │
│  Key-value DBs       ││  ★ bastion                     i-0c3d4e5f607182930  running        t4g.micro     │
│  Caches              ││  worker-batch                  i-0d4e5f60718293041  stopped        c7g.xlarge    │
│  Queues              ││                                                                                  │
│  Sessions            ││                                                                                  │
╰──────────────────────╯╰──────────────────────────────────────────────────────────────────────────────────╯
 Loaded 4 instances
 enter actions • c connect • p port forward • o console • d details • * favorite • r refresh • / filter …
```

## Features

- **The app never leaves its screen.** SSM shells, ECS Exec and tunnels run in a pseudo-terminal drawn inside the right panel. They keep running in the background in the **Sessions** section, and `ctrl+]` detaches from a terminal.
- **Profiles and regions.** Profiles are read from `~/.aws/config` and `~/.aws/credentials`. `ctrl+p` switches the profile and `ctrl+r` switches the region. Credentials are verified on every switch. If an SSO session has expired, `l` runs `aws sso login` inside the app.
- **Permission alerts.** Missing IAM permissions or invalid credentials open an alert that says what went wrong and what to do.
- **Filtering in every list.** `/` filters by free text or by field (`state:running az:1a tag:env=prod`). Matching ignores case and accents.
- **Monitoring.** Each resource's details view has CloudWatch charts, with time ranges from 1h to 7d, refreshed every minute.
- **Favorites and recents.** `*` marks any resource as a favorite. The **Favorites** section jumps straight to it, with separate lists per profile and region.
- **Safe changes.** Every change asks for confirmation (`y`). Profiles can be made read-only.
- **Update notice.** At startup the app checks GitHub for a newer release in the background (at most once a day). When there is one, the header shows `↑ vX.Y.Z available`; click it to open the release page.

| Section | What it does |
|---|---|
| **Instances** (EC2) | Shell over SSM, port forwarding, start/stop/reboot, details with CPU, network and CPU credit charts |
| **Containers** (ECS) | Clusters › services › tasks. Shell with ECS Exec, port forwarding to containers, forced deployments, desired count changes, rollout and service event tracking, CPU and memory charts |
| **Load balancers** (ALB/NLB) | Load balancers › target groups › targets, with each target's health. Listeners, requests, latency and 5XX errors |
| **Key-value DBs** (DynamoDB) | Paginated scans, key queries, items as JSON, item editing and deletion, consumed capacity and throttling |
| **Caches** (ElastiCache) | Redis, Valkey and memcached, serverless included. `valkey-cli`/`redis-cli` and port forwarding through a jump host. CPU, memory, connections and hit rate |
| **Queues** (SQS) | Backlog, dead-letter queue, message peeking, age of the oldest message and throughput |
| **Sessions** | Open shells and tunnels, with the embedded terminal |

## Requirements

- **Linux or macOS.** On Windows, use WSL: sessions need a Unix pseudo-terminal.
- **[AWS CLI v2](https://docs.aws.amazon.com/cli/latest/userguide/getting-started-install.html)** and the **[Session Manager plugin](https://docs.aws.amazon.com/systems-manager/latest/userguide/session-manager-working-with-install-plugin.html)** on your `PATH`, for shells and tunnels. Everything else uses the SDK directly.
- Credentials configured the same way as for the AWS CLI (profiles, SSO, environment variables).

## Installation

**Prebuilt binary:** download the archive for your system from the repository's *Releases* page (`infra_<version>_<linux|darwin>_<amd64|arm64>.tar.gz`), extract it and put `infra` on your `PATH`.

**From source:** this needs Go 1.27.2. With `GOTOOLCHAIN=auto` (the default), Go downloads that version itself.

```sh
git clone <repository-url> infra && cd infra
make build          # builds bin/infra
./bin/infra --version
```

## Usage

```sh
infra                                   # opens with the last profile/region used
infra --profile prod --region sa-east-1
infra --read-only                       # blocks every change for this run
```

| Flag | Description |
|---|---|
| `--profile` | Initial profile. Defaults to the last one used, then `$AWS_PROFILE` |
| `--region` | Initial region. Defaults to the last one used, then the profile's region. If there is none, the app asks for one |
| `--read-only` | Blocks every change, whatever `settings.json` says |
| `--ssm-shell` | Shell command for instances (`AWS-StartInteractiveCommand` document). The default uses bash if present, else sh. `""` uses the default SSM document |
| `--ecs-shell` | ECS Exec command for containers. The default prefers bash |
| `--no-update-check` | Skips the check for a newer version |
| `--version` | Prints the version |

### Keys

**Global**

| Key | Action |
|---|---|
| `↑`/`↓` or `k`/`j` | Move |
| `enter` / `→` | Open the section or item |
| `esc` | Go back (at the top of a section, back to the menu) |
| `/` | Filter the list (`enter` keeps the filter, `esc` clears it) |
| `*` | Toggle favorite |
| `r` | Reload |
| `ctrl+p` / `ctrl+r` | Switch profile / region (or click the header) |
| `q` / `ctrl+c` | Quit (closes every session) |

**Per section**

| Section | Keys |
|---|---|
| Instances | `enter` actions · `c` shell · `p` port forward · `o` console · `d` details · `S` start · `X` stop · `B` reboot |
| Containers | clusters: `enter` services · `t` all tasks — services: `enter` tasks · `d` details and deployments · `D` force deployment · `N` desired count — tasks: `enter` actions · `c` shell · `p` port forward |
| Load balancers | `enter` target groups › targets · `d` details with listeners |
| Key-value DBs | `enter` items · `f` find by key — items: `enter` view JSON · `n` next page · `e` edit (`ctrl+s` saves) · `x` delete |
| Caches | `c` CLI through a jump host · `p` port forward through a jump host · `d` details |
| Queues | `enter`/`d` details · `m` peek messages |
| Favorites | `enter` go to the resource · `x` remove |
| Sessions | `enter` attach · `x` close · `ctrl+]` detach |
| Details | `↑`/`↓` scroll · `t` time range · `r` refresh the charts |

In confirmation dialogs only `y` confirms. `n`, `esc` or a click cancels.

## Configuration

The files live in `~/.config/infra/` (or your OS's config directory).

- **`settings.json`** is edited by you. To block changes on production profiles:

  ```json
  {
    "readOnlyProfiles": ["prod", "*-prd"]
  }
  ```

  Add `"updateCheck": false` to turn off the check for new versions. Patterns use `path.Match` syntax. Read-only profiles show `read-only` in the header. They block start/stop, deployments, item edits and deletions, and SQS message peeking (which raises the messages' receive count).

- **`state.json`** is written by the app. It holds the last profile and region, the chart time range, the result of the last update check, the jump host chosen for each cache, and favorites and recents (per profile and region). You can delete it at any time.

## AWS prerequisites

- **Instance shells and port forwarding:** instances need the SSM Agent and an instance profile with `AmazonSSMManagedInstanceCore`.
- **ECS Exec:** the service needs `enableExecuteCommand: true` (it applies to tasks started after enabling it), and the task role needs the `ssmmessages:*` permissions.
- **ECS task metrics:** these need Container Insights with *enhanced observability* on the cluster. Service metrics always work.
- **Caches:** caches are reached through a *jump host*: a running instance with the SSM Agent and network access to the cache. For `c`, it also needs `valkey-cli` or `redis-cli` installed.

### IAM permissions

The app shows an alert when a permission is missing, so you only need to grant the ones for the features you use.

| Feature | Actions |
|---|---|
| Reading | `ec2:DescribeInstances`, `ecs:List*`, `ecs:Describe*`, `elasticloadbalancing:Describe*`, `dynamodb:ListTables`, `dynamodb:DescribeTable`, `dynamodb:Scan`, `dynamodb:Query`, `elasticache:Describe*`, `sqs:ListQueues`, `sqs:GetQueueAttributes`, `cloudwatch:GetMetricData` |
| Shells and tunnels | `ssm:StartSession` (documents `AWS-StartInteractiveCommand`, `AWS-StartPortForwardingSession` and `AWS-StartPortForwardingSessionToRemoteHost`), `ssm:TerminateSession`, `ecs:ExecuteCommand` |
| Changes | `ec2:StartInstances`, `ec2:StopInstances`, `ec2:RebootInstances`, `ecs:UpdateService`, `dynamodb:PutItem`, `dynamodb:DeleteItem`, `sqs:ReceiveMessage` |

## Development

```sh
make test       # go test -race ./...
make lint       # golangci-lint (config in .golangci.yml)
make fmt        # gofmt + goimports
make snapshot   # builds every release archive into dist/, without publishing
go test ./internal/tui/sections/instances -run TestPortForwardForm   # a single test
```

golangci-lint must be built with Go ≥ 1.27.2:

```sh
GOTOOLCHAIN=go1.27.2 go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.14.0
```

The project follows clean architecture, with dependencies pointing inward:

```
cmd/infra                 composition root: builds adapters, services and sections per profile/region
internal/domain           entities and errors, with no SDK or UI imports
internal/app              use cases and the ports (interfaces) they depend on
internal/adapter/aws      port implementations with the AWS SDK v2 and the AWS CLI
internal/session          processes in a pseudo-terminal with a VT emulator
internal/prefs            state.json and settings.json
internal/filter           the list filter language
internal/tui              root model, sections and components (Bubble Tea v2)
```

Tests never call AWS. Each adapter takes an interface of the SDK client, and each section depends on interfaces that tests replace with fakes. [`CLAUDE.md`](CLAUDE.md) covers the architecture in detail: the contract between the root model and the sections, how to add a new resource type, and known pitfalls of the libraries.

### Releases

CI (`.github/workflows/ci.yml`) runs tests, lint and `goreleaser check` on every push and pull request. To publish a version, create and push a tag. The `release.yml` workflow then builds the Linux and macOS binaries (amd64 and arm64) and creates the GitHub release. Release builds embed the repository name (`GITHUB_REPOSITORY`), which enables the update check; local builds skip it.

```sh
git tag v0.1.0
git push origin v0.1.0
```
