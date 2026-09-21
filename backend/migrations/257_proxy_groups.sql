CREATE TABLE IF NOT EXISTS proxy_groups (
    id BIGSERIAL PRIMARY KEY,
    name VARCHAR(100) NOT NULL UNIQUE CHECK (length(btrim(name)) > 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

ALTER TABLE proxies ADD COLUMN IF NOT EXISTS group_id BIGINT REFERENCES proxy_groups(id) ON DELETE SET NULL;
CREATE INDEX IF NOT EXISTS proxies_group_id_idx ON proxies (group_id);

-- extra 中的分组引用同样需要与删除互斥，避免账号保存时产生悬空引用。
CREATE OR REPLACE FUNCTION validate_account_random_proxy_group()
RETURNS TRIGGER AS $$
BEGIN
    IF NEW.deleted_at IS NULL
        AND lower(btrim(NEW.extra->>'proxy_mode')) = 'random'
        AND lower(btrim(NEW.extra->>'random_proxy_pool_scope')) = 'group' THEN
        PERFORM id FROM proxy_groups
        WHERE id::text = NEW.extra->>'random_proxy_group_id'
        FOR KEY SHARE;
        IF NOT FOUND THEN
            RAISE EXCEPTION 'proxy group not found'
                USING ERRCODE = '23503', CONSTRAINT = 'accounts_random_proxy_group_id_fkey';
        END IF;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS accounts_random_proxy_group_check ON accounts;
CREATE TRIGGER accounts_random_proxy_group_check
BEFORE INSERT OR UPDATE OF extra, deleted_at ON accounts
FOR EACH ROW EXECUTE FUNCTION validate_account_random_proxy_group();
