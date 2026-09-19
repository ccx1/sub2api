package repository

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

const accountProtectionEnabledSQL = "COALESCE(extra -> 'anti_degradation' = 'true'::jsonb, extra #> '{anti_degrade,enabled}' = 'true'::jsonb, false)"

// An atomic expression preserves the committed state even when partial updates
// come from imports or asynchronous credential refreshes with stale snapshots.
func preserveProtectionExtraSQL(ctx context.Context, expression string) string {
	expression = "((" + expression + ") - 'random_proxy_last_used') || CASE WHEN extra ? 'random_proxy_last_used' THEN jsonb_build_object('random_proxy_last_used', extra -> 'random_proxy_last_used') ELSE '{}'::jsonb END"
	if service.ProtectionManagedWrite(ctx) {
		return expression
	}
	base := "ARRAY['anti_degradation','protection_scope','anti_degrade']::text[]"
	mode1 := "ARRAY['anti_degradation','protection_scope','anti_degrade','codex_fingerprint_mode','enable_tls_fingerprint','tls_fingerprint_builtin','tls_fingerprint_profile_id']::text[]"
	modes := "'" + strings.Join(service.RegisteredProtectionModes(), "','") + "'"
	staleEnable := "(extra -> 'anti_degradation' = 'false'::jsonb AND (" + expression + ") #> '{anti_degrade,enabled}' = 'true'::jsonb)"
	keys := "(CASE WHEN (COALESCE(extra #>> '{anti_degrade,enabled}', 'true') <> 'false' AND (extra #> '{anti_degrade,policy_version}' IS NOT NULL OR extra #>> '{anti_degrade,mode}' IN (" + modes + "))) OR " + staleEnable + " THEN " + mode1 + " ELSE " + base + " END)"
	return "((" + expression + ") - " + keys + ") || COALESCE((SELECT jsonb_object_agg(key,value) FROM jsonb_each(COALESCE(extra,'{}'::jsonb)) WHERE key = ANY(" + keys + ")), '{}'::jsonb)"
}

func protectedConcurrencySQL(expression string) string {
	return "CASE WHEN " + accountProtectionEnabledSQL + " AND " + expression + " <= 0 THEN 16 ELSE " + expression + " END"
}

func preserveLockedAccountProtection(ctx context.Context, client *dbent.Client, a *service.Account) error {
	// lockAndMergeAccountProbeExtra already holds FOR NO KEY UPDATE in this
	// transaction. Re-read just extra under that lock to prevent stale writes.
	expected, compareVersion := service.GetProtectionWriteExpectation(ctx)
	query := "SELECT extra FROM accounts WHERE id = $1 AND deleted_at IS NULL"
	if compareVersion {
		query = "SELECT extra, updated_at FROM accounts WHERE id = $1 AND deleted_at IS NULL"
	}
	rows, err := client.QueryContext(ctx, query, a.ID)
	if err != nil {
		return err
	}
	defer rows.Close()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return err
		}
		return service.ErrAccountNotFound
	}
	var raw []byte
	var revision time.Time
	var scanErr error
	if compareVersion {
		scanErr = rows.Scan(&raw, &revision)
	} else {
		scanErr = rows.Scan(&raw)
	}
	if scanErr != nil {
		return scanErr
	}
	if compareVersion && (expected.AccountID != a.ID || !expected.UpdatedAt.Equal(revision)) {
		return service.ErrProtectionConflict
	}
	var extra map[string]any
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &extra); err != nil {
			return err
		}
	}
	current := &service.Account{Platform: a.Platform, Type: a.Type, Extra: extra}
	a.Extra = service.PreserveAccountProtection(ctx, current, a.Extra)
	service.BoundAccountProtectionConcurrency(a)
	if err := service.ValidateAccountProtectionConfiguration(a); err != nil {
		return err
	}
	return rows.Err()
}
