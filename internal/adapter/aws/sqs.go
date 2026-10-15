package aws

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	sdkaws "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/aws/aws-sdk-go-v2/service/sqs/types"
	"golang.org/x/sync/errgroup"

	"infra/internal/domain"
)

// peekVisibility hides peeked messages from consumers for this many
// seconds. It cannot be 0: the SDK omits a zero VisibilityTimeout, and the
// queue default (often 30s) would apply instead.
const peekVisibility = 1

// SQSAPI is the subset of *sqs.Client used.
type SQSAPI interface {
	sqs.ListQueuesAPIClient
	GetQueueAttributes(ctx context.Context, in *sqs.GetQueueAttributesInput, optFns ...func(*sqs.Options)) (*sqs.GetQueueAttributesOutput, error)
	ReceiveMessage(ctx context.Context, in *sqs.ReceiveMessageInput, optFns ...func(*sqs.Options)) (*sqs.ReceiveMessageOutput, error)
}

// QueueRepository reads SQS queues. It implements app.QueueRepository.
type QueueRepository struct {
	client SQSAPI
}

// NewQueueRepository returns a repository backed by client.
func NewQueueRepository(client SQSAPI) *QueueRepository {
	return &QueueRepository{client: client}
}

// ListQueues returns every queue with its attributes, read in parallel.
func (r *QueueRepository) ListQueues(ctx context.Context) ([]domain.Queue, error) {
	var urls []string
	pages := sqs.NewListQueuesPaginator(r.client, &sqs.ListQueuesInput{})
	for pages.HasMorePages() {
		page, err := pages.NextPage(ctx)
		if err != nil {
			return nil, classify(err)
		}
		urls = append(urls, page.QueueUrls...)
	}

	queues := make([]domain.Queue, len(urls))
	g, ctx := errgroup.WithContext(ctx)
	g.SetLimit(describeConcurrency)
	for i, url := range urls {
		g.Go(func() error {
			out, err := r.client.GetQueueAttributes(ctx, &sqs.GetQueueAttributesInput{
				QueueUrl:       &url,
				AttributeNames: []types.QueueAttributeName{types.QueueAttributeNameAll},
			})
			if err != nil {
				return classify(err)
			}
			queues[i] = toDomainQueue(url, out.Attributes)
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return nil, err
	}
	return queues, nil
}

// PeekMessages receives up to max messages without deleting them.
func (r *QueueRepository) PeekMessages(ctx context.Context, q domain.Queue, max int) ([]domain.Message, error) {
	out, err := r.client.ReceiveMessage(ctx, &sqs.ReceiveMessageInput{
		QueueUrl:                    &q.URL,
		MaxNumberOfMessages:         int32(max),
		VisibilityTimeout:           peekVisibility,
		WaitTimeSeconds:             1,
		MessageSystemAttributeNames: []types.MessageSystemAttributeName{types.MessageSystemAttributeNameAll},
		MessageAttributeNames:       []string{"All"},
	})
	if err != nil {
		return nil, classify(err)
	}
	msgs := make([]domain.Message, len(out.Messages))
	for i, m := range out.Messages {
		msgs[i] = domain.Message{
			ID:           sdkaws.ToString(m.MessageId),
			Body:         sdkaws.ToString(m.Body),
			SentAt:       epochMillis(m.Attributes["SentTimestamp"]),
			ReceiveCount: atoi(m.Attributes["ApproximateReceiveCount"]),
			GroupID:      m.Attributes["MessageGroupId"],
			Attributes:   map[string]string{},
		}
		for name, v := range m.MessageAttributes {
			msgs[i].Attributes[name] = sdkaws.ToString(v.StringValue)
		}
	}
	return msgs, nil
}

func toDomainQueue(url string, attrs map[string]string) domain.Queue {
	q := domain.Queue{
		Name:              lastSegment(url),
		URL:               url,
		ARN:               attrs["QueueArn"],
		FIFO:              attrs["FifoQueue"] == "true",
		Visible:           atoi(attrs["ApproximateNumberOfMessages"]),
		InFlight:          atoi(attrs["ApproximateNumberOfMessagesNotVisible"]),
		Delayed:           atoi(attrs["ApproximateNumberOfMessagesDelayed"]),
		Retention:         time.Duration(atoi(attrs["MessageRetentionPeriod"])) * time.Second,
		VisibilityTimeout: time.Duration(atoi(attrs["VisibilityTimeout"])) * time.Second,
		Encrypted:         attrs["SqsManagedSseEnabled"] == "true" || attrs["KmsMasterKeyId"] != "",
	}
	if ts := atoi(attrs["CreatedTimestamp"]); ts > 0 {
		q.CreatedAt = time.Unix(int64(ts), 0)
	}
	if policy := attrs["RedrivePolicy"]; policy != "" {
		var rp struct {
			Target   string          `json:"deadLetterTargetArn"`
			MaxCount json.RawMessage `json:"maxReceiveCount"` // a number or a string
		}
		if json.Unmarshal([]byte(policy), &rp) == nil {
			q.DeadLetterQueue = arnResource(rp.Target)
			q.MaxReceiveCount = atoi(strings.Trim(string(rp.MaxCount), `"`))
		}
	}
	return q
}

// arnResource returns the resource of a colon-separated ARN (the queue name).
func arnResource(arn string) string {
	if i := strings.LastIndex(arn, ":"); i >= 0 {
		return arn[i+1:]
	}
	return arn
}

func atoi(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}

func epochMillis(s string) time.Time {
	ms, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return time.Time{}
	}
	return time.UnixMilli(ms)
}
