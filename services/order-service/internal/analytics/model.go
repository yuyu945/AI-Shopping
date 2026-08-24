// Package analytics consumes review events into a minimal trade-owned read model.
package analytics

import (
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	ReviewEventsTopic       = "review.events"
	ReviewEventsDeadTopic   = "review.events.deadletter"
	ReviewConsumerGroup     = "order-review-analytics-v1"
	BehaviorEventsTopic     = "behavior.events"
	BehaviorEventsDeadTopic = "behavior.events.deadletter"
	BehaviorConsumerGroup   = "order-behavior-analytics-v1"
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

type BehaviorEvent struct {
	EventID      string          `json:"event_id"`
	EventType    string          `json:"event_type"`
	UserID       uint64          `json:"user_id"`
	TraceID      string          `json:"trace_id"`
	ResourceType string          `json:"resource_type"`
	ResourceID   string          `json:"resource_id"`
	Payload      json.RawMessage `json:"payload"`
	OccurredAt   time.Time       `json:"occurred_at"`
	Version      uint32          `json:"version"`
}

func (e BehaviorEvent) Validate() error {
	if _, err := uuid.Parse(e.EventID); err != nil {
		return errors.New("invalid behavior event id")
	}
	if strings.TrimSpace(e.EventType) == "" || e.UserID == 0 || strings.TrimSpace(e.TraceID) == "" || strings.TrimSpace(e.ResourceType) == "" || strings.TrimSpace(e.ResourceID) == "" {
		return errors.New("invalid behavior event identity")
	}
	if e.OccurredAt.IsZero() || e.Version != 1 {
		return errors.New("invalid behavior event version")
	}
	if !json.Valid(e.Payload) || containsPIIKey(e.Payload) {
		return errors.New("invalid behavior event payload")
	}
	return nil
}

type DeadLetterRecord struct {
	Topic          string
	EventKey       string
	Reason         string
	RawEventBase64 string
}

func containsPIIKey(raw json.RawMessage) bool {
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return true
	}
	return containsPIIValue(value)
}

func containsPIIValue(value any) bool {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			switch strings.ToLower(strings.TrimSpace(key)) {
			case "phone", "mobile", "address", "token", "jwt", "authorization", "password", "secret", "api_key", "wallet_balance", "review_content", "prompt":
				return true
			}
			if containsPIIValue(child) {
				return true
			}
		}
	case []any:
		for _, child := range typed {
			if containsPIIValue(child) {
				return true
			}
		}
	}
	return false
}
