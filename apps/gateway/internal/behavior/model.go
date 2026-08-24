package behavior

import (
	"encoding/json"
	"errors"
	"strings"
	"time"
)

const BehaviorEventsTopic = "behavior.events"

type Event struct {
	EventID      string
	UserID       uint64
	EventType    string
	TraceID      string
	ResourceType string
	ResourceID   string
	Payload      json.RawMessage
	OccurredAt   time.Time
}

func (e Event) Validate() error {
	if strings.TrimSpace(e.EventID) == "" || e.UserID == 0 || strings.TrimSpace(e.EventType) == "" || strings.TrimSpace(e.TraceID) == "" || strings.TrimSpace(e.ResourceType) == "" || strings.TrimSpace(e.ResourceID) == "" {
		return errors.New("invalid behavior event")
	}
	if !json.Valid(e.Payload) || containsPIIKey(e.Payload) {
		return errors.New("invalid behavior event payload")
	}
	return nil
}

type LeasedEvent struct {
	ID      uint64
	EventID string
	UserID  uint64
	Type    string
	Topic   string
	Key     string
	Payload json.RawMessage
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
