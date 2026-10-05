package repository

import (
	"context"
	"database/sql/driver"
	"encoding/json"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

type pendingQualityStateArgument struct{}

func (pendingQualityStateArgument) Match(value driver.Value) bool {
	raw, ok := value.(string)
	if !ok {
		return false
	}
	var state qualityState
	if json.Unmarshal([]byte(raw), &state) != nil {
		return false
	}
	return state.Pending && state.Quality5xxEpisode == 1 && state.PreviousConcurrency != nil && *state.PreviousConcurrency == 20 && state.AppliedConcurrency != nil && *state.AppliedConcurrency == 4 && len(state.ModelRateLimits) == 0
}

func TestQuality5xxImmediatePersistsEpisodeWithoutTakingOrClearingProbeLease(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	plan := modelQualityPlan()
	plan.Enabled = true
	plan.ModelID = "alias"
	cfg, err := json.Marshal(plan.PelicanConfig)
	require.NoError(t, err)
	now, lease := time.Now(), time.Now().Add(time.Minute)
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT id,account_id,model_id.*FOR UPDATE").WithArgs(plan.ID, plan.AccountID).WillReturnRows(sqlmock.NewRows([]string{"id", "account_id", "model_id", "cron_expression", "enabled", "max_results", "auto_recover", "last_run_at", "next_run_at", "created_at", "updated_at", "pelican_config", "running_until"}).AddRow(plan.ID, plan.AccountID, plan.ModelID, plan.CronExpression, true, 100, false, nil, now.Add(time.Hour), now, now, cfg, lease))
	mock.ExpectQuery("SELECT type,status,schedulable.*FOR UPDATE").WithArgs(plan.AccountID).WillReturnRows(sqlmock.NewRows([]string{"type", "status", "schedulable"}).AddRow(service.AccountTypeOAuth, "active", true))
	mock.ExpectQuery("SELECT state FROM account_quality_states").WithArgs(plan.ID).WillReturnRows(sqlmock.NewRows([]string{"state"}))
	mock.ExpectQuery("SELECT EXISTS.*account_quality_states").WithArgs(plan.AccountID, plan.ID).WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))
	mock.ExpectQuery("SELECT platform,type,concurrency").WithArgs(plan.AccountID).WillReturnRows(sqlmock.NewRows([]string{"platform", "type", "concurrency", "credentials", "extra"}).AddRow(service.PlatformOpenAI, service.AccountTypeOAuth, 20, []byte(`{}`), []byte(`{}`)))
	mock.ExpectExec("UPDATE accounts SET extra").WithArgs(plan.AccountID, sqlmock.AnyArg(), 4).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("INSERT INTO scheduler_outbox").WithArgs(plan.AccountID).WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec("INSERT INTO account_quality_states").WithArgs(plan.ID, pendingQualityStateArgument{}).WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec("UPDATE scheduled_test_plans SET next_run_at=NOW").WithArgs(plan.ID).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	changed, err := (&scheduledTestPlanRepository{db: db}).applyImmediateQuality5xx(context.Background(), plan.ID, plan.AccountID)
	require.NoError(t, err)
	require.True(t, changed)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestQualityClaimCapturesEpisodeInLeaseTransaction(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	plan := modelQualityPlan()
	now, until := time.Now(), time.Now().Add(time.Minute)
	state, err := json.Marshal(qualityState{Action: service.QualityActionRemoveModel, Pending: true, Quality5xxEpisode: 9})
	require.NoError(t, err)
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT pg_try_advisory_xact_lock").WithArgs(plan.AccountID).WillReturnRows(sqlmock.NewRows([]string{"locked"}).AddRow(true))
	mock.ExpectExec("UPDATE scheduled_test_plans").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery("SELECT state FROM account_quality_states").WithArgs(plan.ID).WillReturnRows(sqlmock.NewRows([]string{"state"}).AddRow(state))
	mock.ExpectCommit()
	claimed, err := NewScheduledTestPlanRepository(db).ClaimPelican(context.Background(), plan, now, until, now.Add(time.Hour))
	require.NoError(t, err)
	require.True(t, claimed)
	require.Equal(t, int64(9), plan.Quality5xxEpisode)
	require.Equal(t, "upstream_5xx", plan.TriggerSource)
	require.NoError(t, mock.ExpectationsWereMet())
}
