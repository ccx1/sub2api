package repository

import (
	"context"
	"database/sql"
	"encoding/json"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

// 账号已在调用方事务加锁；按最新平台与类型验证，避免并发修改绕过档位限制。
func prepareSharedTierState(ctx context.Context, tx *sql.Tx, id int64, tier *string, dispatchPatch string) (string, error) {
	if tier == nil {
		return dispatchPatch, nil
	}
	var account service.Account
	var credentials, extra []byte
	if err := tx.QueryRowContext(ctx, `SELECT platform,type,credentials,extra,parent_account_id FROM accounts WHERE id=$1`, id).Scan(&account.Platform, &account.Type, &credentials, &extra, &account.ParentAccountID); err != nil {
		return "", err
	}
	value, err := service.NormalizeSharedPoolTierOverride(account.Platform, account.Type, *tier)
	if err != nil {
		return "", err
	}
	if len(credentials) > 0 {
		if err = json.Unmarshal(credentials, &account.Credentials); err != nil {
			return "", err
		}
	}
	if len(extra) > 0 {
		if err = json.Unmarshal(extra, &account.Extra); err != nil {
			return "", err
		}
	}
	if account.Extra == nil {
		account.Extra = make(map[string]any)
	}
	account.Extra[service.SharedPoolSubscriptionTierKey] = value
	// 调用方已锁定 shared_pool_accounts 归属，旧快照缺少冗余标记也仍是共享账号。
	account.Extra[service.SharedPoolOwnerKey] = int64(1)
	patch := make(map[string]any)
	if err = json.Unmarshal([]byte(dispatchPatch), &patch); err != nil {
		return "", err
	}
	patch[service.SharedPoolSubscriptionTierKey] = value
	if value == "" {
		patch[service.SharedPoolSubscriptionTierKey] = nil
	}
	if service.SharedPoolCodexTicketRequired(&account) {
		patch[service.OpenAICodexTicketEnabledExtraKey] = true
	}
	encoded, err := json.Marshal(patch)
	return string(encoded), err
}
