CREATE TABLE IF NOT EXISTS shared_pool_auto_transfer_settings (
    user_id BIGINT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    enabled BOOLEAN NOT NULL DEFAULT FALSE,
    threshold NUMERIC(20,8) NOT NULL DEFAULT 1
        CHECK (threshold BETWEEN 0.00000001 AND 1000000000),
    daily_time CHAR(5) NOT NULL DEFAULT '00:00'
        CHECK (daily_time ~ '^([01][0-9]|2[0-3]):[0-5][0-9]$'),
    last_run_date DATE NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_shared_pool_auto_transfer_enabled_user
    ON shared_pool_auto_transfer_settings (user_id) WHERE enabled;
