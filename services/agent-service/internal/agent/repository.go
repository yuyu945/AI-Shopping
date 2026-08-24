package agent

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

const insertAgentSession = `INSERT INTO agent_sessions (session_no, user_id, title, status) VALUES (?, ?, ?, ?)`
const insertAgentMessage = `INSERT INTO agent_messages (session_id, seq_no, role, content, model_name, prompt_version, token_usage_json) VALUES (?, ?, ?, ?, ?, ?, CAST(? AS JSON))`
const insertAgentRun = `INSERT INTO agent_runs (run_id, session_id, user_id, trace_id, user_input, status, model_name, prompt_version, started_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`
const insertAgentStepStarted = `INSERT INTO agent_steps (run_id, step_no, step_type, tool_name, attempt, input_json, status, started_at) VALUES (?, ?, ?, ?, ?, CAST(? AS JSON), ?, ?)`
const updateAgentRunStepCount = `UPDATE agent_runs SET step_count = GREATEST(step_count, ?) WHERE id = ?`
const updateAgentStepSucceeded = `UPDATE agent_steps SET status = 'SUCCEEDED', output_json = CAST(? AS JSON), latency_ms = ?, ended_at = ? WHERE id = ? AND status = 'RUNNING'`
const updateAgentStepFailed = `UPDATE agent_steps SET status = ?, error_code = ?, error_message = ?, latency_ms = ?, ended_at = ? WHERE id = ? AND status = 'RUNNING'`
const updateAgentRunSucceeded = `UPDATE agent_runs SET status = 'SUCCEEDED', final_result_json = CAST(? AS JSON), step_count = ?, ended_at = ?, error_code = NULL, error_message = NULL WHERE id = ? AND status = 'RUNNING'`
const updateAgentRunFailed = `UPDATE agent_runs SET status = ?, step_count = ?, ended_at = ?, error_code = ?, error_message = ? WHERE id = ? AND status = 'RUNNING'`
const queryAgentRunTimeline = `SELECT id, run_id, session_id, user_id, trace_id, user_input, status, model_name, prompt_version, step_count, final_result_json, error_code, error_message, started_at, ended_at, created_at FROM agent_runs WHERE user_id = ? AND run_id = ?`
const queryAgentTimelineSteps = `SELECT id, run_id, step_no, step_type, tool_name, attempt, input_json, output_json, status, error_code, error_message, latency_ms, started_at, ended_at FROM agent_steps WHERE run_id = ? ORDER BY step_no ASC, attempt ASC`
const insertRecommendationSnapshot = `INSERT INTO recommendations (run_id, rank_no, sku_id, product_id, product_title_snapshot, sku_code_snapshot, sku_spec_snapshot_json, price_snapshot, saleable_snapshot, discount_snapshot_json, reason, validation_status, created_at) VALUES (?, ?, ?, ?, ?, ?, CAST(? AS JSON), ?, ?, CAST(? AS JSON), ?, ?, ?)`
const queryRecommendationSnapshots = `SELECT id, run_id, rank_no, sku_id, product_id, product_title_snapshot, sku_code_snapshot, sku_spec_snapshot_json, price_snapshot, saleable_snapshot, discount_snapshot_json, reason, validation_status, created_at FROM recommendations WHERE run_id = ? ORDER BY rank_no ASC`

// ErrRunNotFound is returned when a run is missing or not owned by the user.
var ErrRunNotFound = errors.New("agent run not found")

// MySQLRepository persists Agent sessions, messages, runs, and steps.
type MySQLRepository struct {
	db *sql.DB
}

// NewMySQLRepository constructs an Agent repository backed by MySQL.
func NewMySQLRepository(db *sql.DB) *MySQLRepository {
	return &MySQLRepository{db: db}
}

// CreateRun creates a session, first user message, and run in one transaction.
func (r *MySQLRepository) CreateRun(ctx context.Context, command StartRunCommand) (run Run, err error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return Run{}, fmt.Errorf("begin agent run transaction: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	title := titleFromInput(command.UserInput)
	sessionResult, err := tx.ExecContext(ctx, insertAgentSession, command.SessionNo, command.UserID, title, SessionActive)
	if err != nil {
		return Run{}, fmt.Errorf("insert agent session: %w", err)
	}
	sessionID, err := lastInsertID(sessionResult, "agent session")
	if err != nil {
		return Run{}, err
	}
	if _, err = tx.ExecContext(ctx, insertAgentMessage, uint64(sessionID), uint32(1), MessageUser, command.UserInput, nil, nil, nil); err != nil {
		return Run{}, fmt.Errorf("insert agent user message: %w", err)
	}
	runResult, err := tx.ExecContext(ctx, insertAgentRun, command.RunID, uint64(sessionID), command.UserID, command.TraceID, command.UserInput, RunRunning, command.ModelName, command.PromptVersion, command.Now)
	if err != nil {
		return Run{}, fmt.Errorf("insert agent run: %w", err)
	}
	runID, err := lastInsertID(runResult, "agent run")
	if err != nil {
		return Run{}, err
	}
	if err = tx.Commit(); err != nil {
		return Run{}, fmt.Errorf("commit agent run transaction: %w", err)
	}
	return Run{
		ID: uint64(runID), RunID: command.RunID, SessionID: uint64(sessionID), UserID: command.UserID,
		TraceID: command.TraceID, UserInput: command.UserInput, Status: RunRunning,
		ModelName: command.ModelName, PromptVersion: command.PromptVersion, StartedAt: command.Now,
	}, nil
}

// AppendStepStarted inserts a RUNNING step and advances the parent run step count.
func (r *MySQLRepository) AppendStepStarted(ctx context.Context, start StepStart) (step Step, err error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return Step{}, fmt.Errorf("begin agent step transaction: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	result, err := tx.ExecContext(ctx, insertAgentStepStarted, start.RunDBID, start.StepNo, start.StepType, nullableString(start.ToolName), start.Attempt, []byte(start.InputJSON), StepRunning, start.StartedAt)
	if err != nil {
		return Step{}, fmt.Errorf("insert agent step: %w", err)
	}
	stepID, err := lastInsertID(result, "agent step")
	if err != nil {
		return Step{}, err
	}
	updateResult, updateErr := tx.ExecContext(ctx, updateAgentRunStepCount, start.StepNo, start.RunDBID)
	if err = requireOneRow(updateResult, updateErr, "update agent run step count"); err != nil {
		return Step{}, err
	}
	if err = tx.Commit(); err != nil {
		return Step{}, fmt.Errorf("commit agent step transaction: %w", err)
	}
	return Step{
		ID: uint64(stepID), RunDBID: start.RunDBID, StepNo: start.StepNo, StepType: start.StepType,
		ToolName: start.ToolName, Attempt: start.Attempt, InputJSON: copyJSON(start.InputJSON),
		Status: StepRunning, StartedAt: start.StartedAt,
	}, nil
}

// MarkStepSucceeded records successful step output.
func (r *MySQLRepository) MarkStepSucceeded(ctx context.Context, result StepResult) error {
	updateResult, err := r.db.ExecContext(ctx, updateAgentStepSucceeded, []byte(result.OutputJSON), result.LatencyMS, result.EndedAt, result.StepID)
	return requireOneRow(updateResult, err, "mark agent step succeeded")
}

// MarkStepFailed records terminal step failure.
func (r *MySQLRepository) MarkStepFailed(ctx context.Context, failure StepFailure) error {
	status := failure.Status
	if status == "" {
		status = StepFailed
	}
	updateResult, err := r.db.ExecContext(ctx, updateAgentStepFailed, status, failure.ErrorCode, failure.ErrorMessage, failure.LatencyMS, failure.EndedAt, failure.StepID)
	return requireOneRow(updateResult, err, "mark agent step failed")
}

// MarkRunSucceeded records a successful terminal run.
func (r *MySQLRepository) MarkRunSucceeded(ctx context.Context, result RunResult) error {
	updateResult, err := r.db.ExecContext(ctx, updateAgentRunSucceeded, []byte(result.FinalResultJSON), result.StepCount, result.EndedAt, result.RunDBID)
	return requireOneRow(updateResult, err, "mark agent run succeeded")
}

// MarkRunFailed records a failed or timed-out terminal run.
func (r *MySQLRepository) MarkRunFailed(ctx context.Context, failure RunFailure) error {
	status := failure.Status
	if status == "" {
		status = RunFailed
	}
	updateResult, err := r.db.ExecContext(ctx, updateAgentRunFailed, status, failure.StepCount, failure.EndedAt, failure.ErrorCode, failure.ErrorMessage, failure.RunDBID)
	return requireOneRow(updateResult, err, "mark agent run failed")
}

// SaveRecommendations persists backend-verified recommendation snapshots for a run.
func (r *MySQLRepository) SaveRecommendations(ctx context.Context, runDBID uint64, items []RecommendationSnapshot) (err error) {
	if len(items) == 0 {
		return nil
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin recommendation snapshot transaction: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	for _, item := range items {
		result, execErr := tx.ExecContext(
			ctx,
			insertRecommendationSnapshot,
			runDBID,
			item.RankNo,
			item.SKUID,
			item.ProductID,
			item.ProductTitleSnapshot,
			item.SKUCodeSnapshot,
			[]byte(item.SKUSpecSnapshotJSON),
			item.PriceSnapshot,
			item.SaleableSnapshot,
			[]byte(item.DiscountSnapshotJSON),
			item.Reason,
			item.ValidationStatus,
			item.CreatedAt,
		)
		if err = requireOneRow(result, execErr, "insert recommendation snapshot"); err != nil {
			return err
		}
	}
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("commit recommendation snapshot transaction: %w", err)
	}
	return nil
}

// ListRecommendations loads recommendation snapshots for a run in rank order.
func (r *MySQLRepository) ListRecommendations(ctx context.Context, runDBID uint64) ([]RecommendationSnapshot, error) {
	rows, err := r.db.QueryContext(ctx, queryRecommendationSnapshots, runDBID)
	if err != nil {
		return nil, fmt.Errorf("read recommendation snapshots: %w", err)
	}
	defer rows.Close()
	items := make([]RecommendationSnapshot, 0)
	for rows.Next() {
		item, err := scanRecommendation(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read recommendation snapshot rows: %w", err)
	}
	return items, nil
}

// GetRunTimeline loads an owned run with ordered steps.
func (r *MySQLRepository) GetRunTimeline(ctx context.Context, userID uint64, runID string) (RunTimeline, error) {
	run, err := scanRun(r.db.QueryRowContext(ctx, queryAgentRunTimeline, userID, runID))
	if errors.Is(err, sql.ErrNoRows) {
		return RunTimeline{}, ErrRunNotFound
	}
	if err != nil {
		return RunTimeline{}, fmt.Errorf("read agent run timeline: %w", err)
	}
	rows, err := r.db.QueryContext(ctx, queryAgentTimelineSteps, run.ID)
	if err != nil {
		return RunTimeline{}, fmt.Errorf("read agent timeline steps: %w", err)
	}
	defer rows.Close()
	steps := make([]Step, 0)
	for rows.Next() {
		step, err := scanStep(rows)
		if err != nil {
			return RunTimeline{}, err
		}
		steps = append(steps, step)
	}
	if err := rows.Err(); err != nil {
		return RunTimeline{}, fmt.Errorf("read agent timeline step rows: %w", err)
	}
	recommendations, err := r.ListRecommendations(ctx, run.ID)
	if err != nil {
		return RunTimeline{}, err
	}
	return RunTimeline{Run: run, Steps: steps, Recommendations: recommendations}, nil
}

type scanner interface {
	Scan(...any) error
}

func scanRun(row scanner) (Run, error) {
	var run Run
	var finalResult []byte
	var errorCode, errorMessage sql.NullString
	var endedAt sql.NullTime
	if err := row.Scan(
		&run.ID, &run.RunID, &run.SessionID, &run.UserID, &run.TraceID, &run.UserInput, &run.Status,
		&run.ModelName, &run.PromptVersion, &run.StepCount, &finalResult, &errorCode, &errorMessage,
		&run.StartedAt, &endedAt, &run.CreatedAt,
	); err != nil {
		return Run{}, err
	}
	run.FinalResultJSON = copyJSON(finalResult)
	if errorCode.Valid {
		run.ErrorCode = errorCode.String
	}
	if errorMessage.Valid {
		run.ErrorMessage = errorMessage.String
	}
	if endedAt.Valid {
		run.EndedAt = &endedAt.Time
	}
	return run, nil
}

func scanStep(row scanner) (Step, error) {
	var step Step
	var toolName, errorCode, errorMessage sql.NullString
	var inputJSON, outputJSON []byte
	var endedAt sql.NullTime
	if err := row.Scan(
		&step.ID, &step.RunDBID, &step.StepNo, &step.StepType, &toolName, &step.Attempt,
		&inputJSON, &outputJSON, &step.Status, &errorCode, &errorMessage, &step.LatencyMS,
		&step.StartedAt, &endedAt,
	); err != nil {
		return Step{}, fmt.Errorf("scan agent step: %w", err)
	}
	if toolName.Valid {
		step.ToolName = toolName.String
	}
	step.InputJSON = copyJSON(inputJSON)
	step.OutputJSON = copyJSON(outputJSON)
	if errorCode.Valid {
		step.ErrorCode = errorCode.String
	}
	if errorMessage.Valid {
		step.ErrorMessage = errorMessage.String
	}
	if endedAt.Valid {
		step.EndedAt = &endedAt.Time
	}
	return step, nil
}

func scanRecommendation(row scanner) (RecommendationSnapshot, error) {
	var item RecommendationSnapshot
	var specJSON, discountJSON []byte
	if err := row.Scan(
		&item.ID,
		&item.RunDBID,
		&item.RankNo,
		&item.SKUID,
		&item.ProductID,
		&item.ProductTitleSnapshot,
		&item.SKUCodeSnapshot,
		&specJSON,
		&item.PriceSnapshot,
		&item.SaleableSnapshot,
		&discountJSON,
		&item.Reason,
		&item.ValidationStatus,
		&item.CreatedAt,
	); err != nil {
		return RecommendationSnapshot{}, fmt.Errorf("scan recommendation snapshot: %w", err)
	}
	item.SKUSpecSnapshotJSON = copyJSON(specJSON)
	item.DiscountSnapshotJSON = copyJSON(discountJSON)
	return item, nil
}

func runTimelineColumns() []string {
	return []string{"id", "run_id", "session_id", "user_id", "trace_id", "user_input", "status", "model_name", "prompt_version", "step_count", "final_result_json", "error_code", "error_message", "started_at", "ended_at", "created_at"}
}

func stepColumns() []string {
	return []string{"id", "run_id", "step_no", "step_type", "tool_name", "attempt", "input_json", "output_json", "status", "error_code", "error_message", "latency_ms", "started_at", "ended_at"}
}

func recommendationColumns() []string {
	return []string{"id", "run_id", "rank_no", "sku_id", "product_id", "product_title_snapshot", "sku_code_snapshot", "sku_spec_snapshot_json", "price_snapshot", "saleable_snapshot", "discount_snapshot_json", "reason", "validation_status", "created_at"}
}

func titleFromInput(input string) string {
	title := strings.TrimSpace(input)
	runes := []rune(title)
	if len(runes) > 60 {
		return string(runes[:60])
	}
	return title
}

func nullableString(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}

func copyJSON(value []byte) []byte {
	if len(value) == 0 {
		return nil
	}
	return append([]byte(nil), value...)
}

func lastInsertID(result sql.Result, resource string) (int64, error) {
	id, err := result.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("read %s id: %w", resource, err)
	}
	return id, nil
}

func requireOneRow(result sql.Result, err error, operation string) error {
	if err != nil {
		return fmt.Errorf("%s: %w", operation, err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("%s rows affected: %w", operation, err)
	}
	if count != 1 {
		return fmt.Errorf("%s: row not found", operation)
	}
	return nil
}
