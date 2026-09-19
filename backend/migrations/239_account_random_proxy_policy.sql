-- 239_account_random_proxy_policy.sql
-- Random proxy mode and empty-pool policy are stored in accounts.extra.
-- Normalize historical dirty values first, then add DB-level whitelists.

UPDATE accounts
SET extra = COALESCE(extra, '{}'::jsonb) - 'proxy_mode'
WHERE extra IS NOT NULL
  AND (extra ? 'proxy_mode')
  AND NOT (
    jsonb_typeof(extra->'proxy_mode') = 'string'
    AND lower(btrim(extra->>'proxy_mode')) = 'random'
  );

UPDATE accounts
SET extra = jsonb_set(COALESCE(extra, '{}'::jsonb), '{proxy_mode}', '"random"'::jsonb, false)
WHERE extra IS NOT NULL
  AND (extra ? 'proxy_mode')
  AND extra->>'proxy_mode' <> 'random';

UPDATE accounts
SET extra = COALESCE(extra, '{}'::jsonb) - 'random_proxy_empty_pool_policy'
WHERE extra IS NOT NULL
  AND (extra ? 'random_proxy_empty_pool_policy')
  AND NOT (
    jsonb_typeof(extra->'random_proxy_empty_pool_policy') = 'string'
    AND lower(btrim(extra->>'random_proxy_empty_pool_policy')) IN ('reject', 'disable', 'direct')
  );

UPDATE accounts
SET extra = jsonb_set(
    COALESCE(extra, '{}'::jsonb),
    '{random_proxy_empty_pool_policy}',
    to_jsonb(lower(btrim(extra->>'random_proxy_empty_pool_policy'))),
    false
)
WHERE extra IS NOT NULL
  AND (extra ? 'random_proxy_empty_pool_policy')
  AND extra->>'random_proxy_empty_pool_policy' <> lower(btrim(extra->>'random_proxy_empty_pool_policy'));

DO $$ BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'chk_accounts_extra_proxy_mode') THEN
    ALTER TABLE accounts ADD CONSTRAINT chk_accounts_extra_proxy_mode
      CHECK (
        extra IS NULL
        OR NOT (extra ? 'proxy_mode')
        OR lower(btrim(extra->>'proxy_mode')) = 'random'
      ) NOT VALID;
  END IF;
END $$;

DO $$ BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'chk_accounts_random_proxy_empty_pool_policy') THEN
    ALTER TABLE accounts ADD CONSTRAINT chk_accounts_random_proxy_empty_pool_policy
      CHECK (
        extra IS NULL
        OR NOT (extra ? 'random_proxy_empty_pool_policy')
        OR lower(btrim(extra->>'random_proxy_empty_pool_policy')) IN ('reject', 'disable', 'direct')
      ) NOT VALID;
  END IF;
END $$;

ALTER TABLE accounts VALIDATE CONSTRAINT chk_accounts_extra_proxy_mode;
ALTER TABLE accounts VALIDATE CONSTRAINT chk_accounts_random_proxy_empty_pool_policy;
