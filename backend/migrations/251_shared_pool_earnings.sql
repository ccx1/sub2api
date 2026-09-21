CREATE TABLE IF NOT EXISTS shared_pool_earnings_transfers (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id),
    amount NUMERIC(20,8) NOT NULL CHECK (amount >= 0),
    balance_after NUMERIC(20,8) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS shared_pool_earnings (
    id BIGSERIAL PRIMARY KEY,
    request_id VARCHAR(255) NOT NULL,
    api_key_id BIGINT NOT NULL,
    consumer_user_id BIGINT NOT NULL REFERENCES users(id),
    owner_user_id BIGINT NOT NULL REFERENCES users(id),
    account_id BIGINT NOT NULL REFERENCES accounts(id),
    group_id BIGINT NOT NULL REFERENCES groups(id),
    billing_amount NUMERIC(20,8) NOT NULL CHECK (billing_amount > 0),
    platform_rate_bps INTEGER NOT NULL CHECK (platform_rate_bps BETWEEN 0 AND 10000),
    proxy_rate_bps INTEGER NOT NULL CHECK (proxy_rate_bps BETWEEN 0 AND 10000),
    uses_platform_proxy BOOLEAN NOT NULL,
    proxy_id BIGINT NULL,
    platform_amount NUMERIC(20,8) NOT NULL CHECK (platform_amount >= 0),
    owner_amount NUMERIC(20,8) NOT NULL CHECK (owner_amount >= 0),
    status VARCHAR(16) NOT NULL CHECK (status IN ('available', 'pending', 'transferred')),
    transfer_id BIGINT NULL REFERENCES shared_pool_earnings_transfers(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    released_at TIMESTAMPTZ NULL,
    release_reason VARCHAR(64) NULL,
    transferred_at TIMESTAMPTZ NULL,
    UNIQUE (request_id, api_key_id),
    CHECK (platform_rate_bps + proxy_rate_bps <= 10000),
    CHECK (platform_amount + owner_amount = billing_amount),
    CHECK (uses_platform_proxy OR proxy_rate_bps = 0),
    CHECK ((status = 'transferred') = (transfer_id IS NOT NULL))
);

CREATE INDEX IF NOT EXISTS idx_shared_pool_earnings_owner_created
    ON shared_pool_earnings (owner_user_id, created_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS idx_shared_pool_earnings_owner_available
    ON shared_pool_earnings (owner_user_id, id) WHERE status = 'available';
CREATE INDEX IF NOT EXISTS idx_shared_pool_earnings_account_created
    ON shared_pool_earnings (account_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_shared_pool_earnings_transfers_user
    ON shared_pool_earnings_transfers (user_id, created_at DESC);
