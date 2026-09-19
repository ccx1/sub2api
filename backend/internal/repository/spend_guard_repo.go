package repository

import (
	"context"
	"database/sql"
	"errors"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
)

var _ service.SpendGuardRepository = (*apiKeyRepository)(nil)

func (r *apiKeyRepository) IsAPIKeySpendGuardFrozen(ctx context.Context, id int64) (bool, error) {
	rows, err := r.sql.QueryContext(ctx, `SELECT EXISTS (
		SELECT 1 FROM api_key_spend_guard_freezes WHERE api_key_id = $1 AND released_at IS NULL
	)`, id)
	if err != nil {
		return false, err
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		return false, rows.Err()
	}
	var frozen bool
	if err := rows.Scan(&frozen); err != nil {
		return false, err
	}
	return frozen, rows.Err()
}

func translateSpendGuardUpdateError(err error) error {
	var pgErr *pq.Error
	if errors.As(err, &pgErr) && pgErr.Constraint == "api_key_spend_guard_frozen" {
		return service.ErrAPIKeySpendGuardFrozen
	}
	return err
}

const spendGuardOffendersSQL = `
WITH recent AS (
	SELECT ul.api_key_id AS key_id, COUNT(*) AS reqs,
		SUM(ul.input_tokens::bigint + ul.output_tokens::bigint + ul.cache_read_tokens::bigint + ul.cache_creation_tokens::bigint) AS toks,
		0 AS errs
	FROM usage_logs ul
	LEFT JOIN api_key_spend_guard_freezes f ON f.api_key_id = ul.api_key_id
	WHERE ul.created_at >= NOW() - ($1 * INTERVAL '1 minute')
		AND ul.api_key_id > 0 AND (f.released_at IS NULL OR ul.created_at > f.released_at)
	GROUP BY ul.api_key_id
	UNION ALL
	SELECT el.api_key_id AS key_id, 0 AS reqs, 0 AS toks, COUNT(*) AS errs
	FROM ops_error_logs el
	LEFT JOIN api_key_spend_guard_freezes f ON f.api_key_id = el.api_key_id
	WHERE el.created_at >= NOW() - ($1 * INTERVAL '1 minute')
		AND COALESCE(el.status_code, 0) >= 400 AND NOT COALESCE(el.is_business_limited, FALSE)
		AND el.api_key_id > 0 AND (f.released_at IS NULL OR el.created_at > f.released_at)
	GROUP BY el.api_key_id
), totals AS (
	SELECT key_id, SUM(reqs) AS reqs, SUM(toks) AS toks, SUM(errs) AS errs FROM recent GROUP BY key_id
)
SELECT ak.id, ak.name, ak.user_id, ak.status,
	COALESCE(t.reqs, 0), COALESCE(t.toks, 0), COALESCE(t.errs, 0),
	(f.api_key_id IS NOT NULL AND f.released_at IS NULL) AS frozen, f.released_at
FROM api_keys ak
LEFT JOIN totals t ON t.key_id = ak.id
LEFT JOIN api_key_spend_guard_freezes f ON f.api_key_id = ak.id
WHERE ak.deleted_at IS NULL AND (t.key_id IS NOT NULL OR (f.api_key_id IS NOT NULL AND f.released_at IS NULL))
ORDER BY frozen DESC, COALESCE(t.toks, 0) DESC, COALESCE(t.errs, 0) DESC, ak.id`

func (r *apiKeyRepository) ListSpendGuardOffenders(ctx context.Context, windowMinutes int) ([]service.SpendGuardOffender, error) {
	rows, err := r.sql.QueryContext(ctx, spendGuardOffendersSQL, windowMinutes)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := make([]service.SpendGuardOffender, 0)
	for rows.Next() {
		var item service.SpendGuardOffender
		var released sql.NullTime
		if err := rows.Scan(&item.APIKeyID, &item.Name, &item.UserID, &item.Status,
			&item.Requests, &item.Tokens, &item.Errors, &item.Frozen, &released); err != nil {
			return nil, err
		}
		item.TokensPerMin = float64(item.Tokens) / float64(windowMinutes)
		if total := item.Requests + item.Errors; total > 0 {
			item.ErrRate = float64(item.Errors) / float64(total)
		}
		if released.Valid {
			item.ReleasedAt = &released.Time
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (r *apiKeyRepository) ListSpendGuardEvents(ctx context.Context, limit int) ([]service.SpendGuardEvent, error) {
	rows, err := r.sql.QueryContext(ctx, `SELECT created_at, api_key_id, api_key_name, action, reason
		FROM spend_guard_events ORDER BY created_at DESC, id DESC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := make([]service.SpendGuardEvent, 0)
	for rows.Next() {
		var item service.SpendGuardEvent
		if err := rows.Scan(&item.At, &item.APIKeyID, &item.Name, &item.Action, &item.Reason); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}
