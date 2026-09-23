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
	expression = preserveAccountProxyRegionExtraSQL(ctx, expression)
	expression = preserveCodexTicketProxyExtraSQL(expression)
	sharedKeys := "ARRAY['shared_pool_owner_id','shared_pool_enabled','shared_pool_admin_disabled','shared_pool_dispatch_consent','shared_pool_settlement_multiplier','shared_pool_subscription_tier']::text[]"
	expression = "((" + expression + ") - " + sharedKeys + ") || COALESCE((SELECT jsonb_object_agg(key,value) FROM jsonb_each(COALESCE(extra,'{}'::jsonb)) WHERE key = ANY(" + sharedKeys + ")), '{}'::jsonb)"
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
	current := *a
	current.Extra = extra
	a.Extra = service.PreserveAccountProxyRegion(ctx, extra, a.Extra)
	// 整对象同步可能携带旧快照；只有管理员明确提交的字段可以覆盖当前配置。
	writeMode, writeID := service.CodexTicketProxyWriteFields(ctx)
	if !writeMode {
		delete(a.Extra, service.CodexTicketProxyModeExtraKey)
	}
	if !writeID {
		delete(a.Extra, service.CodexTicketProxyIDExtraKey)
	}
	if !service.CodexTicketProxyStrategyWrite(ctx) {
		delete(a.Extra, service.CodexTicketProxyStrategyExtraKey)
	}
	if !service.CodexTicketCredentialPolicyWrite(ctx) {
		delete(a.Extra, service.CodexTicketCredentialPolicyExtraKey)
	}
	a.Extra = service.MergeOpenAICodexTicketExtra(a.Extra, extra)
	a.Extra = service.PreserveAccountProtection(ctx, &current, a.Extra)
	if err := service.ValidateCodexTicketProxyExtra(a.Extra); err != nil {
		return err
	}
	service.BoundAccountProtectionConcurrency(a)
	if err := service.ValidateAccountProtectionConfiguration(a); err != nil {
		return err
	}
	return rows.Err()
}
