CREATE TABLE IF NOT EXISTS shared_pool_settings (
    id SMALLINT PRIMARY KEY CHECK (id = 1),
    platform_rate_bps INTEGER NOT NULL DEFAULT 2000 CHECK (platform_rate_bps BETWEEN 0 AND 10000),
    proxy_rate_bps INTEGER NOT NULL DEFAULT 100 CHECK (proxy_rate_bps BETWEEN 0 AND 10000),
    max_concurrency INTEGER NOT NULL DEFAULT 10 CHECK (max_concurrency BETWEEN 1 AND 100),
    default_group_ids JSONB NOT NULL DEFAULT '{}'::jsonb,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK (platform_rate_bps + proxy_rate_bps <= 10000)
);
INSERT INTO shared_pool_settings(id) VALUES (1) ON CONFLICT DO NOTHING;

CREATE TABLE IF NOT EXISTS shared_pool_user_rates (
    user_id BIGINT PRIMARY KEY REFERENCES users(id),
    platform_rate_bps INTEGER CHECK (platform_rate_bps BETWEEN 0 AND 10000),
    proxy_rate_bps INTEGER CHECK (proxy_rate_bps BETWEEN 0 AND 10000),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS shared_pool_accounts (
    account_id BIGINT PRIMARY KEY REFERENCES accounts(id),
    owner_user_id BIGINT NOT NULL REFERENCES users(id),
    enabled BOOLEAN NOT NULL DEFAULT FALSE,
    assigned BOOLEAN NOT NULL DEFAULT FALSE,
    admin_disabled BOOLEAN NOT NULL DEFAULT FALSE,
    credential_fingerprint VARCHAR(64) NOT NULL UNIQUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS shared_pool_accounts_owner_idx ON shared_pool_accounts(owner_user_id, account_id);

-- 用户自带代理不能成为平台随机池的候选；所有随机选择路径均排除本表。
CREATE TABLE IF NOT EXISTS shared_pool_proxies (
    proxy_id BIGINT PRIMARY KEY REFERENCES proxies(id),
    owner_user_id BIGINT NOT NULL REFERENCES users(id),
    fingerprint VARCHAR(64) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(owner_user_id, fingerprint)
);
