ALTER TABLE shared_pool_settings
    ADD COLUMN IF NOT EXISTS subscription_settlement_multipliers JSONB NOT NULL DEFAULT '{}'::jsonb;

COMMENT ON COLUMN shared_pool_settings.subscription_settlement_multipliers IS
    '管理员按平台及订阅档位设置的共享结算倍率；用户覆盖倍率优先';
