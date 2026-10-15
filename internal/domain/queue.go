package domain

import "time"

// Queue is a message queue (an SQS queue).
type Queue struct {
	Name              string
	URL               string
	ARN               string
	FIFO              bool
	Visible           int // approximate messages available
	InFlight          int // received but not deleted yet
	Delayed           int
	Retention         time.Duration
	VisibilityTimeout time.Duration
	Encrypted         bool
	CreatedAt         time.Time
	// DeadLetterQueue is the name of the queue failed messages move to after
	// MaxReceiveCount receives; empty when there is none.
	DeadLetterQueue string
	MaxReceiveCount int
}

// Message is a queue message, as peeked.
type Message struct {
	ID           string
	Body         string
	SentAt       time.Time
	ReceiveCount int
	GroupID      string // FIFO queues
	Attributes   map[string]string
}
