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

type fakeRunStore struct {
	run            Run
	startedSteps   []StepStart
	succeededSteps []StepResult
	failedSteps    []StepFailure
	runSucceeded   *RunResult
	runFailed      *RunFailure
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

func (f *fakeRunStore) stepTypes() []StepType {
	types := make([]StepType, 0, len(f.startedSteps))
	for _, step := range f.startedSteps {
		types = append(types, step.StepType)
	}
	return types
}
