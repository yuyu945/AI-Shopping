package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

const (
	ErrorCodeInvalidToolArgument        = "INVALID_TOOL_ARGUMENT"
	ErrorCodeUnknownTool                = "UNKNOWN_TOOL"
	ErrorCodeToolFailed                 = "TOOL_FAILED"
	ErrorCodeDependencyTimeout          = "DEPENDENCY_TIMEOUT"
	ErrorCodeMaxStepsExceeded           = "MAX_STEPS_EXCEEDED"
	ErrorCodeModelFailed                = "MODEL_FAILED"
	ErrorCodeInvalidFinalRecommendation = "INVALID_FINAL_RECOMMENDATION"
	ErrorCodeNoValidRecommendation      = "NO_VALID_RECOMMENDATION"
)

// ErrMaxStepsExceeded reports a run that exhausted its bounded step budget.
var ErrMaxStepsExceeded = errors.New(ErrorCodeMaxStepsExceeded)

// ErrModelFailed reports a model provider failure through a stable domain error.
var ErrModelFailed = errors.New(ErrorCodeModelFailed)

// ErrNoValidRecommendation reports that every model-recommended SKU failed backend verification.
var ErrNoValidRecommendation = errors.New(ErrorCodeNoValidRecommendation)

// RunStore persists Agent runs and steps.
type RunStore interface {
	CreateRun(context.Context, StartRunCommand) (Run, error)
	AppendStepStarted(context.Context, StepStart) (Step, error)
	MarkStepSucceeded(context.Context, StepResult) error
	MarkStepFailed(context.Context, StepFailure) error
	MarkRunSucceeded(context.Context, RunResult) error
	MarkRunFailed(context.Context, RunFailure) error
	SaveRecommendations(context.Context, uint64, []RecommendationSnapshot) error
	CompleteRunWithRecommendations(context.Context, RunResult, []RecommendationSnapshot) error
}

// ChatModel is the model-provider boundary used by the Agent run loop.
type ChatModel interface {
	Next(context.Context, ModelInput) (ModelOutput, error)
}

// ToolRunner executes a validated Tool invocation.
type ToolRunner interface {
	Execute(context.Context, ToolInvocation) (ToolResult, error)
}

// RecommendationVerification builds trusted recommendation snapshots from model candidates.
type RecommendationVerification interface {
	Verify(context.Context, FinalRecommendationOutput) ([]RecommendationSnapshot, error)
}

// ToolCall is one model-requested Tool invocation.
type ToolCall struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

// ModelInput contains the current run state passed to a model turn.
type ModelInput struct {
	RunID       string
	UserID      uint64
	UserInput   string
	ToolResults []ToolResult
}

// ModelOutput contains either final output or one Tool call.
type ModelOutput struct {
	FinalText string          `json:"final_text,omitempty"`
	FinalJSON json.RawMessage `json:"final_json,omitempty"`
	ToolCall  *ToolCall       `json:"tool_call,omitempty"`
}

// RunServiceOptions configures the bounded Agent run loop.
type RunServiceOptions struct {
	MaxSteps               uint32
	RunTimeout             time.Duration
	Now                    func() time.Time
	RecommendationVerifier RecommendationVerification
}

// RunOutcome is the synchronous M4.1 result of a bounded run.
type RunOutcome struct {
	RunID     string
	Status    RunStatus
	FinalText string
	ErrorCode string
}

// RunService coordinates model turns, controlled Tools, and durable steps.
type RunService struct {
	store    RunStore
	model    ChatModel
	registry *ToolRegistry
	tools    ToolRunner
	options  RunServiceOptions
}

// NewRunService constructs a bounded Agent run service.
func NewRunService(store RunStore, model ChatModel, registry *ToolRegistry, tools ToolRunner, options RunServiceOptions) *RunService {
	if registry == nil {
		registry = NewDefaultToolRegistry(time.Second)
	}
	if options.MaxSteps == 0 {
		options.MaxSteps = 8
	}
	if options.RunTimeout <= 0 {
		options.RunTimeout = 30 * time.Second
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	return &RunService{store: store, model: model, registry: registry, tools: tools, options: options}
}

// StartRun creates a run and executes the bounded M4.1 model/tool loop.
func (s *RunService) StartRun(ctx context.Context, command StartRunCommand) (RunOutcome, error) {
	if command.Now.IsZero() {
		command.Now = s.options.Now()
	}
	run, err := s.store.CreateRun(ctx, command)
	if err != nil {
		return RunOutcome{}, err
	}

	runCtx, cancel := context.WithTimeout(ctx, s.options.RunTimeout)
	defer cancel()

	input := ModelInput{RunID: run.RunID, UserID: run.UserID, UserInput: run.UserInput}
	var stepCount uint32
	for {
		if stepCount >= s.options.MaxSteps {
			failure := RunFailure{
				RunDBID: run.ID, Status: RunFailed, ErrorCode: ErrorCodeMaxStepsExceeded,
				ErrorMessage: "agent run exceeded maximum steps", StepCount: stepCount, EndedAt: s.options.Now(),
			}
			_ = s.store.MarkRunFailed(ctx, failure)
			return RunOutcome{RunID: run.RunID, Status: RunFailed, ErrorCode: failure.ErrorCode}, ErrMaxStepsExceeded
		}

		stepCount++
		modelOutput, err := s.executeModelStep(runCtx, run.ID, stepCount, input)
		if err != nil {
			_ = s.store.MarkRunFailed(ctx, RunFailure{
				RunDBID: run.ID, Status: RunFailed, ErrorCode: errorCodeFor(err),
				ErrorMessage: stableErrorMessage(err), StepCount: stepCount, EndedAt: s.options.Now(),
			})
			return RunOutcome{RunID: run.RunID, Status: RunFailed, ErrorCode: errorCodeFor(err)}, err
		}
		if len(modelOutput.FinalJSON) > 0 {
			outcome, err := s.completeRunWithRecommendations(ctx, runCtx, run, stepCount, modelOutput.FinalJSON)
			if err != nil {
				return outcome, err
			}
			return outcome, nil
		}
		if modelOutput.FinalText != "" {
			finalJSON := mustMarshalJSON(map[string]string{"final_text": modelOutput.FinalText})
			if err := s.store.MarkRunSucceeded(ctx, RunResult{RunDBID: run.ID, FinalResultJSON: finalJSON, StepCount: stepCount, EndedAt: s.options.Now()}); err != nil {
				return RunOutcome{}, err
			}
			return RunOutcome{RunID: run.RunID, Status: RunSucceeded, FinalText: modelOutput.FinalText}, nil
		}
		if modelOutput.ToolCall == nil {
			continue
		}
		if stepCount >= s.options.MaxSteps {
			_ = s.store.MarkRunFailed(ctx, RunFailure{
				RunDBID: run.ID, Status: RunFailed, ErrorCode: ErrorCodeMaxStepsExceeded,
				ErrorMessage: "agent run exceeded maximum steps", StepCount: stepCount, EndedAt: s.options.Now(),
			})
			return RunOutcome{RunID: run.RunID, Status: RunFailed, ErrorCode: ErrorCodeMaxStepsExceeded}, ErrMaxStepsExceeded
		}

		stepCount++
		toolResult, err := s.executeToolStep(runCtx, run.ID, stepCount, run.UserID, *modelOutput.ToolCall)
		if err != nil {
			_ = s.store.MarkRunFailed(ctx, RunFailure{
				RunDBID: run.ID, Status: RunFailed, ErrorCode: errorCodeFor(err),
				ErrorMessage: stableErrorMessage(err), StepCount: stepCount, EndedAt: s.options.Now(),
			})
			return RunOutcome{RunID: run.RunID, Status: RunFailed, ErrorCode: errorCodeFor(err)}, err
		}
		input.ToolResults = append(input.ToolResults, toolResult)
	}
}

func (s *RunService) completeRunWithRecommendations(ctx context.Context, runCtx context.Context, run Run, stepCount uint32, raw json.RawMessage) (RunOutcome, error) {
	finalOutput, err := ParseFinalRecommendations(raw)
	if err != nil {
		return s.failRun(ctx, run, stepCount, err)
	}
	if s.options.RecommendationVerifier == nil {
		return s.failRun(ctx, run, stepCount, ErrNoValidRecommendation)
	}
	snapshots, err := s.options.RecommendationVerifier.Verify(runCtx, finalOutput)
	if err != nil {
		return s.failRun(ctx, run, stepCount, err)
	}
	if len(snapshots) == 0 {
		return s.failRun(ctx, run, stepCount, ErrNoValidRecommendation)
	}
	now := s.options.Now()
	for i := range snapshots {
		snapshots[i].RunDBID = run.ID
		if snapshots[i].CreatedAt.IsZero() {
			snapshots[i].CreatedAt = now
		}
	}
	finalJSON := mustMarshalJSON(finalOutput)
	if err := s.store.CompleteRunWithRecommendations(ctx, RunResult{RunDBID: run.ID, FinalResultJSON: finalJSON, StepCount: stepCount, EndedAt: s.options.Now()}, snapshots); err != nil {
		return RunOutcome{}, err
	}
	return RunOutcome{RunID: run.RunID, Status: RunSucceeded}, nil
}

func (s *RunService) failRun(ctx context.Context, run Run, stepCount uint32, err error) (RunOutcome, error) {
	code := errorCodeFor(err)
	_ = s.store.MarkRunFailed(ctx, RunFailure{
		RunDBID: run.ID, Status: RunFailed, ErrorCode: code,
		ErrorMessage: stableErrorMessage(err), StepCount: stepCount, EndedAt: s.options.Now(),
	})
	return RunOutcome{RunID: run.RunID, Status: RunFailed, ErrorCode: code}, err
}

func (s *RunService) executeModelStep(ctx context.Context, runDBID uint64, stepNo uint32, input ModelInput) (ModelOutput, error) {
	startedAt := s.options.Now()
	step, err := s.store.AppendStepStarted(ctx, StepStart{
		RunDBID: runDBID, StepNo: stepNo, StepType: StepTypeModel, Attempt: 1,
		InputJSON: mustMarshalJSON(input), StartedAt: startedAt,
	})
	if err != nil {
		return ModelOutput{}, err
	}
	output, err := s.model.Next(ctx, input)
	latency := latencyMillis(startedAt, s.options.Now())
	if err != nil {
		failureErr := fmt.Errorf("%w: model turn failed", ErrModelFailed)
		_ = s.store.MarkStepFailed(ctx, StepFailure{
			StepID: step.ID, Status: StepFailed, ErrorCode: ErrorCodeModelFailed,
			ErrorMessage: stableErrorMessage(failureErr), LatencyMS: latency, EndedAt: s.options.Now(),
		})
		return ModelOutput{}, failureErr
	}
	if err := s.store.MarkStepSucceeded(ctx, StepResult{StepID: step.ID, OutputJSON: mustMarshalJSON(output), LatencyMS: latency, EndedAt: s.options.Now()}); err != nil {
		return ModelOutput{}, err
	}
	return output, nil
}

func (s *RunService) executeToolStep(ctx context.Context, runDBID uint64, stepNo uint32, userID uint64, call ToolCall) (ToolResult, error) {
	startedAt := s.options.Now()
	step, err := s.store.AppendStepStarted(ctx, StepStart{
		RunDBID: runDBID, StepNo: stepNo, StepType: StepTypeTool, ToolName: call.Name, Attempt: 1,
		InputJSON: mustMarshalJSON(call), StartedAt: startedAt,
	})
	if err != nil {
		return ToolResult{}, err
	}
	invocation, err := s.registry.Validate(call.Name, call.Arguments, ToolContext{UserID: userID})
	if err != nil {
		_ = s.markToolStepFailed(ctx, step.ID, err, startedAt)
		return ToolResult{}, err
	}
	result, err := s.tools.Execute(ctx, invocation)
	if err != nil {
		_ = s.markToolStepFailed(ctx, step.ID, err, startedAt)
		return ToolResult{}, err
	}
	if err := s.store.MarkStepSucceeded(ctx, StepResult{StepID: step.ID, OutputJSON: mustMarshalJSON(result.Output), LatencyMS: latencyMillis(startedAt, s.options.Now()), EndedAt: s.options.Now()}); err != nil {
		return ToolResult{}, err
	}
	return result, nil
}

func (s *RunService) markToolStepFailed(ctx context.Context, stepID uint64, err error, startedAt time.Time) error {
	status := StepFailed
	if errors.Is(err, ErrDependencyTimeout) || errors.Is(err, context.DeadlineExceeded) {
		status = StepTimeout
	}
	return s.store.MarkStepFailed(ctx, StepFailure{
		StepID: stepID, Status: status, ErrorCode: errorCodeFor(err),
		ErrorMessage: stableErrorMessage(err), LatencyMS: latencyMillis(startedAt, s.options.Now()), EndedAt: s.options.Now(),
	})
}

func errorCodeFor(err error) string {
	switch {
	case errors.Is(err, ErrInvalidToolArgument):
		return ErrorCodeInvalidToolArgument
	case errors.Is(err, ErrUnknownTool):
		return ErrorCodeUnknownTool
	case errors.Is(err, ErrDependencyTimeout), errors.Is(err, context.DeadlineExceeded):
		return ErrorCodeDependencyTimeout
	case errors.Is(err, ErrMaxStepsExceeded):
		return ErrorCodeMaxStepsExceeded
	case errors.Is(err, ErrModelFailed):
		return ErrorCodeModelFailed
	case errors.Is(err, ErrInvalidFinalRecommendation):
		return ErrorCodeInvalidFinalRecommendation
	case errors.Is(err, ErrNoValidRecommendation):
		return ErrorCodeNoValidRecommendation
	default:
		return ErrorCodeToolFailed
	}
}

func stableErrorMessage(err error) string {
	switch errorCodeFor(err) {
	case ErrorCodeInvalidToolArgument:
		return "invalid tool arguments"
	case ErrorCodeUnknownTool:
		return "unknown tool"
	case ErrorCodeDependencyTimeout:
		return "dependency timeout"
	case ErrorCodeMaxStepsExceeded:
		return "agent run exceeded maximum steps"
	case ErrorCodeModelFailed:
		return "model request failed"
	case ErrorCodeInvalidFinalRecommendation:
		return "invalid final recommendation"
	case ErrorCodeNoValidRecommendation:
		return "no valid recommendation"
	default:
		return "tool execution failed"
	}
}

func latencyMillis(start, end time.Time) uint32 {
	if end.Before(start) {
		return 0
	}
	return uint32(end.Sub(start).Milliseconds())
}

func mustMarshalJSON(value any) json.RawMessage {
	data, err := json.Marshal(value)
	if err != nil {
		return json.RawMessage(`null`)
	}
	return data
}
