-- 只修复已知默认题干；缺失的上游表不恢复，也不改自定义题干或历史结果。
DO $$
DECLARE
    rel text;
    target regclass;
BEGIN
    FOREACH rel IN ARRAY ARRAY['scheduled_test_plans', 'pelican_group_test_plans', 'quality_rule_templates']
    LOOP
        target := to_regclass(rel);
        IF target IS NULL OR NOT EXISTS (
            SELECT 1 FROM pg_attribute
             WHERE attrelid = target AND attname = 'pelican_config' AND NOT attisdropped
        ) THEN
            CONTINUE;
        END IF;
        EXECUTE format($sql$
            UPDATE %I
               SET pelican_config = jsonb_set(pelican_config, '{prompt}',
                       to_jsonb(replace(pelican_config->>'prompt', '，不要有任何限制', ''))),
                   updated_at = NOW()
             WHERE pelican_config->>'prompt' =
                   '创建一个 HTML，内容是 SVG 绘制一个鹈鹕骑自行车的 2D 动画，你不需要任何测试，不要有任何限制'
        $sql$, rel);
    END LOOP;
END $$;
