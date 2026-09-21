ALTER TABLE shared_pool_settings
    ADD COLUMN IF NOT EXISTS settlement_multiplier NUMERIC(12,6) NOT NULL DEFAULT 1 CHECK (settlement_multiplier BETWEEN 0 AND 100);

ALTER TABLE shared_pool_user_rates
    ADD COLUMN IF NOT EXISTS settlement_multiplier NUMERIC(12,6) CHECK (settlement_multiplier BETWEEN 0 AND 100);

-- 新授权采用独立结算；历史流水保留原来的实际扣费分成口径。
ALTER TABLE shared_pool_earnings
    ADD COLUMN IF NOT EXISTS base_amount NUMERIC(20,8),
    ADD COLUMN IF NOT EXISTS settlement_multiplier NUMERIC(12,6),
    ADD COLUMN IF NOT EXISTS settlement_amount NUMERIC(20,8),
    ADD COLUMN IF NOT EXISTS spread_amount NUMERIC(20,8);

ALTER TABLE shared_pool_earnings ADD CONSTRAINT shared_pool_settlement_snapshot_valid CHECK (
    (base_amount IS NULL AND settlement_multiplier IS NULL AND settlement_amount IS NULL AND spread_amount IS NULL)
    OR (base_amount IS NOT NULL AND settlement_multiplier IS NOT NULL AND settlement_amount IS NOT NULL AND spread_amount IS NOT NULL
        AND base_amount >= 0 AND settlement_multiplier BETWEEN 0 AND 100 AND settlement_amount >= 0
        AND spread_amount >= 0 AND billing_amount = settlement_amount + spread_amount)
);
