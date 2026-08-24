package agent

import (
	"context"
	"database/sql"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestRepositoryCreatesSessionRunAndUserMessage(t *testing.T) {
	db, mock := newRepositoryMock(t)
	now := time.Date(2026, 8, 23, 10, 0, 0, 0, time.UTC)
	repository := NewMySQLRepository(db)
	command := StartRunCommand{
		SessionNo: "sess_1", RunID: "run_1", UserID: 42, TraceID: "trace_1",
		UserInput: "预算 5000 买笔记本", ModelName: "qwen-plus", PromptVersion: "m4.1-v1", Now: now,
	}

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(insertAgentSession)).
		WithArgs(command.SessionNo, command.UserID, "预算 5000 买笔记本", SessionActive).
		WillReturnResult(sqlmock.NewResult(100, 1))
	mock.ExpectExec(regexp.QuoteMeta(insertAgentMessage)).
		WithArgs(uint64(100), uint32(1), MessageUser, command.UserInput, nil, nil, nil).
		WillReturnResult(sqlmock.NewResult(200, 1))
	mock.ExpectExec(regexp.QuoteMeta(insertAgentRun)).
		WithArgs(command.RunID, uint64(100), command.UserID, command.TraceID, command.UserInput, RunRunning, command.ModelName, command.PromptVersion, now).
		WillReturnResult(sqlmock.NewResult(300, 1))
	mock.ExpectCommit()

	run, err := repository.CreateRun(context.Background(), command)
	if err != nil {
		t.Fatal(err)
	}
	if run.ID != 300 || run.SessionID != 100 || run.RunID != "run_1" || run.Status != RunRunning {
		t.Fatalf("run=%#v", run)
	}
	assertSQLExpectations(t, mock)
}

func TestRepositoryAppendsStepsInOrder(t *testing.T) {
	db, mock := newRepositoryMock(t)
	now := time.Date(2026, 8, 23, 10, 1, 0, 0, time.UTC)
	repository := NewMySQLRepository(db)

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(insertAgentStepStarted)).
		WithArgs(uint64(300), uint32(1), StepTypeTool, "search_products", uint32(1), []byte(`{"keyword":"laptop"}`), StepRunning, now).
		WillReturnResult(sqlmock.NewResult(400, 1))
	mock.ExpectExec(regexp.QuoteMeta(updateAgentRunStepCount)).
		WithArgs(uint32(1), uint64(300)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	step, err := repository.AppendStepStarted(context.Background(), StepStart{
		RunDBID: 300, StepNo: 1, StepType: StepTypeTool, ToolName: "search_products",
		Attempt: 1, InputJSON: []byte(`{"keyword":"laptop"}`), StartedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if step.ID != 400 || step.StepNo != 1 || step.Status != StepRunning {
		t.Fatalf("step=%#v", step)
	}
	assertSQLExpectations(t, mock)
}

func TestRepositoryMarksRunAndStepTerminal(t *testing.T) {
	db, mock := newRepositoryMock(t)
	now := time.Date(2026, 8, 23, 10, 2, 0, 0, time.UTC)
	repository := NewMySQLRepository(db)

	mock.ExpectExec(regexp.QuoteMeta(updateAgentStepSucceeded)).
		WithArgs([]byte(`{"count":3}`), uint32(25), now, uint64(400)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta(updateAgentRunSucceeded)).
		WithArgs([]byte(`{"answer":"ok"}`), uint32(1), now, uint64(300)).
		WillReturnResult(sqlmock.NewResult(0, 1))

	if err := repository.MarkStepSucceeded(context.Background(), StepResult{StepID: 400, OutputJSON: []byte(`{"count":3}`), LatencyMS: 25, EndedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := repository.MarkRunSucceeded(context.Background(), RunResult{RunDBID: 300, FinalResultJSON: []byte(`{"answer":"ok"}`), StepCount: 1, EndedAt: now}); err != nil {
		t.Fatal(err)
	}
	assertSQLExpectations(t, mock)
}

func TestRepositorySavesRecommendationSnapshots(t *testing.T) {
	db, mock := newRepositoryMock(t)
	now := time.Date(2026, 8, 24, 10, 0, 0, 0, time.UTC)
	repository := NewMySQLRepository(db)

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(insertRecommendationSnapshot)).
		WithArgs(uint64(300), uint32(1), uint64(2001), uint64(1001), "轻薄笔记本", "LAPTOP-16G", []byte(`{"memory":"16G"}`), "4999.00", true, []byte(`[{"promotion_id":3001}]`), "适合编程", RecommendationVerified, now).
		WillReturnResult(sqlmock.NewResult(500, 1))
	mock.ExpectCommit()

	err := repository.SaveRecommendations(context.Background(), 300, []RecommendationSnapshot{{
		RunDBID: 300, RankNo: 1, SKUID: 2001, ProductID: 1001,
		ProductTitleSnapshot: "轻薄笔记本", SKUCodeSnapshot: "LAPTOP-16G",
		SKUSpecSnapshotJSON: []byte(`{"memory":"16G"}`), PriceSnapshot: "4999.00",
		SaleableSnapshot: true, DiscountSnapshotJSON: []byte(`[{"promotion_id":3001}]`),
		Reason: "适合编程", ValidationStatus: RecommendationVerified, CreatedAt: now,
	}})
	if err != nil {
		t.Fatal(err)
	}
	assertSQLExpectations(t, mock)
}

func TestRepositoryCompletesRunWithRecommendationSnapshotsAtomically(t *testing.T) {
	db, mock := newRepositoryMock(t)
	now := time.Date(2026, 8, 24, 10, 2, 0, 0, time.UTC)
	repository := NewMySQLRepository(db)

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(insertRecommendationSnapshot)).
		WithArgs(uint64(300), uint32(1), uint64(2001), uint64(1001), "轻薄笔记本", "LAPTOP-16G", []byte(`{"memory":"16G"}`), "4999.00", true, []byte(`[]`), "适合编程", RecommendationVerified, now).
		WillReturnResult(sqlmock.NewResult(500, 1))
	mock.ExpectExec(regexp.QuoteMeta(updateAgentRunSucceeded)).
		WithArgs([]byte(`{"recommendations":[{"sku_id":2001,"rank_no":1,"reason":"适合编程"}]}`), uint32(1), now, uint64(300)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	err := repository.CompleteRunWithRecommendations(context.Background(), RunResult{
		RunDBID: 300, FinalResultJSON: []byte(`{"recommendations":[{"sku_id":2001,"rank_no":1,"reason":"适合编程"}]}`),
		StepCount: 1, EndedAt: now,
	}, []RecommendationSnapshot{{
		RankNo: 1, SKUID: 2001, ProductID: 1001, ProductTitleSnapshot: "轻薄笔记本",
		SKUCodeSnapshot: "LAPTOP-16G", SKUSpecSnapshotJSON: []byte(`{"memory":"16G"}`),
		PriceSnapshot: "4999.00", SaleableSnapshot: true, DiscountSnapshotJSON: []byte(`[]`),
		Reason: "适合编程", ValidationStatus: RecommendationVerified, CreatedAt: now,
	}})
	if err != nil {
		t.Fatal(err)
	}
	assertSQLExpectations(t, mock)
}

func TestRepositoryLoadsRunTimelineForOwner(t *testing.T) {
	db, mock := newRepositoryMock(t)
	now := time.Date(2026, 8, 23, 10, 3, 0, 0, time.UTC)
	repository := NewMySQLRepository(db)

	mock.ExpectQuery(regexp.QuoteMeta(queryAgentRunTimeline)).
		WithArgs(uint64(42), "run_1").
		WillReturnRows(sqlmock.NewRows(runTimelineColumns()).AddRow(uint64(300), "run_1", uint64(100), uint64(42), "trace_1", "input", RunSucceeded, "qwen-plus", "m4.1-v1", uint32(1), []byte(`{"answer":"ok"}`), nil, nil, now, now, now))
	mock.ExpectQuery(regexp.QuoteMeta(queryAgentTimelineSteps)).
		WithArgs(uint64(300)).
		WillReturnRows(sqlmock.NewRows(stepColumns()).AddRow(uint64(400), uint64(300), uint32(1), StepTypeTool, sql.NullString{String: "search_products", Valid: true}, uint32(1), []byte(`{"keyword":"laptop"}`), []byte(`{"count":3}`), StepSucceeded, nil, nil, uint32(25), now, now))
	mock.ExpectQuery(regexp.QuoteMeta(queryRecommendationSnapshots)).
		WithArgs(uint64(300)).
		WillReturnRows(sqlmock.NewRows(recommendationColumns()))

	timeline, err := repository.GetRunTimeline(context.Background(), 42, "run_1")
	if err != nil {
		t.Fatal(err)
	}
	if timeline.Run.RunID != "run_1" || len(timeline.Steps) != 1 || timeline.Steps[0].ToolName != "search_products" {
		t.Fatalf("timeline=%#v", timeline)
	}
	assertSQLExpectations(t, mock)
}

func TestRepositoryLoadsRunTimelineWithRecommendations(t *testing.T) {
	db, mock := newRepositoryMock(t)
	now := time.Date(2026, 8, 24, 10, 1, 0, 0, time.UTC)
	repository := NewMySQLRepository(db)

	mock.ExpectQuery(regexp.QuoteMeta(queryAgentRunTimeline)).
		WithArgs(uint64(42), "run_1").
		WillReturnRows(sqlmock.NewRows(runTimelineColumns()).AddRow(uint64(300), "run_1", uint64(100), uint64(42), "trace_1", "input", RunSucceeded, "fake", "m4.2-v1", uint32(1), []byte(`{"recommendations":[{"sku_id":2001}]}`), nil, nil, now, now, now))
	mock.ExpectQuery(regexp.QuoteMeta(queryAgentTimelineSteps)).
		WithArgs(uint64(300)).
		WillReturnRows(sqlmock.NewRows(stepColumns()))
	mock.ExpectQuery(regexp.QuoteMeta(queryRecommendationSnapshots)).
		WithArgs(uint64(300)).
		WillReturnRows(sqlmock.NewRows(recommendationColumns()).AddRow(uint64(500), uint64(300), uint32(1), uint64(2001), uint64(1001), "轻薄笔记本", "LAPTOP-16G", []byte(`{"memory":"16G"}`), "4999.00", true, []byte(`[{"promotion_id":3001}]`), "适合编程", RecommendationVerified, now))

	timeline, err := repository.GetRunTimeline(context.Background(), 42, "run_1")
	if err != nil {
		t.Fatal(err)
	}
	if len(timeline.Recommendations) != 1 || timeline.Recommendations[0].SKUID != 2001 {
		t.Fatalf("timeline=%#v", timeline)
	}
	assertSQLExpectations(t, mock)
}

func TestRepositoryListsRunsOpsByStatusAndUser(t *testing.T) {
	db, mock := newRepositoryMock(t)
	now := time.Date(2026, 8, 24, 11, 0, 0, 0, time.UTC)
	repository := NewMySQLRepository(db)
	filter := RunOpsFilter{Status: RunFailed, UserID: 42, PageSize: 20}

	mock.ExpectQuery(regexp.QuoteMeta(queryListRunsOps(filter))).
		WithArgs(RunFailed, uint64(42), 20).
		WillReturnRows(sqlmock.NewRows(runTimelineColumns()).
			AddRow(uint64(300), "run_1", uint64(100), uint64(42), "trace_1", "input", RunFailed, "qwen-plus", "m4.2-v1", uint32(2), nil, "MODEL_FAILED", "model request failed", now, now, now))

	got, err := repository.ListRunsOps(context.Background(), filter)
	if err != nil || len(got.Runs) != 1 || got.Runs[0].TraceID != "trace_1" || got.Runs[0].ErrorCode != "MODEL_FAILED" {
		t.Fatalf("ListRunsOps() = %#v, %v", got, err)
	}
	assertSQLExpectations(t, mock)
}

func TestRepositoryLoadsRunOpsWithRedactedSteps(t *testing.T) {
	db, mock := newRepositoryMock(t)
	now := time.Date(2026, 8, 24, 11, 0, 0, 0, time.UTC)
	repository := NewMySQLRepository(db)

	mock.ExpectQuery(regexp.QuoteMeta(queryAgentRunOpsByID)).
		WithArgs("run_1").
		WillReturnRows(sqlmock.NewRows(runTimelineColumns()).
			AddRow(uint64(300), "run_1", uint64(100), uint64(42), "trace_1", "input", RunSucceeded, "qwen-plus", "m4.2-v1", uint32(1), []byte(`{"final_text":"ok"}`), nil, nil, now, now, now))
	mock.ExpectQuery(regexp.QuoteMeta(queryAgentTimelineSteps)).
		WithArgs(uint64(300)).
		WillReturnRows(sqlmock.NewRows(stepColumns()).
			AddRow(uint64(400), uint64(300), uint32(1), StepTypeTool, sql.NullString{String: "get_user_profile", Valid: true}, uint32(1), []byte(`{"phone":"13800000000"}`), []byte(`{"address":"secret","ok":true}`), StepSucceeded, nil, nil, uint32(25), now, now))
	mock.ExpectQuery(regexp.QuoteMeta(queryRecommendationSnapshots)).
		WithArgs(uint64(300)).
		WillReturnRows(sqlmock.NewRows(recommendationColumns()))

	got, err := repository.GetRunOps(context.Background(), "run_1")
	if err != nil || got.Run.TraceID != "trace_1" || len(got.Steps) != 1 {
		t.Fatalf("GetRunOps() = %#v, %v", got, err)
	}
	if string(got.Steps[0].InputJSON) != `{"phone":"[REDACTED]"}` || string(got.Steps[0].OutputJSON) != `{"address":"[REDACTED]","ok":true}` {
		t.Fatalf("step=%s %s", got.Steps[0].InputJSON, got.Steps[0].OutputJSON)
	}
	assertSQLExpectations(t, mock)
}

func TestRepositoryRejectsNonOwnerRunLookup(t *testing.T) {
	db, mock := newRepositoryMock(t)
	repository := NewMySQLRepository(db)
	mock.ExpectQuery(regexp.QuoteMeta(queryAgentRunTimeline)).
		WithArgs(uint64(99), "run_1").
		WillReturnError(sql.ErrNoRows)

	_, err := repository.GetRunTimeline(context.Background(), 99, "run_1")
	if err == nil {
		t.Fatal("GetRunTimeline() error = nil, want not found")
	}
	assertSQLExpectations(t, mock)
}

func newRepositoryMock(t *testing.T) (*sql.DB, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db, mock
}

func assertSQLExpectations(t *testing.T, mock sqlmock.Sqlmock) {
	t.Helper()
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
