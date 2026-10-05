package repository

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func modelQualityPlan() *service.ScheduledTestPlan {
	return &service.ScheduledTestPlan{ID: 7, AccountID: 11, CronExpression: "*/30 * * * *", PelicanConfig: &service.PelicanTestConfig{Quality: &service.QualityPolicy{Action: service.QualityActionRemoveModel, RemoveModels: []string{"alias"}, TriggerOnUpstream5xx: true, RecoveryConcurrency: 4}}}
}

func TestQualityModelPendingCooldownAndGradualRecovery(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	a := &service.Account{Type: service.AccountTypeOAuth, Concurrency: 20, Extra: map[string]any{}, Credentials: map[string]any{"model_mapping": map[string]any{"alias": "target"}}}
	state, plan := qualityState{Quality5xxEpisode: 1}, modelQualityPlan()
	action, err := transitionQualityModels(a, &state, plan, "pending", now.Add(time.Hour), now)
	require.NoError(t, err)
	require.Equal(t, "probe_pending", action)
	require.Equal(t, 4, a.Concurrency)
	require.Empty(t, a.Extra["model_rate_limits"], "a 5xx is not a quality verdict")
	action, err = transitionQualityModels(a, &state, plan, "failed", now.Add(time.Hour), now)
	require.NoError(t, err)
	require.Equal(t, "models_cooled", action)
	limits := a.Extra["model_rate_limits"].(map[string]any)
	require.Contains(t, limits, "target")
	require.NotContains(t, limits, "other")
	action, err = transitionQualityModels(a, &state, plan, "passed", now.Add(time.Hour), now)
	require.NoError(t, err)
	require.Equal(t, "recovery_started", action)
	require.Empty(t, a.Extra["model_rate_limits"])
	require.Equal(t, 20, *state.RecoveryTarget)
	require.False(t, advanceQualityConcurrency(a, &state, 20), "late pre-quarantine requests cannot advance recovery")
	for i := 0; i < 16; i++ {
		require.True(t, advanceQualityConcurrency(a, &state, a.Concurrency))
	}
	require.Equal(t, 20, a.Concurrency)
	require.Nil(t, state.RecoveryTarget)
	require.False(t, advanceQualityConcurrency(a, &state, a.Concurrency))
}

func TestQualityModelRestorationPreservesNativeCooldownAndManualChanges(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	native := map[string]any{"reason": "upstream_429", "rate_limit_reset_at": now.Add(2 * time.Hour).Format(time.RFC3339)}
	a := &service.Account{Type: service.AccountTypeOAuth, Concurrency: 20, Extra: map[string]any{"model_rate_limits": map[string]any{"alias": native, "other": native}}}
	state, plan := qualityState{}, modelQualityPlan()
	_, err := transitionQualityModels(a, &state, plan, "failed", now.Add(time.Hour), now)
	require.NoError(t, err)
	_, err = transitionQualityModels(a, &state, plan, "passed", now.Add(time.Hour), now)
	require.NoError(t, err)
	limits := a.Extra["model_rate_limits"].(map[string]any)
	require.Equal(t, native, limits["alias"])
	require.Equal(t, native, limits["other"])
	a.Concurrency = 9
	require.False(t, advanceQualityConcurrency(a, &state, a.Concurrency))
	action, err := transitionQualityModels(a, &state, plan, "pending", now.Add(time.Hour), now)
	require.NoError(t, err)
	require.Equal(t, "restore_conflict", action)
	require.Equal(t, 9, a.Concurrency)
}

func TestQualityModelUnknownPeerDoesNotUnlockConcurrency(t *testing.T) {
	now := time.Now()
	a := &service.Account{Type: service.AccountTypeOAuth, Concurrency: 20, Extra: map[string]any{}}
	state, plan := qualityState{}, modelQualityPlan()
	plan.PelicanConfig.Quality.RemoveModels = []string{"alias", "other"}
	_, err := transitionQualityModels(a, &state, plan, "pending", now.Add(time.Hour), now)
	require.NoError(t, err)
	plan.QualityModelOutcomes = map[string]string{"alias": "passed", "other": "inconclusive"}
	_, err = transitionQualityModels(a, &state, plan, "inconclusive", now.Add(time.Hour), now)
	require.NoError(t, err)
	require.Nil(t, state.RecoveryTarget)
	require.Equal(t, 4, a.Concurrency)
}

func TestQualityModelAPIKeyRecoveryReleasesOwnershipWithoutChangingConcurrency(t *testing.T) {
	now := time.Now()
	a := &service.Account{Type: service.AccountTypeAPIKey, Concurrency: 12, Extra: map[string]any{}}
	state, plan := qualityState{}, modelQualityPlan()
	plan.PelicanConfig.Quality.TriggerOnUpstream5xx = false
	plan.PelicanConfig.Quality.AutoRestore = true
	_, err := transitionQualityModels(a, &state, plan, "failed", now.Add(time.Hour), now)
	require.NoError(t, err)
	require.Equal(t, 12, a.Concurrency)
	_, err = transitionQualityModels(a, &state, plan, "passed", now.Add(time.Hour), now)
	require.NoError(t, err)
	require.Empty(t, state.Action)
	require.Equal(t, 12, a.Concurrency)
	require.Empty(t, a.Extra["model_rate_limits"])
}

func TestQualityNew5xxEpisodeFencesOldSuccessfulProbe(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	plan := modelQualityPlan()
	plan.UpdatedAt, plan.Quality5xxEpisode = time.Now(), 1
	until := time.Now().Add(time.Minute)
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT enabled AND updated_at").WillReturnRows(sqlmock.NewRows([]string{"valid"}).AddRow(true))
	mock.ExpectQuery("SELECT updated_at, schedulable, status").WillReturnRows(sqlmock.NewRows([]string{"updated_at", "schedulable", "status"}).AddRow(time.Now(), true, "active"))
	raw, err := json.Marshal(qualityState{Quality5xxEpisode: 2, Action: service.QualityActionRemoveModel, Pending: true})
	require.NoError(t, err)
	mock.ExpectQuery("SELECT state FROM account_quality_states").WillReturnRows(sqlmock.NewRows([]string{"state"}).AddRow(raw))
	mock.ExpectRollback()
	action, err := NewScheduledTestPlanRepository(db).ApplyQualityOutcome(context.Background(), plan, until, "passed")
	require.NoError(t, err)
	require.Equal(t, "stale_run", action)
	require.NoError(t, mock.ExpectationsWereMet())
}
