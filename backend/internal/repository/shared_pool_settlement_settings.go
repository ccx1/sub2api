package repository

import (
	"context"
	"database/sql"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

func saveSharedSettlementSettings(ctx context.Context, tx *sql.Tx, settings *service.SharedPoolSettings) error {
	if err := service.ValidateSharedSettlementSettings(settings); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `UPDATE shared_pool_settings SET settlement_multiplier=$1 WHERE id=1`, settings.SettlementMultiplier)
	return err
}
