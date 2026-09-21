package repository

import (
	"context"
	"database/sql"
	"errors"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	dbgroup "github.com/Wei-Shaw/sub2api/ent/group"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

type sharedPoolRepository struct {
	client   *dbent.Client
	db       *sql.DB
	accounts *accountRepository
}

func NewSharedPoolRepository(client *dbent.Client, db *sql.DB, cache service.SchedulerCache) service.SharedPoolRepository {
	return &sharedPoolRepository{client: client, db: db, accounts: newAccountRepositoryWithSQL(client, db, cache)}
}

func (r *sharedPoolRepository) GetSharedAccount(ctx context.Context, ownerID, id int64) (*service.SharedPoolAccountRecord, error) {
	var item service.SharedPoolAccountRecord
	err := r.db.QueryRowContext(ctx, `SELECT s.account_id,s.owner_user_id,u.email,s.enabled,s.admin_disabled,s.assigned
        FROM shared_pool_accounts s JOIN accounts a ON a.id=s.account_id JOIN users u ON u.id=s.owner_user_id
        WHERE s.account_id=$1 AND ($2=0 OR s.owner_user_id=$2) AND a.deleted_at IS NULL`, id, ownerID).
		Scan(&item.AccountID, &item.OwnerUserID, &item.OwnerEmail, &item.Enabled, &item.AdminDisabled, &item.Assigned)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrSharedPoolAccountNotFound
	}
	return &item, err
}

func (r *sharedPoolRepository) ListSharedAccounts(ctx context.Context, ownerID int64, page, size int) ([]service.SharedPoolAccountRecord, int64, error) {
	var total int64
	search := service.SharedPoolSearch(ctx)
	err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM shared_pool_accounts s JOIN accounts a ON a.id=s.account_id JOIN users u ON u.id=s.owner_user_id
        WHERE ($1=0 OR s.owner_user_id=$1) AND a.deleted_at IS NULL AND ($2='' OR a.name ILIKE '%'||$2||'%' OR u.email ILIKE '%'||$2||'%')`, ownerID, search).Scan(&total)
	if err != nil {
		return nil, 0, err
	}
	rows, err := r.db.QueryContext(ctx, `SELECT s.account_id,s.owner_user_id,u.email,s.enabled,s.admin_disabled
        FROM shared_pool_accounts s JOIN accounts a ON a.id=s.account_id JOIN users u ON u.id=s.owner_user_id
        WHERE ($1=0 OR s.owner_user_id=$1) AND a.deleted_at IS NULL AND ($4='' OR a.name ILIKE '%'||$4||'%' OR u.email ILIKE '%'||$4||'%')
        ORDER BY s.account_id DESC LIMIT $2 OFFSET $3`, ownerID, size, (int64(page)-1)*int64(size), search)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := make([]service.SharedPoolAccountRecord, 0)
	for rows.Next() {
		var item service.SharedPoolAccountRecord
		if err = rows.Scan(&item.AccountID, &item.OwnerUserID, &item.OwnerEmail, &item.Enabled, &item.AdminDisabled); err != nil {
			return nil, 0, err
		}
		items = append(items, item)
	}
	return items, total, rows.Err()
}

func (r *sharedPoolRepository) CreateSharedAccount(ctx context.Context, a *service.Account, ownerID int64, fingerprint string) error {
	tx, err := r.client.Tx(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, id := range a.GroupIDs {
		g, e := tx.Group.Query().Where(dbgroup.IDEQ(id), dbgroup.DeletedAtIsNil()).ForShare().Only(ctx)
		if e != nil || !sharedAccountDefaultGroupAllowed(groupEntityToService(g), a.Platform, a.Type, service.SharedPoolDispatchConsented(a)) {
			return infraerrors.BadRequest("INVALID_SHARED_GROUP", "默认共享分组不可用或与账号不兼容")
		}
	}
	if err = createAccountRecord(ctx, tx.Client(), a); err != nil {
		return err
	}
	_, err = tx.Client().ExecContext(ctx, `INSERT INTO shared_pool_accounts(account_id,owner_user_id,credential_fingerprint,enabled,assigned) VALUES($1,$2,$3,$4,$5)`, a.ID, ownerID, fingerprint, a.Extra[service.SharedPoolEnabledKey], len(a.GroupIDs) > 0)
	if dbent.IsConstraintError(err) || isSharedPoolUniqueConflict(err) {
		return serviceSharedConflict()
	}
	if err != nil {
		return err
	}
	for _, id := range a.GroupIDs {
		if _, err = tx.AccountGroup.Create().SetAccountID(a.ID).SetGroupID(id).SetPriority(a.Priority).Save(ctx); err != nil {
			return err
		}
	}
	if err = enqueueSchedulerOutbox(ctx, tx.Client(), service.SchedulerOutboxEventAccountChanged, &a.ID, nil, buildSchedulerGroupPayload(a.GroupIDs)); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	r.accounts.syncSchedulerAccountSnapshot(ctx, a.ID)
	return nil
}

func (r *sharedPoolRepository) RemoveSharedAccount(ctx context.Context, ownerID, id int64) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = lockSharedAccount(ctx, tx, ownerID, id); err != nil {
		return err
	}
	groupIDs, err := sharedAccountGroupIDs(ctx, tx, id)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE accounts SET deleted_at=NOW(),schedulable=FALSE,updated_at=NOW() WHERE id=$1`, id)
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE shared_pool_accounts SET enabled=FALSE,updated_at=NOW() WHERE account_id=$1`, id); err != nil {
		return err
	}
	if err = enqueueSchedulerOutbox(ctx, tx, service.SchedulerOutboxEventAccountChanged, &id, nil, buildSchedulerGroupPayload(groupIDs)); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	r.accounts.syncSchedulerAccountSnapshot(ctx, id)
	return nil
}

func lockSharedAccount(ctx context.Context, tx *sql.Tx, ownerID, id int64) error {
	var exists int64
	err := tx.QueryRowContext(ctx, `SELECT a.id FROM accounts a JOIN shared_pool_accounts s ON s.account_id=a.id
        WHERE a.id=$1 AND ($2=0 OR s.owner_user_id=$2) AND a.deleted_at IS NULL FOR UPDATE OF a,s`, id, ownerID).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return service.ErrSharedPoolAccountNotFound
	}
	return err
}
