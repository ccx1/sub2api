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
	var platform, kind string
	if err := tx.QueryRowContext(ctx, `SELECT platform,type FROM accounts WHERE id=$1`, id).Scan(&platform, &kind); err != nil {
		return "", err
	}
	value, err := service.NormalizeSharedPoolTierOverride(platform, kind, *tier)
	if err != nil {
		return "", err
	}
	patch := make(map[string]any)
	if err = json.Unmarshal([]byte(dispatchPatch), &patch); err != nil {
		return "", err
	}
	patch[service.SharedPoolSubscriptionTierKey] = value
	if value == "" {
		patch[service.SharedPoolSubscriptionTierKey] = nil
	}
	encoded, err := json.Marshal(patch)
	return string(encoded), err
}
