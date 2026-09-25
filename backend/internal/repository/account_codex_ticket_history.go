package repository

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

func (r *accountRepository) RecordCodexTicketAttempt(ctx context.Context, id int64, attempt service.CodexTicketAttempt) error {
	if id <= 0 || attempt.ID == "" || attempt.StartedAt.IsZero() {
		return errors.New("invalid codex ticket attempt")
	}
	tx, err := r.client.Tx(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := appendCodexTicketAttempt(ctx, tx.Client(), id, attempt); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *accountRepository) MarkCodexTicketAttemptFailed(ctx context.Context, id int64, attemptID, model string, capturedAt time.Time, reason string) error {
	if id <= 0 || attemptID == "" || model == "" || capturedAt.IsZero() ||
		(reason != "model_quality_capability_failed" && reason != "model_quality_canary_failed" && reason != "model_quality_model_mismatch") {
		return errors.New("invalid codex ticket attempt failure")
	}
	tx, err := r.client.Tx(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	history, account, err := lockedCodexTicketHistory(ctx, tx.Client(), id)
	if err != nil {
		return err
	}
	changed, err := history.ReconcileQualityFailure(account, attemptID, model, capturedAt, reason)
	if err != nil {
		return err
	}
	if changed {
		if err := persistCodexTicketHistory(ctx, tx.Client(), id, history); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func appendCodexTicketAttempt(ctx context.Context, client *dbent.Client, id int64, attempt service.CodexTicketAttempt) error {
	history, account, err := lockedCodexTicketHistory(ctx, client, id)
	if err != nil {
		return err
	}
	if err := history.AppendWithQualityFailures(attempt, account); err != nil {
		return err
	}
	history.PruneCodexTicketHistory(time.Now())
	return persistCodexTicketHistory(ctx, client, id, history)
}

func lockedCodexTicketHistory(ctx context.Context, client *dbent.Client, id int64) (service.CodexTicketHistory, *service.Account, error) {
	var history service.CodexTicketHistory
	account := &service.Account{ID: id}
	rows, err := client.QueryContext(ctx,
		`SELECT extra FROM accounts WHERE id = $1 AND deleted_at IS NULL FOR NO KEY UPDATE`, id)
	if err != nil {
		return history, nil, err
	}
	var raw []byte
	if !rows.Next() {
		err = rows.Err()
		_ = rows.Close()
		if err != nil {
			return history, nil, err
		}
		return history, nil, service.ErrAccountNotFound
	}
	err = rows.Scan(&raw)
	_ = rows.Close()
	if err != nil {
		return history, nil, err
	}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &account.Extra); err != nil {
			return history, nil, err
		}
	}
	history, err = service.DecodeCodexTicketHistory(account.Extra[service.OpenAICodexTicketHistoryKey])
	return history, account, err
}

func persistCodexTicketHistory(ctx context.Context, client *dbent.Client, id int64, history service.CodexTicketHistory) error {
	encoded, err := json.Marshal(history)
	if err != nil {
		return err
	}
	_, err = client.ExecContext(ctx, `UPDATE accounts SET extra = COALESCE(extra, '{}'::jsonb) || jsonb_build_object($2::text, $3::jsonb) WHERE id = $1 AND deleted_at IS NULL`,
		id, service.OpenAICodexTicketHistoryKey, string(encoded))
	return err
}
