package repository

import (
	"context"
	"database/sql"
	"strconv"
)

// 已删除账号只让出凭证占位，历史归属、停用标记和收益记录仍留在旧账号。
// 必须与新账号创建共用事务，后续写入失败时旧占位也一并回滚。
func prepareSharedAccountReimport(ctx context.Context, exec sqlExecutor, fingerprint string) error {
	rows, err := exec.QueryContext(ctx, `SELECT s.account_id,a.deleted_at
		FROM shared_pool_accounts s JOIN accounts a ON a.id=s.account_id
		WHERE s.credential_fingerprint=$1 FOR UPDATE OF a,s`, fingerprint)
	if err != nil {
		return err
	}
	defer rows.Close()
	if !rows.Next() {
		return rows.Err()
	}
	var accountID int64
	var deletedAt sql.NullTime
	if err = rows.Scan(&accountID, &deletedAt); err != nil {
		return err
	}
	if err = rows.Close(); err != nil {
		return err
	}
	if err = rows.Err(); err != nil {
		return err
	}
	if !deletedAt.Valid {
		return serviceSharedConflict()
	}
	// 真实指纹为十六进制 SHA256；独立前缀不会与有效凭证冲突。
	retired := "deleted:" + strconv.FormatInt(accountID, 10)
	_, err = exec.ExecContext(ctx, `UPDATE shared_pool_accounts
		SET credential_fingerprint=$2,updated_at=NOW() WHERE account_id=$1`, accountID, retired)
	return err
}
