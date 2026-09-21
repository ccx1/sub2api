package repository

import (
	"context"
	"encoding/json"
	"errors"

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

func appendCodexTicketAttempt(ctx context.Context, client *dbent.Client, id int64, attempt service.CodexTicketAttempt) error {
	rows, err := client.QueryContext(ctx,
		`SELECT extra -> $2 FROM accounts WHERE id = $1 AND deleted_at IS NULL FOR NO KEY UPDATE`, id, service.OpenAICodexTicketHistoryKey)
	if err != nil {
		return err
	}
	var raw []byte
	if !rows.Next() {
		err = rows.Err()
		_ = rows.Close()
		if err != nil {
			return err
		}
		return service.ErrAccountNotFound
	}
	err = rows.Scan(&raw)
	_ = rows.Close()
	if err != nil {
		return err
	}
	var value any
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &value); err != nil {
			return err
		}
	}
	history, err := service.DecodeCodexTicketHistory(value)
	if err != nil {
		return err
	}
	history.Append(attempt)
	encoded, err := json.Marshal(history)
	if err != nil {
		return err
	}
	_, err = client.ExecContext(ctx, `UPDATE accounts SET extra = COALESCE(extra, '{}'::jsonb) || jsonb_build_object($2::text, $3::jsonb) WHERE id = $1 AND deleted_at IS NULL`,
		id, service.OpenAICodexTicketHistoryKey, string(encoded))
	return err
}
