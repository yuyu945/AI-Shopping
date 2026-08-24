package handler

import (
	"context"
	"encoding/json"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/yuyu945/AI-Shopping/apps/gateway/internal/behavior"
	platformauth "github.com/yuyu945/AI-Shopping/internal/platform/auth"
	gozerotrace "github.com/zeromicro/go-zero/core/trace"
)

type BehaviorRecorder interface {
	Record(context.Context, behavior.Event) error
}

func recordBehavior(ctx context.Context, recorder BehaviorRecorder, eventType, resourceType, resourceID string, payload map[string]any) {
	if recorder == nil {
		return
	}
	userID := uint64(0)
	if principal, ok := platformauth.PrincipalFromContext(ctx); ok {
		userID = principal.UserID
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return
	}
	_ = recorder.Record(ctx, behavior.Event{
		EventID: uuid.NewString(), UserID: userID, EventType: eventType, TraceID: gozerotrace.TraceIDFromContext(ctx),
		ResourceType: resourceType, ResourceID: resourceID, Payload: data, OccurredAt: time.Now().UTC(),
	})
}

func uintResourceID(value uint64) string {
	return strconv.FormatUint(value, 10)
}

func requestStatusPayload(status string) map[string]any {
	return map[string]any{"status": status, "version": 1}
}
