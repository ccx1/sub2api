ALTER TABLE shared_pool_settings
    ADD COLUMN IF NOT EXISTS default_priority INTEGER NOT NULL DEFAULT 50
    CHECK (default_priority >= 0 AND default_priority <= 100);

COMMENT ON COLUMN shared_pool_settings.default_priority IS
    '新增共享账号的默认调度优先级，数值越小越优先；已有账号由管理员单独调整';
