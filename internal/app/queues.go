package app

import (
	"cmp"
	"context"
	"fmt"
	"slices"

	"infra/internal/domain"
)

// PeekLimit is the most messages one peek returns (an SQS limit).
const PeekLimit = 10

// QueueDeps are the adapters the queue use cases need.
type QueueDeps struct {
	Repo    QueueRepository
	Console QueueConsoleURLBuilder
	Opener  URLOpener
	Metrics QueueMetricsSource
	Policy  Policy
}

// QueueService implements the use cases available for message queues.
type QueueService struct {
	d QueueDeps
}

// NewQueueService wires the queue use cases with their adapters.
func NewQueueService(d QueueDeps) *QueueService { return &QueueService{d: d} }

// List returns the queues sorted by name.
func (s *QueueService) List(ctx context.Context) ([]domain.Queue, error) {
	queues, err := s.d.Repo.ListQueues(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing queues: %w", err)
	}
	slices.SortFunc(queues, func(a, b domain.Queue) int { return cmp.Compare(a.Name, b.Name) })
	return queues, nil
}

// Peek reads up to PeekLimit messages. It counts as a change: each receive
// increments the messages' receive count, which can move them to the dead
// letter queue.
func (s *QueueService) Peek(ctx context.Context, q domain.Queue) ([]domain.Message, error) {
	if err := s.d.Policy.CheckWrite("peek messages of " + q.Name); err != nil {
		return nil, err
	}
	msgs, err := s.d.Repo.PeekMessages(ctx, q, PeekLimit)
	if err != nil {
		return nil, fmt.Errorf("peeking %s: %w", q.Name, err)
	}
	slices.SortFunc(msgs, func(a, b domain.Message) int { return a.SentAt.Compare(b.SentAt) })
	return msgs, nil
}

// Metrics returns the monitoring series of q over w.
func (s *QueueService) Metrics(ctx context.Context, q domain.Queue, w domain.MetricWindow) ([]domain.Series, error) {
	series, err := s.d.Metrics.QueueMetrics(ctx, q, w)
	if err != nil {
		return nil, fmt.Errorf("reading metrics of %s: %w", q.Name, err)
	}
	return series, nil
}

// OpenConsole opens the queue in the web console and returns the URL.
func (s *QueueService) OpenConsole(q domain.Queue) (string, error) {
	return open(s.d.Opener, s.d.Console.QueueURL(q))
}

// open opens url, returning it for messages either way.
func open(opener URLOpener, url string) (string, error) {
	if err := opener.Open(url); err != nil {
		return url, fmt.Errorf("opening console: %w", err)
	}
	return url, nil
}
