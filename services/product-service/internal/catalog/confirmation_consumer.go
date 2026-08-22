package catalog

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/segmentio/kafka-go"
)

const inventoryConfirmationConsumerGroup = "product-inventory-confirmation"

const (
	confirmationRetryInitial = 100 * time.Millisecond
	confirmationRetryMaximum = time.Second
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

// KafkaConfirmationConsumer reads the inventory confirmation topic and commits only handled messages.
type KafkaConfirmationConsumer struct {
	reader  *kafka.Reader
	handler *ConfirmationConsumer
}

// NewKafkaConfirmationConsumer creates the product-service consumer for paid inventory confirmations.
func NewKafkaConfirmationConsumer(brokers []string, handler *ConfirmationConsumer) *KafkaConfirmationConsumer {
	return &KafkaConfirmationConsumer{reader: kafka.NewReader(kafka.ReaderConfig{Brokers: brokers, GroupID: inventoryConfirmationConsumerGroup, Topic: "inventory.reservation.confirm", MinBytes: 1, MaxBytes: 1e6}), handler: handler}
}

// Close releases the Kafka reader.
func (c *KafkaConfirmationConsumer) Close() error {
	if c == nil || c.reader == nil {
		return nil
	}
	return c.reader.Close()
}

// Run consumes until cancellation. Failed messages are not committed and will be retried by Kafka.
func (c *KafkaConfirmationConsumer) Run(ctx context.Context) error {
	if c == nil || c.reader == nil || c.handler == nil {
		return errors.New("inventory confirmation kafka consumer is unavailable")
	}
	for {
		message, err := c.reader.FetchMessage(ctx)
		if err != nil {
			return err
		}
		var event ConfirmationEvent
		if err := json.Unmarshal(message.Value, &event); err != nil {
			return errors.New("decode inventory confirmation event failed")
		}
		if err := retryConfirmation(ctx, func() error { return c.handler.Handle(ctx, event) }); err != nil {
			return err
		}
		if err := c.reader.CommitMessages(ctx, message); err != nil {
			return err
		}
	}
}

func retryConfirmation(ctx context.Context, handle func() error) error {
	delay := confirmationRetryInitial
	for {
		if err := handle(); err == nil {
			return nil
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
}
