-- 冻结状态独立于用户可编辑的 API Key 状态，重启和统计窗口滚动均不丢失。
CREATE TABLE IF NOT EXISTS api_key_spend_guard_freezes (
    api_key_id BIGINT PRIMARY KEY REFERENCES api_keys(id) ON DELETE CASCADE,
    frozen_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    reason TEXT NOT NULL,
    released_at TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS idx_spend_guard_active_freezes
    ON api_key_spend_guard_freezes(frozen_at DESC) WHERE released_at IS NULL;

CREATE TABLE IF NOT EXISTS spend_guard_events (
    id BIGSERIAL PRIMARY KEY,
    api_key_id BIGINT NOT NULL,
    api_key_name VARCHAR(100) NOT NULL DEFAULT '',
    action VARCHAR(20) NOT NULL CHECK (action IN ('frozen', 'unfrozen')),
    reason TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_spend_guard_events_created_at
    ON spend_guard_events(created_at DESC, id DESC);

-- 不把历史 disabled 状态推断为 spend guard 冻结；它也可能来自管理员手动停用。
-- 只有运行期明确触发 spend guard 时才写入冻结表。

CREATE OR REPLACE FUNCTION enforce_api_key_spend_guard_freeze()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.status = 'active' AND EXISTS (
        SELECT 1 FROM api_key_spend_guard_freezes
        WHERE api_key_id = NEW.id AND released_at IS NULL
    ) THEN
        RAISE EXCEPTION 'API key is frozen by spend guard; administrator unfreeze required'
            USING ERRCODE = '23514', CONSTRAINT = 'api_key_spend_guard_frozen';
    END IF;
    RETURN NEW;
END;
$$;
DROP TRIGGER IF EXISTS trg_api_key_spend_guard_freeze ON api_keys;
CREATE TRIGGER trg_api_key_spend_guard_freeze
BEFORE UPDATE OF status ON api_keys
FOR EACH ROW EXECUTE FUNCTION enforce_api_key_spend_guard_freeze();

-- 策略开关亦属鉴权快照，复用现有持久化失效队列保证跨实例更新。
CREATE OR REPLACE FUNCTION enqueue_group_security_policy_auth_invalidation()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    IF OLD.security_policy_enabled IS DISTINCT FROM NEW.security_policy_enabled
       OR OLD.security_policy_mode IS DISTINCT FROM NEW.security_policy_mode
       OR OLD.security_policy_email_enabled IS DISTINCT FROM NEW.security_policy_email_enabled THEN
        INSERT INTO auth_cache_invalidation_outbox(cache_key)
        SELECT encode(sha256(convert_to(k.key, 'UTF8')), 'hex') FROM api_keys k
        WHERE k.group_id = NEW.id AND k.deleted_at IS NULL AND k.key <> '';
    END IF;
    RETURN NEW;
END;
$$;
DROP TRIGGER IF EXISTS trg_group_security_policy_auth_invalidation ON groups;
CREATE TRIGGER trg_group_security_policy_auth_invalidation
AFTER UPDATE OF security_policy_enabled, security_policy_mode, security_policy_email_enabled ON groups
FOR EACH ROW EXECUTE FUNCTION enqueue_group_security_policy_auth_invalidation();
