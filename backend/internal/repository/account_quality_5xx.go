package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/robfig/cron/v3"
)

func scheduledQualityNext(expression string, now time.Time) (time.Time, error) {
	schedule, err := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow).Parse(expression)
	if err != nil {
		return time.Time{}, err
	}
	return schedule.Next(now), nil
}

func qualityReadState(ctx context.Context, tx *sql.Tx, id int64) (qualityState, error) {
	state := qualityState{}
	var raw []byte
	err := tx.QueryRowContext(ctx, `SELECT state FROM account_quality_states WHERE plan_id=$1`, id).Scan(&raw)
	if err == sql.ErrNoRows {
		return state, nil
	}
	if err != nil {
		return state, err
	}
	if err := json.Unmarshal(raw, &state); err != nil {
		return state, err
	}
	return state, nil
}

// Lock ordering matches the probe transition: plan, then account.
func qualityLockPlanAccount(ctx context.Context, tx *sql.Tx, planID, accountID int64) (*service.ScheduledTestPlan, bool, error) {
	plan, err := scanPlan(tx.QueryRowContext(ctx, `SELECT id,account_id,model_id,cron_expression,enabled,max_results,auto_recover,last_run_at,next_run_at,created_at,updated_at,pelican_config,running_until FROM scheduled_test_plans WHERE id=$1 AND account_id=$2 FOR UPDATE`, planID, accountID))
	if err == sql.ErrNoRows {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if !plan.Enabled || plan.PelicanConfig == nil || plan.PelicanConfig.Quality == nil {
		return plan, false, nil
	}
	q := plan.PelicanConfig.Quality
	if !q.TriggerOnUpstream5xx || q.Action != service.QualityActionRemoveModel {
		return plan, false, nil
	}
	var kind, status string
	var enabled bool
	err = tx.QueryRowContext(ctx, `SELECT type,status,schedulable FROM accounts WHERE id=$1 AND deleted_at IS NULL FOR UPDATE`, accountID).Scan(&kind, &status, &enabled)
	if err == sql.ErrNoRows {
		return plan, false, nil
	}
	return plan, err == nil && kind == service.AccountTypeOAuth && status == "active" && enabled, err
}

func qualityCandidatePlans(ctx context.Context, db *sql.DB, id int64, recovering bool) ([]int64, error) {
	query := `SELECT p.id FROM scheduled_test_plans p WHERE p.account_id=$1 AND p.enabled AND p.pelican_config->'quality'->>'trigger_on_upstream_5xx'='true' AND p.pelican_config->'quality'->>'action'='remove_models'`
	if recovering {
		query += ` AND EXISTS(SELECT 1 FROM account_quality_states s WHERE s.plan_id=p.id AND (s.state->>'recovery_target')::int>0)`
	}
	rows, err := db.QueryContext(ctx, query, id)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	ids := []int64{}
	for rows.Next() {
		var planID int64
		if err := rows.Scan(&planID); err != nil {
			return nil, err
		}
		ids = append(ids, planID)
	}
	return ids, rows.Err()
}

func (r *accountRepository) qualityDatabase() (*sql.DB, error) {
	db, ok := r.sql.(*sql.DB)
	if !ok {
		return nil, fmt.Errorf("quality protection requires a transactional database")
	}
	return db, nil
}

func (r *accountRepository) ApplyQuality5xx(ctx context.Context, accountID int64) error {
	db, err := r.qualityDatabase()
	if err != nil {
		return err
	}
	ids, err := qualityCandidatePlans(ctx, db, accountID, false)
	if err != nil {
		return err
	}
	plans := &scheduledTestPlanRepository{db: db}
	for _, id := range ids {
		changed, err := plans.applyImmediateQuality5xx(ctx, id, accountID)
		if err != nil {
			return err
		}
		if changed {
			r.syncSchedulerAccountSnapshot(ctx, accountID)
		}
	}
	return nil
}

func (r *scheduledTestPlanRepository) applyImmediateQuality5xx(ctx context.Context, planID, accountID int64) (bool, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()
	plan, valid, err := qualityLockPlanAccount(ctx, tx, planID, accountID)
	if err != nil || !valid {
		return false, err
	}
	state, err := qualityReadState(ctx, tx, planID)
	if err != nil {
		return false, err
	}
	if state.Action != "" && state.Action != service.QualityActionRemoveModel {
		return false, nil
	}
	var otherOwner bool
	err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM account_quality_states s JOIN scheduled_test_plans p ON p.id=s.plan_id WHERE p.account_id=$1 AND p.id<>$2 AND COALESCE(s.state->>'action','')<>'')`, accountID, planID).Scan(&otherOwner)
	if err != nil || otherOwner {
		return false, err
	}
	state.Quality5xxEpisode++
	action, err := applyQualityModelOutcome(ctx, tx, plan, state, "pending")
	if err != nil || action != "probe_pending" {
		return false, err
	}
	// Persisted due time bridges serving replicas and the existing leased worker.
	if _, err := tx.ExecContext(ctx, `UPDATE scheduled_test_plans SET next_run_at=NOW() WHERE id=$1`, planID); err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}

func (r *accountRepository) RecordQualityRecoverySuccess(ctx context.Context, accountID int64, observedConcurrency int) error {
	db, err := r.qualityDatabase()
	if err != nil {
		return err
	}
	ids, err := qualityCandidatePlans(ctx, db, accountID, true)
	if err != nil {
		return err
	}
	for _, id := range ids {
		changed, err := qualityAdvanceConcurrency(ctx, db, id, accountID, observedConcurrency)
		if err != nil {
			return err
		}
		if changed {
			r.syncSchedulerAccountSnapshot(ctx, accountID)
		}
	}
	return nil
}

func advanceQualityConcurrency(account *service.Account, state *qualityState, observedConcurrency int) bool {
	if state.Pending || state.RecoveryTarget == nil || state.AppliedConcurrency == nil || len(state.ModelRateLimits) > 0 || account.Concurrency != *state.AppliedConcurrency || observedConcurrency != account.Concurrency {
		return false
	}
	if account.Concurrency >= *state.RecoveryTarget {
		return false
	}
	account.Concurrency++
	current := account.Concurrency
	state.AppliedConcurrency = &current
	if current >= *state.RecoveryTarget {
		state.Action, state.PreviousConcurrency, state.AppliedConcurrency, state.RecoveryTarget = "", nil, nil, nil
	}
	return true
}

func qualityAdvanceConcurrency(ctx context.Context, db *sql.DB, planID, accountID int64, observedConcurrency int) (bool, error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()
	_, valid, err := qualityLockPlanAccount(ctx, tx, planID, accountID)
	if err != nil || !valid {
		return false, err
	}
	state, err := qualityReadState(ctx, tx, planID)
	if err != nil {
		return false, err
	}
	account, err := qualityLoadAccount(ctx, tx, accountID)
	if err != nil {
		return false, err
	}
	if !advanceQualityConcurrency(account, &state, observedConcurrency) {
		return false, nil
	}
	if err := qualitySaveAccount(ctx, tx, account); err != nil {
		return false, err
	}
	if err := qualityUpsertState(ctx, tx, planID, state); err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}
