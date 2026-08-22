package catalog

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/segmentio/kafka-go"
)

const inventoryConfirmationConsumerGroup = "product-inventory-confirmation"
const inventoryConfirmationDeadLetterTopic = "inventory.reservation.confirm.deadletter"

const (
	confirmationRetryInitial     = 10 * time.Millisecond
	confirmationRetryMaximum     = 100 * time.Millisecond
	confirmationRetryMaxAttempts = 5
)

// ConfirmationEvent is the payload published after a wallet payment commits.
type ConfirmationEvent struct {
	EventID          string `json:"event_id"`
	ReservationID    string `json:"reservation_id"`
	OrderNo          string `json:"order_no"`
	PaymentAttemptID string `json:"payment_attempt_id"`
	Version          int    `json:"version"`
}

// ConfirmationStore atomically confirms a local reservation and records the Kafka event.
type ConfirmationStore interface {
	ConfirmConsumed(context.Context, string, string, string, time.Time) error
}

// ConfirmationConsumer applies paid reservation events exactly once per consumer group.
type ConfirmationConsumer struct {
	store ConfirmationStore
	group string
}

// NewConfirmationConsumer builds the minimal Kafka message handler.
func NewConfirmationConsumer(store ConfirmationStore, group string) *ConfirmationConsumer {
	if group == "" {
		group = inventoryConfirmationConsumerGroup
	}
	return &ConfirmationConsumer{store: store, group: group}
}

// Handle commits local confirmation and consumption identity together; duplicates are successful no-ops.
func (c *ConfirmationConsumer) Handle(ctx context.Context, event ConfirmationEvent) error {
	if c == nil || c.store == nil {
		return errors.New("inventory confirmation consumer is unavailable")
	}
	if strings.TrimSpace(event.EventID) == "" || strings.TrimSpace(event.ReservationID) == "" || strings.TrimSpace(event.OrderNo) == "" || strings.TrimSpace(event.PaymentAttemptID) == "" || event.Version != 1 {
		return errors.New("invalid inventory confirmation event")
	}
	if err := c.store.ConfirmConsumed(ctx, event.EventID, c.group, event.ReservationID, time.Now().UTC().Truncate(time.Millisecond)); err != nil {
		return errors.New("confirm consumed inventory reservation failed")
	}
	return nil
}

type confirmationMessageReader interface {
	FetchMessage(context.Context) (kafka.Message, error)
	CommitMessages(context.Context, ...kafka.Message) error
	Close() error
}

type confirmationDeadLetterPublisher interface {
	Publish(context.Context, kafka.Message) error
	Close() error
}

type kafkaConfirmationDeadLetterPublisher struct{ writer *kafka.Writer }

func (p *kafkaConfirmationDeadLetterPublisher) Publish(ctx context.Context, message kafka.Message) error {
	return p.writer.WriteMessages(ctx, message)
}

func (p *kafkaConfirmationDeadLetterPublisher) Close() error { return p.writer.Close() }

// KafkaConfirmationConsumer reads the inventory confirmation topic and commits only handled or dead-lettered messages.
type KafkaConfirmationConsumer struct {
	reader       confirmationMessageReader
	handler      *ConfirmationConsumer
	dlqPublisher confirmationDeadLetterPublisher
}

// NewKafkaConfirmationConsumer creates the product-service consumer for paid inventory confirmations.
func NewKafkaConfirmationConsumer(brokers []string, handler *ConfirmationConsumer) *KafkaConfirmationConsumer {
	reader := kafka.NewReader(kafka.ReaderConfig{Brokers: brokers, GroupID: inventoryConfirmationConsumerGroup, Topic: "inventory.reservation.confirm", MinBytes: 1, MaxBytes: 1e6})
	publisher := &kafkaConfirmationDeadLetterPublisher{writer: &kafka.Writer{Addr: kafka.TCP(brokers...), RequiredAcks: kafka.RequireAll, Async: false}}
	return newKafkaConfirmationConsumer(reader, handler, publisher)
}

func newKafkaConfirmationConsumer(reader confirmationMessageReader, handler *ConfirmationConsumer, publisher confirmationDeadLetterPublisher) *KafkaConfirmationConsumer {
	return &KafkaConfirmationConsumer{reader: reader, handler: handler, dlqPublisher: publisher}
}

// Close releases the Kafka reader.
func (c *KafkaConfirmationConsumer) Close() error {
	if c == nil {
		return nil
	}
	if c.reader != nil {
		if err := c.reader.Close(); err != nil {
			return err
		}
	}
	if c.dlqPublisher != nil {
		return c.dlqPublisher.Close()
	}
	return nil
}

// Run consumes until cancellation. Messages are committed only after local confirmation or a producer-acknowledged DLQ publication.
func (c *KafkaConfirmationConsumer) Run(ctx context.Context) error {
	if c == nil || c.reader == nil || c.handler == nil || c.dlqPublisher == nil {
		return errors.New("inventory confirmation kafka consumer is unavailable")
	}
	for {
		message, err := c.reader.FetchMessage(ctx)
		if err != nil {
			return err
		}
		var event ConfirmationEvent
		if err := json.Unmarshal(message.Value, &event); err != nil {
			if err := c.deadLetterAndCommit(ctx, message, "malformed_confirmation_event"); err != nil {
				return err
			}
			continue
		}
		if err := retryConfirmation(ctx, func() error { return c.handler.Handle(ctx, event) }); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if err := c.deadLetterAndCommit(ctx, message, "confirmation_retry_exhausted"); err != nil {
				return err
			}
			continue
		}
		if err := c.reader.CommitMessages(ctx, message); err != nil {
			return err
		}
	}
}

func retryConfirmation(ctx context.Context, handle func() error) error {
	delay := confirmationRetryInitial
	var lastErr error
	for attempt := 0; attempt < confirmationRetryMaxAttempts; attempt++ {
		if err := handle(); err == nil {
			return nil
		} else {
			lastErr = err
		}
		if attempt == confirmationRetryMaxAttempts-1 {
			break
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return ctx.Err()
		case <-timer.C:
		}
		if delay < confirmationRetryMaximum {
			delay *= 2
			if delay > confirmationRetryMaximum {
				delay = confirmationRetryMaximum
			}
		}
	}
	return lastErr
}

func (c *KafkaConfirmationConsumer) deadLetterAndCommit(ctx context.Context, message kafka.Message, reason string) error {
	payload, err := json.Marshal(struct {
		Reason         string `json:"reason"`
		RawEventBase64 string `json:"raw_event_base64"`
	}{Reason: reason, RawEventBase64: base64.StdEncoding.EncodeToString(message.Value)})
	if err != nil {
		return errors.New("marshal inventory confirmation dead-letter event failed")
	}
	if err := retryConfirmation(ctx, func() error {
		return c.dlqPublisher.Publish(ctx, kafka.Message{Topic: inventoryConfirmationDeadLetterTopic, Key: message.Key, Value: payload})
	}); err != nil {
		return errors.New("publish inventory confirmation dead-letter event failed")
	}
	if err := c.reader.CommitMessages(ctx, message); err != nil {
		return errors.New("commit dead-lettered inventory confirmation message failed")
	}
	return nil
}
