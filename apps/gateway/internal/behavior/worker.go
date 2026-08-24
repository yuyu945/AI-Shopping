package behavior

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/segmentio/kafka-go"
)

type Repository interface {
	Claim(context.Context, int, string) ([]LeasedEvent, error)
	MarkPublished(context.Context, uint64, string) error
	MarkRetry(context.Context, uint64, string) error
}

type Publisher interface {
	Publish(context.Context, kafka.Message) error
}

type KafkaPublisher struct {
	writer *kafka.Writer
}

func NewKafkaPublisher(brokers []string) *KafkaPublisher {
	return &KafkaPublisher{writer: &kafka.Writer{Addr: kafka.TCP(brokers...), RequiredAcks: kafka.RequireAll, Async: false}}
}

func (p *KafkaPublisher) Publish(ctx context.Context, message kafka.Message) error {
	return p.writer.WriteMessages(ctx, message)
}

func (p *KafkaPublisher) Close() error {
	if p == nil || p.writer == nil {
		return nil
	}
	return p.writer.Close()
}

type Config struct {
	BatchSize     int
	LeaseDuration time.Duration
	CallTimeout   time.Duration
	Now           func() time.Time
	ClaimToken    func() string
}

type Worker struct {
	repository Repository
	publisher  Publisher
	config     Config
}

func NewWorker(repository Repository, publisher Publisher, config Config) *Worker {
	if config.BatchSize <= 0 {
		config.BatchSize = 20
	}
	if config.CallTimeout <= 0 {
		config.CallTimeout = time.Second
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	if config.ClaimToken == nil {
		config.ClaimToken = uuid.NewString
	}
	return &Worker{repository: repository, publisher: publisher, config: config}
}

func (w *Worker) Run(ctx context.Context, interval time.Duration) error {
	if interval <= 0 {
		interval = time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if err := w.RunOnce(ctx); err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func (w *Worker) RunOnce(ctx context.Context) error {
	claimToken := w.config.ClaimToken()
	events, err := w.repository.Claim(ctx, w.config.BatchSize, claimToken)
	if err != nil {
		return err
	}
	for _, event := range events {
		callCtx, cancel := context.WithTimeout(ctx, w.config.CallTimeout)
		err := w.publisher.Publish(callCtx, kafka.Message{Topic: event.Topic, Key: []byte(event.Key), Value: append([]byte(nil), event.Payload...)})
		cancel()
		if err != nil {
			_ = w.repository.MarkRetry(ctx, event.ID, "publish behavior event failed")
			continue
		}
		if err := w.repository.MarkPublished(ctx, event.ID, claimToken); err != nil {
			return err
		}
	}
	return nil
}
