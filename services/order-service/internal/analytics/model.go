// Package analytics consumes review events into a minimal trade-owned read model.
package analytics

import (
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	ReviewEventsTopic     = "review.events"
	ReviewEventsDeadTopic = "review.events.deadletter"
	ReviewConsumerGroup   = "order-review-analytics-v1"
)

type ReviewEvent struct {
	EventID    string    `json:"event_id"`
	EventType  string    `json:"event_type"`
	ReviewNo   string    `json:"review_no"`
	OrderNo    string    `json:"order_no"`
	UserID     uint64    `json:"user_id"`
	ProductID  uint64    `json:"product_id"`
	SKUID      uint64    `json:"sku_id"`
	Rating     uint32    `json:"rating"`
	Content    string    `json:"content"`
	OccurredAt time.Time `json:"occurred_at"`
	Version    uint32    `json:"version"`
}

func (e ReviewEvent) Validate() error {
	if _, err := uuid.Parse(e.EventID); err != nil {
		return errors.New("invalid review event id")
	}
	if e.EventType != "review.submitted" || strings.TrimSpace(e.ReviewNo) == "" || strings.TrimSpace(e.OrderNo) == "" || e.UserID == 0 || e.ProductID == 0 || e.SKUID == 0 {
		return errors.New("invalid review event identity")
	}
	if e.Rating < 1 || e.Rating > 5 {
		return errors.New("invalid review event rating")
	}
	if strings.TrimSpace(e.Content) == "" || len([]rune(e.Content)) > 1000 {
		return errors.New("invalid review event content")
	}
	if e.OccurredAt.IsZero() || e.Version != 1 {
		return errors.New("invalid review event version")
	}
	return nil
}
