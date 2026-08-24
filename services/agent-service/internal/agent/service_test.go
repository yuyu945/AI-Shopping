package agent

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func TestRunServiceCompletesAfterToolLoop(t *testing.T) {
	store := newFakeRunStore()
	model := &fakeChatModel{outputs: []ModelOutput{
		{ToolCall: &ToolCall{Name: ToolSearchProducts, Arguments: json.RawMessage(`{"keyword":"laptop","limit":1}`)}},
		{FinalText: "推荐轻薄笔记本"},
	}}
	tools := &fakeToolRunner{result: ToolResult{ToolName: ToolSearchProducts, Output: ProductSearchResult{Products: []ProductSearchItem{{ProductID: 1001, Title: "轻薄笔记本"}}}}}
	service := NewRunService(store, model, NewDefaultToolRegistry(time.Second), tools, RunServiceOptions{MaxSteps: 8, RunTimeout: time.Second, Now: fixedRunTime})

	result, err := service.StartRun(context.Background(), StartRunCommand{
		SessionNo: "sess_1", RunID: "run_1", UserID: 42, TraceID: "trace_1", UserInput: "预算 5000 买笔记本",
		ModelName: "qwen-plus", PromptVersion: "m4.1-v1", Now: fixedRunTime(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != RunSucceeded || result.FinalText != "推荐轻薄笔记本" {
		t.Fatalf("result=%#v", result)
	}
	if got := store.stepTypes(); len(got) != 3 || got[0] != StepTypeModel || got[1] != StepTypeTool || got[2] != StepTypeModel {
		t.Fatalf("step types=%v", got)
	}
	if len(store.succeededSteps) != 3 || store.runSucceeded == nil || store.runSucceeded.StepCount != 3 {
		t.Fatalf("store=%#v", store)
	}
	if len(model.inputs) != 2 || len(model.inputs[1].ToolResults) != 1 {
		t.Fatalf("model inputs=%#v", model.inputs)
	}
}

func TestRunServiceCompletesWithVerifiedRecommendations(t *testing.T) {
	store := newFakeRunStore()
	model := &fakeChatModel{outputs: []ModelOutput{{FinalJSON: []byte(`{"recommendations":[{"sku_id":2001,"rank_no":1,"reason":"适合编程"}]}`)}}}
	verifier := &fakeRecommendationVerifier{snapshots: []RecommendationSnapshot{{
		RankNo: 1, SKUID: 2001, ProductID: 1001, ProductTitleSnapshot: "轻薄笔记本",
		SKUCodeSnapshot: "LAPTOP-16G", SKUSpecSnapshotJSON: []byte(`{"memory":"16G"}`),
		PriceSnapshot: "4999.00", SaleableSnapshot: true, DiscountSnapshotJSON: []byte(`[{"promotion_id":3001}]`),
		Reason: "适合编程", ValidationStatus: RecommendationVerified,
	}}}
	service := NewRunService(store, model, NewDefaultToolRegistry(time.Second), &fakeToolRunner{}, RunServiceOptions{MaxSteps: 8, RunTimeout: time.Second, Now: fixedRunTime, RecommendationVerifier: verifier})

	result, err := service.StartRun(context.Background(), validRunCommand())
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != RunSucceeded || result.FinalText != "" {
		t.Fatalf("result=%#v", result)
	}
	if len(verifier.output.Recommendations) != 1 || verifier.output.Recommendations[0].SKUID != 2001 {
		t.Fatalf("verifier output=%#v", verifier.output)
	}
	if len(store.savedRecommendations) != 1 || store.savedRecommendations[0].RunDBID != 300 || store.savedRecommendations[0].CreatedAt.IsZero() {
		t.Fatalf("saved recommendations=%#v", store.savedRecommendations)
	}
	if store.runSucceeded == nil || string(store.runSucceeded.FinalResultJSON) == "" {
		t.Fatalf("run succeeded=%#v", store.runSucceeded)
	}
}

func TestRunServiceFailsWhenNoValidRecommendation(t *testing.T) {
	store := newFakeRunStore()
	model := &fakeChatModel{outputs: []ModelOutput{{FinalJSON: []byte(`{"recommendations":[{"sku_id":9999,"rank_no":1,"reason":"不存在"}]}`)}}}
	verifier := &fakeRecommendationVerifier{err: ErrNoValidRecommendation}
	service := NewRunService(store, model, NewDefaultToolRegistry(time.Second), &fakeToolRunner{}, RunServiceOptions{MaxSteps: 8, RunTimeout: time.Second, Now: fixedRunTime, RecommendationVerifier: verifier})

	_, err := service.StartRun(context.Background(), validRunCommand())
	if !errors.Is(err, ErrNoValidRecommendation) {
		t.Fatalf("error=%v, want ErrNoValidRecommendation", err)
	}
	if store.runFailed == nil || store.runFailed.ErrorCode != ErrorCodeNoValidRecommendation || len(store.savedRecommendations) != 0 {
		t.Fatalf("run failure=%#v saved=%#v", store.runFailed, store.savedRecommendations)
	}
}

func TestRunServiceRejectsInvalidToolArguments(t *testing.T) {
	store := newFakeRunStore()
	model := &fakeChatModel{outputs: []ModelOutput{
		{ToolCall: &ToolCall{Name: ToolGetPriceStock, Arguments: json.RawMessage(`{"sku_ids":[]}`)}},
	}}
	service := NewRunService(store, model, NewDefaultToolRegistry(time.Second), &fakeToolRunner{}, RunServiceOptions{MaxSteps: 8, RunTimeout: time.Second, Now: fixedRunTime})

	_, err := service.StartRun(context.Background(), validRunCommand())
	if !errors.Is(err, ErrInvalidToolArgument) {
		t.Fatalf("StartRun() error = %v, want ErrInvalidToolArgument", err)
	}
	if store.failedSteps[0].ErrorCode != ErrorCodeInvalidToolArgument || store.runFailed.ErrorCode != ErrorCodeInvalidToolArgument {
		t.Fatalf("failed step=%#v run=%#v", store.failedSteps[0], store.runFailed)
	}
}

func TestRunServiceTimesOutToolStep(t *testing.T) {
	store := newFakeRunStore()
	model := &fakeChatModel{outputs: []ModelOutput{
		{ToolCall: &ToolCall{Name: ToolSearchProducts, Arguments: json.RawMessage(`{"keyword":"laptop","limit":1}`)}},
	}}
	tools := &fakeToolRunner{err: ErrDependencyTimeout}
	service := NewRunService(store, model, NewDefaultToolRegistry(time.Second), tools, RunServiceOptions{MaxSteps: 8, RunTimeout: time.Second, Now: fixedRunTime})

	_, err := service.StartRun(context.Background(), validRunCommand())
	if !errors.Is(err, ErrDependencyTimeout) {
		t.Fatalf("StartRun() error = %v, want ErrDependencyTimeout", err)
	}
	if store.failedSteps[0].Status != StepTimeout || store.failedSteps[0].ErrorCode != ErrorCodeDependencyTimeout {
		t.Fatalf("failed step=%#v", store.failedSteps[0])
	}
}

func TestRunServiceFailsOnUnknownTool(t *testing.T) {
	store := newFakeRunStore()
	model := &fakeChatModel{outputs: []ModelOutput{
		{ToolCall: &ToolCall{Name: "write_order", Arguments: json.RawMessage(`{}`)}},
	}}
	service := NewRunService(store, model, NewDefaultToolRegistry(time.Second), &fakeToolRunner{}, RunServiceOptions{MaxSteps: 8, RunTimeout: time.Second, Now: fixedRunTime})

	_, err := service.StartRun(context.Background(), validRunCommand())
	if !errors.Is(err, ErrUnknownTool) {
		t.Fatalf("StartRun() error = %v, want ErrUnknownTool", err)
	}
	if store.runFailed.ErrorCode != ErrorCodeUnknownTool {
		t.Fatalf("run failure=%#v", store.runFailed)
	}
}

func TestRunServiceFailsWhenMaxStepsExceeded(t *testing.T) {
	store := newFakeRunStore()
	model := &fakeChatModel{outputs: []ModelOutput{
		{FinalText: ""},
		{FinalText: ""},
	}}
	service := NewRunService(store, model, NewDefaultToolRegistry(time.Second), &fakeToolRunner{}, RunServiceOptions{MaxSteps: 1, RunTimeout: time.Second, Now: fixedRunTime})

	_, err := service.StartRun(context.Background(), validRunCommand())
	if !errors.Is(err, ErrMaxStepsExceeded) {
		t.Fatalf("StartRun() error = %v, want ErrMaxStepsExceeded", err)
	}
	if store.runFailed.ErrorCode != ErrorCodeMaxStepsExceeded || store.runFailed.StepCount != 1 {
		t.Fatalf("run failure=%#v", store.runFailed)
	}
}

func TestRunServiceMapsModelFailure(t *testing.T) {
	store := newFakeRunStore()
	model := &fakeChatModel{err: errors.New("provider unavailable")}
	service := NewRunService(store, model, NewDefaultToolRegistry(time.Second), &fakeToolRunner{}, RunServiceOptions{MaxSteps: 8, RunTimeout: time.Second, Now: fixedRunTime})

	_, err := service.StartRun(context.Background(), validRunCommand())
	if !errors.Is(err, ErrModelFailed) {
		t.Fatalf("StartRun() error = %v, want ErrModelFailed", err)
	}
	if store.failedSteps[0].ErrorCode != ErrorCodeModelFailed || store.runFailed.ErrorCode != ErrorCodeModelFailed {
		t.Fatalf("failed step=%#v run=%#v", store.failedSteps[0], store.runFailed)
	}
}

func validRunCommand() StartRunCommand {
	return StartRunCommand{
		SessionNo: "sess_1", RunID: "run_1", UserID: 42, TraceID: "trace_1", UserInput: "预算 5000 买笔记本",
		ModelName: "qwen-plus", PromptVersion: "m4.1-v1", Now: fixedRunTime(),
	}
}

func fixedRunTime() time.Time {
	return time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)
}

type fakeChatModel struct {
	inputs  []ModelInput
	outputs []ModelOutput
	err     error
}

func (f *fakeChatModel) Next(ctx context.Context, input ModelInput) (ModelOutput, error) {
	f.inputs = append(f.inputs, input)
	if f.err != nil {
		return ModelOutput{}, f.err
	}
	if len(f.outputs) == 0 {
		return ModelOutput{}, nil
	}
	output := f.outputs[0]
	f.outputs = f.outputs[1:]
	return output, nil
}

type fakeToolRunner struct {
	invocations []ToolInvocation
	result      ToolResult
	err         error
}

func (f *fakeToolRunner) Execute(ctx context.Context, invocation ToolInvocation) (ToolResult, error) {
	f.invocations = append(f.invocations, invocation)
	return f.result, f.err
}

type fakeRecommendationVerifier struct {
	output    FinalRecommendationOutput
	snapshots []RecommendationSnapshot
	err       error
}

func (f *fakeRecommendationVerifier) Verify(ctx context.Context, output FinalRecommendationOutput) ([]RecommendationSnapshot, error) {
	f.output = output
	return f.snapshots, f.err
}

type fakeRunStore struct {
	run                  Run
	startedSteps         []StepStart
	succeededSteps       []StepResult
	failedSteps          []StepFailure
	runSucceeded         *RunResult
	runFailed            *RunFailure
	savedRecommendations []RecommendationSnapshot
}

func newFakeRunStore() *fakeRunStore {
	return &fakeRunStore{run: Run{ID: 300, RunID: "run_1", SessionID: 100, UserID: 42, Status: RunRunning}}
}

func (f *fakeRunStore) CreateRun(ctx context.Context, command StartRunCommand) (Run, error) {
	f.run.RunID = command.RunID
	f.run.UserID = command.UserID
	f.run.UserInput = command.UserInput
	f.run.ModelName = command.ModelName
	f.run.PromptVersion = command.PromptVersion
	f.run.StartedAt = command.Now
	return f.run, nil
}

func (f *fakeRunStore) AppendStepStarted(ctx context.Context, start StepStart) (Step, error) {
	f.startedSteps = append(f.startedSteps, start)
	return Step{ID: uint64(400 + len(f.startedSteps)), RunDBID: start.RunDBID, StepNo: start.StepNo, StepType: start.StepType, ToolName: start.ToolName, Status: StepRunning}, nil
}

func (f *fakeRunStore) MarkStepSucceeded(ctx context.Context, result StepResult) error {
	f.succeededSteps = append(f.succeededSteps, result)
	return nil
}

func (f *fakeRunStore) MarkStepFailed(ctx context.Context, failure StepFailure) error {
	f.failedSteps = append(f.failedSteps, failure)
	return nil
}

func (f *fakeRunStore) MarkRunSucceeded(ctx context.Context, result RunResult) error {
	f.runSucceeded = &result
	return nil
}

func (f *fakeRunStore) MarkRunFailed(ctx context.Context, failure RunFailure) error {
	f.runFailed = &failure
	return nil
}

func (f *fakeRunStore) SaveRecommendations(ctx context.Context, runDBID uint64, items []RecommendationSnapshot) error {
	f.savedRecommendations = append(f.savedRecommendations, items...)
	return nil
}

func (f *fakeRunStore) stepTypes() []StepType {
	types := make([]StepType, 0, len(f.startedSteps))
	for _, step := range f.startedSteps {
		types = append(types, step.StepType)
	}
	return types
}
