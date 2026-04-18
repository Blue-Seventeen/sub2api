package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
)

type accountAutoOpsRepository struct {
	db *sql.DB
}

func NewAccountAutoOpsRepository(db *sql.DB) service.AccountAutoOpsRepository {
	return &accountAutoOpsRepository{db: db}
}

func (r *accountAutoOpsRepository) CreateRun(ctx context.Context, run *service.AccountAutoOpsRun) (*service.AccountAutoOpsRun, error) {
	requestedJSON, err := json.Marshal(run.RequestedAccountIDs)
	if err != nil {
		return nil, err
	}
	row := r.db.QueryRowContext(ctx, `
		INSERT INTO account_auto_ops_runs (
			trigger_mode, status, requested_account_ids, total_accounts, eligible_accounts, completed_accounts, error_message, started_at, created_at, updated_at
		) VALUES ($1, $2, $3::jsonb, $4, $5, $6, $7, $8, NOW(), NOW())
		RETURNING id, trigger_mode, status, requested_account_ids, total_accounts, eligible_accounts, completed_accounts, error_message, started_at, finished_at, created_at, updated_at
	`, run.TriggerMode, run.Status, string(requestedJSON), run.TotalAccounts, run.EligibleAccounts, run.CompletedAccounts, run.ErrorMessage, run.StartedAt)
	return scanAccountAutoOpsRun(row)
}

func (r *accountAutoOpsRepository) FinishRun(ctx context.Context, runID int64, status string, totalAccounts, eligibleAccounts, completedAccounts int, errorMessage string, finishedAt time.Time) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE account_auto_ops_runs
		SET status = $2,
			total_accounts = $3,
			eligible_accounts = $4,
			completed_accounts = $5,
			error_message = $6,
			finished_at = $7,
			updated_at = NOW()
		WHERE id = $1
	`, runID, status, totalAccounts, eligibleAccounts, completedAccounts, errorMessage, finishedAt)
	return err
}

func (r *accountAutoOpsRepository) CreateStep(ctx context.Context, step *service.AccountAutoOpsStep) (*service.AccountAutoOpsStep, error) {
	row := r.db.QueryRowContext(ctx, `
		INSERT INTO account_auto_ops_steps (
			run_id, account_id, account_name, step_index, subject, action, status, matched_rule_id, matched_rule_name, response_text, response_hash, action_result_text, created_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, NOW())
		RETURNING id, run_id, account_id, account_name, step_index, subject, action, status, matched_rule_id, matched_rule_name, response_text, response_hash, action_result_text, created_at
	`, step.RunID, step.AccountID, step.AccountName, step.StepIndex, step.Subject, step.Action, step.Status, step.MatchedRuleID, step.MatchedRuleName, step.ResponseText, step.ResponseHash, step.ActionResultText)
	return scanAccountAutoOpsStep(row)
}

func (r *accountAutoOpsRepository) ListRuns(ctx context.Context, since time.Time, limit int) ([]*service.AccountAutoOpsRun, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, trigger_mode, status, requested_account_ids, total_accounts, eligible_accounts, completed_accounts, error_message, started_at, finished_at, created_at, updated_at
		FROM account_auto_ops_runs
		WHERE started_at >= $1
		ORDER BY started_at DESC
		LIMIT $2
	`, since, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var runs []*service.AccountAutoOpsRun
	for rows.Next() {
		run, scanErr := scanAccountAutoOpsRun(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		runs = append(runs, run)
	}
	return runs, rows.Err()
}

func (r *accountAutoOpsRepository) ListStepsByRunIDs(ctx context.Context, runIDs []int64) ([]*service.AccountAutoOpsStep, error) {
	if len(runIDs) == 0 {
		return []*service.AccountAutoOpsStep{}, nil
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, run_id, account_id, account_name, step_index, subject, action, status, matched_rule_id, matched_rule_name, response_text, response_hash, action_result_text, created_at
		FROM account_auto_ops_steps
		WHERE run_id = ANY($1)
		ORDER BY run_id DESC, account_id ASC, step_index ASC, id ASC
	`, pq.Array(runIDs))
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var steps []*service.AccountAutoOpsStep
	for rows.Next() {
		step, scanErr := scanAccountAutoOpsStep(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		steps = append(steps, step)
	}
	return steps, rows.Err()
}

func (r *accountAutoOpsRepository) ListSamples(ctx context.Context, since time.Time, limit int) ([]*service.AccountAutoOpsSample, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT subject, response_hash, MAX(response_text) AS response_text, COUNT(*)::INT AS occurrences, MAX(created_at) AS last_seen_at
		FROM account_auto_ops_steps
		WHERE created_at >= $1
		  AND subject IN ($2, $3)
		  AND response_hash <> ''
		  AND response_text <> ''
		GROUP BY subject, response_hash
		ORDER BY MAX(created_at) DESC
		LIMIT $4
	`, since, service.AccountAutoOpsSubjectTestResponse, service.AccountAutoOpsSubjectRefreshResponse, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var samples []*service.AccountAutoOpsSample
	for rows.Next() {
		item := &service.AccountAutoOpsSample{}
		if scanErr := rows.Scan(&item.Subject, &item.ResponseHash, &item.ResponseText, &item.Occurrences, &item.LastSeenAt); scanErr != nil {
			return nil, scanErr
		}
		samples = append(samples, item)
	}
	return samples, rows.Err()
}

func (r *accountAutoOpsRepository) DeleteOlderThan(ctx context.Context, cutoff time.Time) error {
	if _, err := r.db.ExecContext(ctx, `DELETE FROM account_auto_ops_runs WHERE started_at < $1`, cutoff); err != nil {
		return err
	}
	_, err := r.db.ExecContext(ctx, `DELETE FROM account_auto_ops_steps WHERE created_at < $1 AND run_id NOT IN (SELECT id FROM account_auto_ops_runs)`, cutoff)
	return err
}

func (r *accountAutoOpsRepository) GetLatestStartedAtByTrigger(ctx context.Context, triggerMode string) (*time.Time, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT started_at
		FROM account_auto_ops_runs
		WHERE trigger_mode = $1
		ORDER BY started_at DESC
		LIMIT 1
	`, triggerMode)
	var startedAt time.Time
	if err := row.Scan(&startedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &startedAt, nil
}

type accountAutoOpsScannable interface {
	Scan(dest ...any) error
}

func scanAccountAutoOpsRun(row accountAutoOpsScannable) (*service.AccountAutoOpsRun, error) {
	run := &service.AccountAutoOpsRun{}
	var requestedRaw string
	if err := row.Scan(
		&run.ID,
		&run.TriggerMode,
		&run.Status,
		&requestedRaw,
		&run.TotalAccounts,
		&run.EligibleAccounts,
		&run.CompletedAccounts,
		&run.ErrorMessage,
		&run.StartedAt,
		&run.FinishedAt,
		&run.CreatedAt,
		&run.UpdatedAt,
	); err != nil {
		return nil, err
	}
	requestedRaw = strings.TrimSpace(requestedRaw)
	if requestedRaw != "" {
		_ = json.Unmarshal([]byte(requestedRaw), &run.RequestedAccountIDs)
	}
	if run.RequestedAccountIDs == nil {
		run.RequestedAccountIDs = []int64{}
	}
	return run, nil
}

func scanAccountAutoOpsStep(row accountAutoOpsScannable) (*service.AccountAutoOpsStep, error) {
	step := &service.AccountAutoOpsStep{}
	if err := row.Scan(
		&step.ID,
		&step.RunID,
		&step.AccountID,
		&step.AccountName,
		&step.StepIndex,
		&step.Subject,
		&step.Action,
		&step.Status,
		&step.MatchedRuleID,
		&step.MatchedRuleName,
		&step.ResponseText,
		&step.ResponseHash,
		&step.ActionResultText,
		&step.CreatedAt,
	); err != nil {
		return nil, err
	}
	return step, nil
}

func (r *accountAutoOpsRepository) String() string {
	return fmt.Sprintf("accountAutoOpsRepository")
}
