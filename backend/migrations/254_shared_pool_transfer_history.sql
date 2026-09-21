-- 只补历史转账的展示记录，不重复增加用户余额、充值金额或邀请返利。
DO $$
BEGIN
    -- 既有来源编号必须指向同一转账；异常碰撞直接拒绝整批补录。
    IF EXISTS (
        SELECT 1
        FROM shared_pool_earnings_transfers t
        JOIN redeem_codes r ON r.code = 'SHARED-' || t.id::text
        WHERE t.amount > 0
          AND (r.type IS DISTINCT FROM 'shared_pool_transfer'
            OR r.status IS DISTINCT FROM 'used'
            OR r.used_by IS DISTINCT FROM t.user_id
            OR r.value IS DISTINCT FROM t.amount
            OR r.used_at IS DISTINCT FROM t.created_at
            OR r.created_at IS DISTINCT FROM t.created_at)
    ) THEN
        RAISE EXCEPTION 'shared pool transfer history source conflict';
    END IF;

    INSERT INTO redeem_codes (code, type, value, status, used_by, used_at, created_at, notes)
    SELECT 'SHARED-' || t.id::text, 'shared_pool_transfer', t.amount, 'used',
           t.user_id, t.created_at, t.created_at, '共享收益转入余额'
    FROM shared_pool_earnings_transfers t
    WHERE t.amount > 0
      AND NOT EXISTS (SELECT 1 FROM redeem_codes r WHERE r.code = 'SHARED-' || t.id::text);
END $$;
