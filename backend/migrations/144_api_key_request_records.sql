-- Store user-submitted API Key request input excerpts for risk-control review.
-- This table is intentionally separate from content_moderation_logs so daily
-- request records do not pollute audit hit/non-hit logs.
CREATE TABLE IF NOT EXISTS api_key_request_records (
    id             BIGSERIAL PRIMARY KEY,
    request_id     VARCHAR(128) NOT NULL DEFAULT '',
    user_id        BIGINT REFERENCES users(id) ON DELETE SET NULL,
    user_email     VARCHAR(255) NOT NULL DEFAULT '',
    api_key_id     BIGINT REFERENCES api_keys(id) ON DELETE SET NULL,
    api_key_name   VARCHAR(100) NOT NULL DEFAULT '',
    group_id       BIGINT REFERENCES groups(id) ON DELETE SET NULL,
    group_name     VARCHAR(255) NOT NULL DEFAULT '',
    endpoint       VARCHAR(128) NOT NULL DEFAULT '',
    provider       VARCHAR(64) NOT NULL DEFAULT '',
    model          VARCHAR(255) NOT NULL DEFAULT '',
    input_excerpt  TEXT NOT NULL DEFAULT '',
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_api_key_request_records_created_at ON api_key_request_records(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_api_key_request_records_group_created_at ON api_key_request_records(group_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_api_key_request_records_user_created_at ON api_key_request_records(user_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_api_key_request_records_api_key_created_at ON api_key_request_records(api_key_id, created_at DESC);
