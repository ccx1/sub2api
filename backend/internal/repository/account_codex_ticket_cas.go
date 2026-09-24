package repository

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

const codexTicketCASKeyPrefix = "codex_turn_ticket:"

func (r *accountRepository) GetCodexTicketAccountSnapshot(ctx context.Context, id int64) (*service.Account, error) {
	return r.GetByID(ctx, id)
}

// 发布核对采集身份、代理配置和旧票；撤销只核对实际发送的票据版本。
func (r *accountRepository) CompareAndSwapCodexTicket(ctx context.Context, account *service.Account, model string, replacement any) (bool, error) {
	if r == nil {
		return false, service.ErrAccountNilInput
	}
	request, err := prepareCodexTicketCAS(account, model, replacement)
	if err != nil {
		return false, err
	}
	client := clientFromContext(ctx, r.client)
	if client == nil {
		return false, errors.New("account repository client is unavailable")
	}
	if dbent.TxFromContext(ctx) != nil {
		return executeCodexTicketCAS(ctx, client, request)
	}
	if account.ProxyID != nil && !request.revoke {
		return r.commitCodexTicketCAS(ctx, request)
	}
	changed, err := executeCodexTicketCAS(ctx, client, request)
	if changed && err == nil {
		r.syncSchedulerAccountSnapshot(ctx, account.ID)
	}
	return changed, err
}

func (r *accountRepository) commitCodexTicketCAS(ctx context.Context, request codexTicketCASRequest) (bool, error) {
	tx, err := r.client.Tx(ctx)
	if errors.Is(err, dbent.ErrTxStarted) {
		return executeCodexTicketCAS(ctx, r.client, request)
	}
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()
	changed, err := executeCodexTicketCAS(dbent.NewTxContext(ctx, tx), tx.Client(), request)
	if err != nil || !changed {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	r.syncSchedulerAccountSnapshot(ctx, request.account.ID)
	return true, nil
}

type codexTicketCASRequest struct {
	account          *service.Account
	args             []any
	publish          bool
	revoke           bool
	withInvalidation bool
}

func prepareCodexTicketCAS(account *service.Account, model string, replacement any) (codexTicketCASRequest, error) {
	request := codexTicketCASRequest{account: account}
	if account == nil || account.ID <= 0 {
		return request, service.ErrAccountNilInput
	}
	model = strings.TrimSpace(model)
	if model == "" {
		return request, errors.New("codex ticket model is required")
	}
	key := codexTicketCASKeyPrefix + model
	replacementJSON, err := json.Marshal(replacement)
	if err != nil {
		return request, err
	}
	var marker struct {
		Revoked json.RawMessage `json:"revoked"`
	}
	_ = json.Unmarshal(replacementJSON, &marker)
	if string(marker.Revoked) == "true" {
		return prepareCodexTicketRevocation(account, key, replacementJSON)
	}
	if (account.ProxyID == nil) != (account.Proxy == nil) ||
		(account.ProxyID != nil && (account.Proxy.ID != *account.ProxyID || *account.ProxyID <= 0)) {
		return request, errors.New("codex ticket proxy identity is incomplete")
	}
	config := codexTicketCASConfig(account.Extra)
	values := []any{replacement, account.Credentials, account.Extra[key], config}
	encoded := make([]string, len(values))
	for i, value := range values {
		payload, err := json.Marshal(value)
		if err != nil {
			return request, err
		}
		encoded[i] = string(payload)
	}
	var proxyID any
	// 随机出口只存在于本次请求中，不可拿运行时关联对比持久化 proxy_id。
	configured := account.ConfiguredProxySnapshot()
	if !account.IsRandomProxy() && configured.ProxyID != nil {
		proxyID = *configured.ProxyID
	}
	request.args = []any{key, encoded[0], account.ID, account.Platform, account.Type, encoded[1], proxyID, encoded[2], encoded[3]}
	request.publish = encoded[0] != "null" && string(marker.Revoked) != "true"
	return request, nil
}

func executeCodexTicketCAS(ctx context.Context, client *dbent.Client, request codexTicketCASRequest) (bool, error) {
	if request.revoke {
		query := codexTicketRevocationSQL
		if request.withInvalidation {
			query = codexTicketRevocationWithInvalidationSQL
		}
		result, err := client.ExecContext(ctx, query, request.args...)
		if err != nil {
			return false, err
		}
		affected, err := result.RowsAffected()
		return affected > 0 && err == nil, err
	}
	if codexTicketCASPublicationBlocked(request) {
		return false, nil
	}
	if request.account.ProxyID != nil {
		matches, err := lockAndMatchProbeProxyIdentity(ctx, client, request.account)
		if err != nil || !matches {
			return false, err
		}
	}
	if configured := request.account.ConfiguredProxySnapshot(); configured != request.account {
		matches, err := lockAndMatchProbeProxyIdentity(ctx, client, configured)
		if err != nil || !matches {
			return false, err
		}
	}
	// 等待代理行锁后再核对每日冷却边界；撤销旧票不受发布条件限制。
	if codexTicketCASPublicationBlocked(request) {
		return false, nil
	}
	result, err := client.ExecContext(ctx, codexTicketCASSQL, request.args...)
	if err != nil {
		return false, err
	}
	affected, err := result.RowsAffected()
	return affected > 0 && err == nil, err
}

func prepareCodexTicketRevocation(account *service.Account, key string, replacementJSON []byte) (codexTicketCASRequest, error) {
	request := codexTicketCASRequest{account: account, revoke: true}
	var used, expected codexTicketRevocationIdentity
	expectedJSON, err := json.Marshal(account.Extra[key])
	if err != nil {
		return request, err
	}
	if json.Unmarshal(replacementJSON, &used) != nil || json.Unmarshal(expectedJSON, &expected) != nil || !codexTicketRevocationHasCredentials(used) {
		return request, errors.New("codex ticket revocation identity does not match the sent ticket")
	}
	matched, ok := matchCodexTicketRevocationIdentity(used, expected)
	if !ok {
		return request, errors.New("codex ticket revocation capture time is required")
	}
	expected = matched
	request.args = []any{key, account.ID, account.Platform, account.Type, used.State, expected.CapturedAt}
	if len(used.Invalidation) > 0 && string(used.Invalidation) != "null" {
		payload, err := prepareCodexTicketInvalidation(key, used, expected)
		if err != nil {
			// 诊断不是撤销票据的前置条件，异常元数据不能阻止原有撤票行为。
			return request, nil
		}
		request.withInvalidation = true
		request.args = append(request.args, string(payload), service.OpenAICodexTicketInvalidationsKey, service.OpenAICodexTicketInvalidationsLimit)
	}
	return request, nil
}

func codexTicketCASPublicationBlocked(request codexTicketCASRequest) bool {
	if !request.publish {
		return false
	}
	account, now := request.account, time.Now()
	return account.Status != service.StatusActive || !account.Schedulable || !service.SharedPoolSharingAllowed(account) || account.IsInDailyCooldown(now) ||
		(account.AutoPauseOnExpired && account.ExpiresAt != nil && !now.Before(*account.ExpiresAt))
}

func codexTicketCASConfig(extra map[string]any) map[string]any {
	keys := []string{service.OpenAICodexTicketEnabledExtraKey, service.ProxyModeExtraKey,
		service.SharedPoolOwnerKey, service.SharedPoolEnabledKey, service.SharedPoolAdminDisabledKey,
		service.CodexTicketCredentialPolicyExtraKey,
		service.CodexTicketProxyModeExtraKey, service.CodexTicketProxyIDExtraKey, service.CodexTicketProxyStrategyExtraKey,
		service.RandomProxyEmptyPoolPolicyExtraKey, service.RandomProxyPoolScopeExtraKey,
		service.RandomProxyPoolIDsExtraKey, service.RandomProxyGroupIDExtraKey, service.RandomProxyMaxReuseMinutesExtraKey, service.RandomProxyRegionFallbackExtraKey, service.DailyCooldownExtraKey,
		"enable_tls_fingerprint", "tls_fingerprint_builtin", "tls_fingerprint_profile_id",
		"codex_fingerprint_mode", service.AntiDegradeMarkerExtraKey, service.AntiDegradationExtraKey}
	config := make(map[string]any, len(keys))
	for _, key := range keys {
		config[key] = extra[key]
	}
	return config
}

const codexTicketCASSQL = `UPDATE accounts
SET extra = CASE WHEN $2::jsonb = 'null'::jsonb
 THEN COALESCE(extra, '{}'::jsonb) - $1
 ELSE COALESCE(extra, '{}'::jsonb) || jsonb_build_object($1::text, $2::jsonb)
 END, updated_at = NOW()
WHERE id = $3 AND platform = $4 AND type = $5
 AND credentials = $6::jsonb
 AND proxy_id IS NOT DISTINCT FROM $7
 AND COALESCE(extra -> $1, 'null'::jsonb) = $8::jsonb
 AND NOT EXISTS (
  SELECT 1 FROM jsonb_each($9::jsonb) AS expected(key, value)
  WHERE COALESCE(extra -> expected.key, 'null'::jsonb) <> expected.value
 )
 AND ($2::jsonb = 'null'::jsonb OR $2::jsonb @> '{"revoked":true}'::jsonb OR (status = 'active' AND schedulable = true
  AND (NOT auto_pause_on_expired OR expires_at IS NULL OR expires_at > NOW())))
 AND deleted_at IS NULL`

// 撤票只归属实际发送的票据版本，不依赖可能已经刷新的 OAuth 凭据和出口配置。
var codexTicketRevocationSQL = `UPDATE accounts
SET extra = COALESCE(extra, '{}'::jsonb) ||
 jsonb_build_object($1::text, ` + codexTicketUpdatedInventorySQL(`'{"revoked":true}'::jsonb`) + `), updated_at = NOW()
WHERE id = $2 AND platform = $3 AND type = $4
 AND (` + codexTicketPrimaryMatchesSQL + ` OR ` + codexTicketStandbyMatchesSQL + ` OR ` + codexTicketReserveMatchesSQL + `)
 AND deleted_at IS NULL`
