ALTER TABLE groups ADD COLUMN IF NOT EXISTS is_shared_pool BOOLEAN NOT NULL DEFAULT FALSE;

COMMENT ON COLUMN groups.is_shared_pool IS '共享池分组：有可调度共享账号时向所有用户公开';
