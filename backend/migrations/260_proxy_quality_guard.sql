-- 代理池质量巡检：记录自动禁用轮数与冷却期限，重启后仍按原轮数继续恢复或删除。
CREATE TABLE IF NOT EXISTS proxy_quality_guard_states (
    proxy_id BIGINT PRIMARY KEY,
    state VARCHAR(20) NOT NULL DEFAULT 'active',
    rounds INT NOT NULL DEFAULT 0,
    consecutive_failures INT NOT NULL DEFAULT 0,
    disabled_until TIMESTAMPTZ,
    last_checked_at TIMESTAMPTZ,
    last_success_at TIMESTAMPTZ,
    last_failed_at TIMESTAMPTZ,
    last_score INT,
    last_grade VARCHAR(4) NOT NULL DEFAULT '',
    last_error TEXT NOT NULL DEFAULT '',
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_proxy_quality_guard_states_disabled
    ON proxy_quality_guard_states(disabled_until) WHERE state = 'disabled';

CREATE TABLE IF NOT EXISTS proxy_quality_guard_events (
    id BIGSERIAL PRIMARY KEY,
    proxy_id BIGINT NOT NULL,
    proxy_name VARCHAR(100) NOT NULL DEFAULT '',
    action VARCHAR(20) NOT NULL,
    round INT NOT NULL DEFAULT 0,
    reason TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_proxy_quality_guard_events_created_at
    ON proxy_quality_guard_events(created_at DESC, id DESC);
