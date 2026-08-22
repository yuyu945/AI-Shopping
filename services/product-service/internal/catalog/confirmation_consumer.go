package catalog

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"

	"github.com/segmentio/kafka-go"
)

const inventoryConfirmationConsumerGroup = "product-inventory-confirmation"

// ConfirmationEvent is the payload published after a wallet payment commits.
type ConfirmationEvent struct {
	EventID          string `json:"event_id"`
	ReservationID    string `json:"reservation_id"`
	OrderNo          string `json:"order_no"`
	PaymentAttemptID string `json:"payment_attempt_id"`
	Version          int    `json:"version"`
}

// ConsumptionStore owns catalog-side idempotency records for Kafka events.
type ConsumptionStore interface {
	Record(context.Context, string, string) (bool, error)
}

type reservationConfirmer interface {
	ConfirmReservation(context.Context, string) (Reservation, error)
}

// ConfirmationConsumer applies paid reservation events exactly once per consumer group.
type ConfirmationConsumer struct {
	consumptions ConsumptionStore
	reservations reservationConfirmer
	group        string
}

// NewConfirmationConsumer builds the minimal Kafka message handler.
func NewConfirmationConsumer(consumptions ConsumptionStore, reservations reservationConfirmer, group string) *ConfirmationConsumer {
	if group == "" {
		group = inventoryConfirmationConsumerGroup
	}
	return &ConfirmationConsumer{consumptions: consumptions, reservations: reservations, group: group}
}

// Handle records a message before confirming the local reservation; duplicate records are successful no-ops.
func (c *ConfirmationConsumer) Handle(ctx context.Context, event ConfirmationEvent) error {
	if c == nil || c.consumptions == nil || c.reservations == nil {
		return errors.New("inventory confirmation consumer is unavailable")
	}
	if strings.TrimSpace(event.EventID) == "" || strings.TrimSpace(event.ReservationID) == "" || strings.TrimSpace(event.OrderNo) == "" || strings.TrimSpace(event.PaymentAttemptID) == "" || event.Version != 1 {
		return errors.New("invalid inventory confirmation event")
	}
	recorded, err := c.consumptions.Record(ctx, event.EventID, c.group)
	if err != nil {
		return errors.New("record inventory confirmation event failed")
	}
	if !recorded {
		return nil
	}
	_, err = c.reservations.ConfirmReservation(ctx, event.ReservationID)
	return err
}

// MySQLConsumptionStore stores catalog-owned Kafka consumption identities.
type MySQLConsumptionStore struct{ db *sql.DB }

// NewMySQLConsumptionStore constructs catalog consumption persistence.
func NewMySQLConsumptionStore(db *sql.DB) *MySQLConsumptionStore {
	return &MySQLConsumptionStore{db: db}
}

// Record inserts the event identity once. Duplicate deliveries are successful no-ops.
func (s *MySQLConsumptionStore) Record(ctx context.Context, eventID, group string) (bool, error) {
	if s == nil || s.db == nil {
		return false, errors.New("catalog consumption database is required")
	}
	result, err := s.db.ExecContext(ctx, `INSERT IGNORE INTO reservation_event_consumptions (event_id, consumer_group) VALUES (?, ?)`, eventID, group)
	if err != nil {
		return false, err
	}
	rows, err := result.RowsAffected()
	return rows == 1, err
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
		if err := c.handler.Handle(ctx, event); err != nil {
			return err
		}
		if err := c.reader.CommitMessages(ctx, message); err != nil {
			return err
		}
	}
}
