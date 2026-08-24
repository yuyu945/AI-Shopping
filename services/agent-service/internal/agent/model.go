package agent

import (
	"encoding/json"
	"time"
)

// SessionStatus is the lifecycle state of an agent conversation session.
type SessionStatus string

const (
	SessionActive   SessionStatus = "ACTIVE"
	SessionArchived SessionStatus = "ARCHIVED"
)

// MessageRole identifies who produced an agent message.
type MessageRole string

const (
	MessageUser      MessageRole = "USER"
	MessageAssistant MessageRole = "ASSISTANT"
	MessageTool      MessageRole = "TOOL"
	MessageSystem    MessageRole = "SYSTEM"
)

// RunStatus is the lifecycle state of an agent run.
type RunStatus string

const (
	RunRunning   RunStatus = "RUNNING"
	RunSucceeded RunStatus = "SUCCEEDED"
	RunFailed    RunStatus = "FAILED"
	RunTimeout   RunStatus = "TIMEOUT"
)

// StepStatus is the lifecycle state of a model or tool step.
type StepStatus string

const (
	StepRunning   StepStatus = "RUNNING"
	StepSucceeded StepStatus = "SUCCEEDED"
	StepFailed    StepStatus = "FAILED"
	StepTimeout   StepStatus = "TIMEOUT"
)

// StepType identifies whether a step records model reasoning or tool execution.
type StepType string

const (
	StepTypeModel StepType = "MODEL"
	StepTypeTool  StepType = "TOOL"
)

// Run stores one bounded Agent execution.
type Run struct {
	ID              uint64
	RunID           string
	SessionID       uint64
	UserID          uint64
	TraceID         string
	UserInput       string
	Status          RunStatus
	ModelName       string
	PromptVersion   string
	StepCount       uint32
	FinalResultJSON json.RawMessage
	ErrorCode       string
	ErrorMessage    string
	StartedAt       time.Time
	EndedAt         *time.Time
	CreatedAt       time.Time
}

// Step stores one model or tool action within an Agent run.
type Step struct {
	ID           uint64
	RunDBID      uint64
	StepNo       uint32
	StepType     StepType
	ToolName     string
	Attempt      uint32
	InputJSON    json.RawMessage
	OutputJSON   json.RawMessage
	Status       StepStatus
	ErrorCode    string
	ErrorMessage string
	LatencyMS    uint32
	StartedAt    time.Time
	EndedAt      *time.Time
}

// StartRunCommand contains the durable data needed to create a run.
type StartRunCommand struct {
	SessionNo     string
	RunID         string
	UserID        uint64
	TraceID       string
	UserInput     string
	ModelName     string
	PromptVersion string
	Now           time.Time
}

// StepStart contains the durable data for a newly started step.
type StepStart struct {
	RunDBID   uint64
	StepNo    uint32
	StepType  StepType
	ToolName  string
	Attempt   uint32
	InputJSON json.RawMessage
	StartedAt time.Time
}

// StepResult contains the successful output of a step.
type StepResult struct {
	StepID     uint64
	OutputJSON json.RawMessage
	LatencyMS  uint32
	EndedAt    time.Time
}

// StepFailure contains the terminal failure of a step.
type StepFailure struct {
	StepID       uint64
	Status       StepStatus
	ErrorCode    string
	ErrorMessage string
	LatencyMS    uint32
	EndedAt      time.Time
}

// RunResult contains the terminal successful output of a run.
type RunResult struct {
	RunDBID         uint64
	FinalResultJSON json.RawMessage
	StepCount       uint32
	EndedAt         time.Time
}

// RunFailure contains the terminal failure of a run.
type RunFailure struct {
	RunDBID      uint64
	Status       RunStatus
	ErrorCode    string
	ErrorMessage string
	StepCount    uint32
	EndedAt      time.Time
}

// RunTimeline is a Run plus its ordered Step records.
type RunTimeline struct {
	Run   Run
	Steps []Step
}

// FinalRecommendationOutput is the only accepted model final recommendation shape.
type FinalRecommendationOutput struct {
	Recommendations []ModelRecommendation `json:"recommendations"`
}

// ModelRecommendation is the model-proposed SKU ranking before backend verification.
type ModelRecommendation struct {
	SKUID  uint64 `json:"sku_id"`
	RankNo uint32 `json:"rank_no"`
	Reason string `json:"reason"`
}

// RecommendationStatus is the validation state of a persisted recommendation snapshot.
type RecommendationStatus string

const (
	RecommendationVerified RecommendationStatus = "VERIFIED"
)

// RecommendationSnapshot stores backend-verified recommendation data.
type RecommendationSnapshot struct {
	ID                   uint64
	RunDBID              uint64
	RankNo               uint32
	SKUID                uint64
	ProductID            uint64
	ProductTitleSnapshot string
	SKUCodeSnapshot      string
	SKUSpecSnapshotJSON  json.RawMessage
	PriceSnapshot        string
	SaleableSnapshot     bool
	DiscountSnapshotJSON json.RawMessage
	Reason               string
	ValidationStatus     RecommendationStatus
	CreatedAt            time.Time
}
