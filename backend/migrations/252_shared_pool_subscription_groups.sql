ALTER TABLE shared_pool_settings
    ADD COLUMN IF NOT EXISTS subscription_group_ids JSONB NOT NULL DEFAULT '{}'::jsonb;

COMMENT ON COLUMN shared_pool_settings.subscription_group_ids IS '共享账号按平台及订阅档位自动分配的目标共享分组';
