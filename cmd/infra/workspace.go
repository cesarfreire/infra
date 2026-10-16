package main

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/service/cloudwatch"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ecs"
	"github.com/aws/aws-sdk-go-v2/service/elasticache"
	elb "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	"github.com/aws/aws-sdk-go-v2/service/sts"

	"infra/internal/adapter/aws"
	"infra/internal/app"
	"infra/internal/domain"
	"infra/internal/prefs"
	"infra/internal/session"
	"infra/internal/tui"
	"infra/internal/tui/section"
	"infra/internal/tui/sections/caches"
	"infra/internal/tui/sections/config"
	"infra/internal/tui/sections/containers"
	"infra/internal/tui/sections/favorites"
	"infra/internal/tui/sections/instances"
	"infra/internal/tui/sections/keyvalue"
	"infra/internal/tui/sections/loadbalancers"
	"infra/internal/tui/sections/queues"
	"infra/internal/tui/theme"
)

// workspace builds the object graph for a profile and region. It
// implements tui.Workspace; the root calls Open on every switch.
type workspace struct {
	profiles *aws.ProfileStore
	prefs    *prefs.Store // nil when the preferences could not be read
	shells   aws.Shells
	opener   app.URLOpener
	sessions *session.Manager
	// sessionsSection is shared by every environment: sessions keep
	// running (with the profile they were started with) across switches.
	sessionsSection section.Section
	theme           theme.Theme
	forceReadOnly   bool // --read-only
}

func (w *workspace) Profiles() ([]domain.Profile, error) { return w.profiles.List() }

func (w *workspace) Regions() []domain.Region { return aws.Regions() }

// Login runs `aws sso login` as a session.
func (w *workspace) Login(profile string) (tui.Login, error) {
	args := []string{"sso", "login"}
	if profile != "" {
		args = append(args, "--profile", profile)
	}
	title := "aws sso login"
	if profile != "" {
		title += " · " + profile
	}
	s, err := w.sessions.Start(session.KindShell, title, app.Command{Name: "aws", Args: args})
	if err != nil {
		return tui.Login{}, err
	}
	return tui.Login{SessionID: s.ID, Wait: func() error { <-s.Done(); return s.Err() }}, nil
}

func (w *workspace) Open(profile, region string) (tui.Environment, error) {
	cfg, err := aws.LoadConfig(context.Background(), profile, region)
	if err != nil {
		return tui.Environment{}, fmt.Errorf("loading profile %q: %w", profile, err)
	}
	if cfg.Region == "" {
		return tui.Environment{}, domain.ErrNoRegion
	}

	readOnly := w.forceReadOnly || (w.prefs != nil && w.prefs.ReadOnly(profile))
	policy := app.Policy{ReadOnly: readOnly}
	deps := section.MemoryDeps(w.theme)
	if w.prefs != nil {
		scoped := w.prefs.Scope(profile, cfg.Region)
		deps.Bookmarks, deps.Prefs = scoped, scoped
		_ = w.prefs.SetLastContext(profile, cfg.Region) // best effort: only the next start uses it
	}
	deps.ReadOnly = readOnly

	sessionCmds := aws.NewSessionCommandBuilder(profile, cfg.Region, w.shells)
	consoleURLs := aws.NewConsoleURLBuilder(cfg.Region)
	metrics := aws.NewMetrics(cloudwatch.NewFromConfig(cfg))
	instanceRepo := aws.NewInstanceRepository(ec2.NewFromConfig(cfg))
	containerRepo := aws.NewContainerRepository(ecs.NewFromConfig(cfg))
	kvRepo := aws.NewKeyValueRepository(dynamodb.NewFromConfig(cfg))
	lbRepo := aws.NewLoadBalancerRepository(elb.NewFromConfig(cfg))
	identity := aws.NewIdentityChecker(sts.NewFromConfig(cfg))

	instanceSvc := app.NewInstanceService(app.InstanceDeps{Repo: instanceRepo, Control: instanceRepo,
		Sessions: sessionCmds, Console: consoleURLs, Opener: w.opener, Metrics: metrics, Policy: policy})
	containerSvc := app.NewContainerService(app.ContainerDeps{Repo: containerRepo, Control: containerRepo,
		Sessions: sessionCmds, Console: consoleURLs, Opener: w.opener, Metrics: metrics, Policy: policy, Targets: lbRepo})
	keyValueSvc := app.NewKeyValueService(app.KeyValueDeps{Repo: kvRepo, Writer: kvRepo,
		Console: consoleURLs, Opener: w.opener, Metrics: metrics, Policy: policy})
	cacheSvc := app.NewCacheService(app.CacheDeps{Repo: aws.NewCacheRepository(elasticache.NewFromConfig(cfg)),
		Instances: instanceRepo, Sessions: sessionCmds, Console: consoleURLs, Opener: w.opener, Metrics: metrics})
	queueSvc := app.NewQueueService(app.QueueDeps{Repo: aws.NewQueueRepository(sqs.NewFromConfig(cfg)),
		Console: consoleURLs, Opener: w.opener, Metrics: metrics, Policy: policy})
	lbSvc := app.NewLoadBalancerService(app.LoadBalancerDeps{Repo: lbRepo,
		Console: consoleURLs, Opener: w.opener, Metrics: metrics})

	configSvc := app.NewConfigService(app.ConfigDeps{
		Parameters: aws.NewParameterRepository(ssm.NewFromConfig(cfg)),
		Secrets:    aws.NewSecretRepository(secretsmanager.NewFromConfig(cfg)),
		Console:    consoleURLs, Opener: w.opener})

	containersSection := containers.New(containerSvc, w.sessions, deps)
	owners := map[domain.ResourceKind]string{
		domain.KindInstance:     "Instances",
		domain.KindECSCluster:   containersSection.Title(),
		domain.KindECSService:   containersSection.Title(),
		domain.KindTable:        keyvalue.Title,
		domain.KindCache:        caches.Title,
		domain.KindQueue:        queues.Title,
		domain.KindLoadBalancer: loadbalancers.Title,
		domain.KindParameter:    config.Title,
		domain.KindSecret:       config.Title,
	}
	return tui.Environment{
		Profile: profile,
		Region:  cfg.Region,
		Sections: []section.Section{
			favorites.New(deps, owners),
			instances.New(instanceSvc, w.sessions, deps),
			containersSection,
			loadbalancers.New(lbSvc, deps),
			keyvalue.New(keyValueSvc, deps),
			caches.New(cacheSvc, w.sessions, deps),
			queues.New(queueSvc, deps),
			config.New(configSvc, deps),
			w.sessionsSection,
		},
		Verify:   identity.CallerIdentity,
		ReadOnly: readOnly,
	}, nil
}
