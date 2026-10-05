package repository

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

func cooldownEntryEqual(current any, applied json.RawMessage) bool {
	raw, err := json.Marshal(current)
	if err != nil {
		return false
	}
	var decoded any
	if json.Unmarshal(applied, &decoded) != nil {
		return false
	}
	expected, _ := json.Marshal(decoded)
	return bytes.Equal(raw, expected)
}

func cooldownEntryUntil(entry any) time.Time {
	value, ok := entry.(map[string]any)
	if !ok {
		return time.Time{}
	}
	raw, _ := value["rate_limit_reset_at"].(string)
	until, _ := time.Parse(time.RFC3339, raw)
	return until
}

func lowerQualityRecoveryConcurrency(account *service.Account, state *qualityState, cap int) bool {
	if account.Type != service.AccountTypeOAuth {
		return true
	}
	if state.AppliedConcurrency != nil && account.Concurrency != *state.AppliedConcurrency {
		return false
	}
	if cap <= 0 {
		cap = 5
	}
	if state.PreviousConcurrency == nil {
		previous := account.Concurrency
		if state.RecoveryTarget != nil {
			previous = *state.RecoveryTarget
		}
		state.PreviousConcurrency = &previous
	}
	state.RecoveryTarget = nil
	applied := account.Concurrency
	if applied > cap {
		applied = cap
	}
	state.AppliedConcurrency, account.Concurrency = &applied, applied
	return true
}

func applyQualityFailedModel(account *service.Account, state *qualityState, model string, until, now time.Time, planID int64) (string, error) {
	if account.Extra == nil {
		account.Extra = map[string]any{}
	}
	limits, _ := account.Extra["model_rate_limits"].(map[string]any)
	if limits == nil {
		limits = map[string]any{}
	}
	key := account.GetMappedModel(model)
	if previous, owned := state.ModelApplied[key]; owned && !cooldownEntryEqual(limits[key], previous) {
		return "action_conflict", nil
	}
	if state.ModelRateLimits == nil {
		state.ModelRateLimits, state.ModelApplied = map[string]json.RawMessage{}, map[string]json.RawMessage{}
	}
	if _, owned := state.ModelRateLimits[key]; !owned {
		raw, err := json.Marshal(limits[key])
		if err != nil {
			return "", err
		}
		state.ModelRateLimits[key] = raw
	}
	if previous := cooldownEntryUntil(limits[key]); previous.After(until) {
		until = previous
	}
	entry := map[string]any{"rate_limited_at": now.UTC().Format(time.RFC3339), "rate_limit_reset_at": until.UTC().Format(time.RFC3339), "reason": fmt.Sprintf("quality_rule:%d", planID)}
	raw, err := json.Marshal(entry)
	if err != nil {
		return "", err
	}
	state.Action, limits[key], state.ModelApplied[key] = service.QualityActionRemoveModel, entry, raw
	account.Extra["model_rate_limits"] = limits
	return "models_cooled", nil
}

func restoreQualityModel(account *service.Account, state *qualityState, model string, now time.Time) (string, error) {
	key := account.GetMappedModel(model)
	previous, owned := state.ModelRateLimits[key]
	if !owned {
		return "passed", nil
	}
	limits, _ := account.Extra["model_rate_limits"].(map[string]any)
	var applied any
	if err := json.Unmarshal(state.ModelApplied[key], &applied); err != nil {
		return "", err
	}
	if !cooldownEntryEqual(limits[key], state.ModelApplied[key]) && (limits[key] != nil || cooldownEntryUntil(applied).After(now)) {
		return "restore_conflict", nil
	}
	var original any
	if err := json.Unmarshal(previous, &original); err != nil {
		return "", err
	}
	if cooldownEntryUntil(original).After(now) {
		if limits == nil {
			limits = map[string]any{}
		}
		limits[key] = original
	} else {
		delete(limits, key)
	}
	delete(state.ModelRateLimits, key)
	delete(state.ModelApplied, key)
	if len(limits) == 0 {
		delete(account.Extra, "model_rate_limits")
	} else {
		account.Extra["model_rate_limits"] = limits
	}
	return "models_partially_restored", nil
}

func transitionQualityModels(account *service.Account, state *qualityState, plan *service.ScheduledTestPlan, outcome string, until, now time.Time) (string, error) {
	q := plan.PelicanConfig.Quality
	action := "inconclusive"
	if state.AppliedConcurrency != nil && account.Concurrency != *state.AppliedConcurrency {
		return "restore_conflict", nil
	}
	if outcome == "pending" {
		if !lowerQualityRecoveryConcurrency(account, state, q.RecoveryConcurrency) {
			return "action_conflict", nil
		}
		state.Action, state.Pending = service.QualityActionRemoveModel, true
		return "probe_pending", nil
	}
	modelActions := map[string]string{}
	for _, model := range q.RemoveModels {
		verdict := outcome
		if plan.QualityModelOutcomes != nil {
			verdict = plan.QualityModelOutcomes[model]
		}
		var err error
		modelAction := verdict
		if verdict == "failed" {
			if !lowerQualityRecoveryConcurrency(account, state, q.RecoveryConcurrency) {
				return "action_conflict", nil
			}
			modelAction, err = applyQualityFailedModel(account, state, model, until, now, plan.ID)
		} else if verdict == "passed" && (q.AutoRestore || q.TriggerOnUpstream5xx) {
			modelAction, err = restoreQualityModel(account, state, model, now)
		}
		if err != nil {
			return "", err
		}
		if modelAction == "restore_conflict" || modelAction == "action_conflict" {
			return modelAction, nil
		}
		modelActions[model] = modelAction
		if modelAction == "models_cooled" || modelAction == "models_partially_restored" {
			action = modelAction
		}
	}
	plan.QualityModelActions, state.Pending = modelActions, false
	if outcome == "passed" && len(state.ModelRateLimits) == 0 && (q.AutoRestore || q.TriggerOnUpstream5xx) {
		return restoreQualityConcurrency(account, state, q.TriggerOnUpstream5xx), nil
	}
	return action, nil
}

func restoreQualityConcurrency(account *service.Account, state *qualityState, gradual bool) string {
	if state.PreviousConcurrency == nil || state.AppliedConcurrency == nil {
		state.Action = ""
		return "passed"
	}
	if account.Concurrency != *state.AppliedConcurrency {
		return "restore_conflict"
	}
	if gradual && *state.PreviousConcurrency > account.Concurrency {
		target := *state.PreviousConcurrency
		state.RecoveryTarget = &target
		return "recovery_started"
	}
	account.Concurrency = *state.PreviousConcurrency
	state.Action, state.PreviousConcurrency, state.AppliedConcurrency, state.RecoveryTarget = "", nil, nil, nil
	return "restored"
}

func qualityUpsertState(ctx context.Context, tx *sql.Tx, planID int64, state qualityState) error {
	if state.Action == "" && state.RecoveryTarget == nil {
		_, err := tx.ExecContext(ctx, `DELETE FROM account_quality_states WHERE plan_id=$1`, planID)
		return err
	}
	raw, err := json.Marshal(state)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO account_quality_states(plan_id,state) VALUES($1,$2::jsonb) ON CONFLICT(plan_id) DO UPDATE SET state=EXCLUDED.state`, planID, string(raw))
	return err
}

func qualityLoadAccount(ctx context.Context, tx *sql.Tx, id int64) (*service.Account, error) {
	account := &service.Account{ID: id}
	var credentials, extra []byte
	err := tx.QueryRowContext(ctx, `SELECT platform,type,concurrency,COALESCE(credentials,'{}'::jsonb),COALESCE(extra,'{}'::jsonb) FROM accounts WHERE id=$1`, id).Scan(&account.Platform, &account.Type, &account.Concurrency, &credentials, &extra)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(credentials, &account.Credentials); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(extra, &account.Extra); err != nil {
		return nil, err
	}
	return account, nil
}

func qualitySaveAccount(ctx context.Context, tx *sql.Tx, account *service.Account) error {
	service.BoundAccountProtectionConcurrency(account)
	raw, err := json.Marshal(account.Extra)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE accounts SET extra=$2::jsonb,concurrency=$3,updated_at=GREATEST(clock_timestamp(),updated_at+interval '1 microsecond') WHERE id=$1`, account.ID, string(raw), account.Concurrency)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO scheduler_outbox(event_type,account_id,payload) VALUES('account_changed',$1,'{}')`, account.ID)
	return err
}

func applyQualityModelOutcome(ctx context.Context, tx *sql.Tx, plan *service.ScheduledTestPlan, state qualityState, outcome string) (string, error) {
	account, err := qualityLoadAccount(ctx, tx, plan.AccountID)
	if err != nil {
		return "", err
	}
	before, err := json.Marshal(account)
	if err != nil {
		return "", err
	}
	until, err := scheduledQualityNext(plan.CronExpression, time.Now())
	if err != nil {
		return "", err
	}
	action, err := transitionQualityModels(account, &state, plan, outcome, until, time.Now())
	if err != nil || action == "restore_conflict" || action == "action_conflict" {
		return action, err
	}
	after, err := json.Marshal(account)
	if err != nil {
		return "", err
	}
	if !bytes.Equal(before, after) {
		if err := qualitySaveAccount(ctx, tx, account); err != nil {
			return "", err
		}
	}
	if err := qualityUpsertState(ctx, tx, plan.ID, state); err != nil {
		return "", err
	}
	return action, nil
}
