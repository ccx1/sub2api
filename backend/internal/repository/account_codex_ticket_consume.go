package repository

import (
	"context"
	"errors"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

// codexTicketConsumptionClaimSQL 在同一行 UPDATE 中原子登记“用后即删”的领取记录：
// 指纹已存在时不更新任何行（其他实例已先领取）；否则剔除已失效条目、只保留最晚失效的
// $6 条，再写入本次指纹。只改账本键，不改票池键和 updated_at，因此不会与发布 CAS 冲突。
// 并发 UPDATE 在行锁释放后按新版本重新核对 WHERE，同一指纹最多一次成功。
const codexTicketConsumptionClaimSQL = `UPDATE accounts
SET extra = COALESCE(extra, '{}'::jsonb) || jsonb_build_object($2::text,
 COALESCE((
  SELECT jsonb_object_agg(kept.key, kept.value) FROM (
   SELECT entry.key, entry.value
   FROM jsonb_each(CASE WHEN jsonb_typeof(extra -> $2::text) = 'object' THEN extra -> $2::text ELSE '{}'::jsonb END) AS entry(key, value)
   WHERE jsonb_typeof(entry.value) = 'number' AND (entry.value #>> '{}')::numeric > $5::bigint
   ORDER BY (entry.value #>> '{}')::numeric DESC, entry.key
   LIMIT $6
  ) AS kept
 ), '{}'::jsonb) || jsonb_build_object($3::text, $4::bigint))
WHERE id = $1 AND deleted_at IS NULL
 AND (CASE WHEN jsonb_typeof(extra -> $2::text) = 'object' THEN extra -> $2::text ELSE '{}'::jsonb END) -> $3::text IS NULL`

// ClaimCodexTicketConsumption 在共享账本中登记一次领取；返回 false 表示该票已被领取（或账号不存在）。
// 账本只保存不可逆短指纹与失效时间，不含任何票据或 Cookie 内容。
func (r *accountRepository) ClaimCodexTicketConsumption(ctx context.Context, accountID int64, fingerprint string, expiresAt, now time.Time, limit int) (bool, error) {
	if r == nil {
		return false, service.ErrAccountNilInput
	}
	if accountID <= 0 || !validCodexTicketConsumptionFingerprint(fingerprint) {
		return false, errors.New("codex ticket consumption identity is invalid")
	}
	if !expiresAt.After(now) {
		return false, errors.New("codex ticket consumption expiry must be in the future")
	}
	client := clientFromContext(ctx, r.client)
	if client == nil {
		return false, errors.New("account repository client is unavailable")
	}
	result, err := client.ExecContext(ctx, codexTicketConsumptionClaimSQL,
		accountID, service.OpenAICodexTicketConsumedKey, fingerprint, expiresAt.Unix(), now.Unix(), max(limit-1, 1))
	if err != nil {
		return false, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	if affected != 1 {
		return false, nil
	}
	if dbent.TxFromContext(ctx) == nil {
		// 让其他实例在下一次读取账号快照时尽快看到领取记录，减少重复领取尝试并及时补票。
		r.syncSchedulerAccountSnapshotDetached(ctx, accountID)
	}
	return true, nil
}

func validCodexTicketConsumptionFingerprint(fingerprint string) bool {
	if len(fingerprint) == 0 || len(fingerprint) > 64 {
		return false
	}
	for _, r := range fingerprint {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}
