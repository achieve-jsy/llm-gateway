-- 061: 粘性路由全局默认 + 组级覆盖
-- 将 sticky_enabled 改为 nullable：NULL = 跟随全局默认，TRUE/FALSE = 组级显式覆盖

-- 1. 去掉 NOT NULL 约束，允许 NULL
ALTER TABLE model_groups ALTER COLUMN sticky_enabled DROP NOT NULL;
ALTER TABLE model_groups ALTER COLUMN sticky_enabled DROP DEFAULT;

-- 2. 将现有 false 改为 NULL（跟随全局），保留 true 为显式开启
UPDATE model_groups SET sticky_enabled = NULL WHERE sticky_enabled = FALSE;

-- 3. sticky_ttl_seconds 也改为 nullable，NULL = 跟随全局
ALTER TABLE model_groups ALTER COLUMN sticky_ttl_seconds DROP NOT NULL;
ALTER TABLE model_groups ALTER COLUMN sticky_ttl_seconds DROP DEFAULT;
UPDATE model_groups SET sticky_ttl_seconds = NULL WHERE sticky_ttl_seconds = 3600;

-- 4. 全局设置项
INSERT INTO system_configs (key, value, description, created_at, updated_at)
VALUES ('sticky_default_enabled', 'true', '全局粘性路由默认开关（组级未设置时生效）', NOW()::text, NOW()::text)
ON CONFLICT (key) DO NOTHING;

INSERT INTO system_configs (key, value, description, created_at, updated_at)
VALUES ('sticky_default_ttl_seconds', '300', '全局粘性路由默认 TTL 秒数（组级未设置时生效）', NOW()::text, NOW()::text)
ON CONFLICT (key) DO NOTHING;

-- 5. 更新索引（nullable 后原索引条件仍有效）
DROP INDEX IF EXISTS idx_model_groups_sticky_enabled;
CREATE INDEX idx_model_groups_sticky_enabled ON model_groups(sticky_enabled) WHERE sticky_enabled = TRUE AND deleted_at IS NULL;

COMMENT ON COLUMN model_groups.sticky_enabled IS 'Session stickiness: NULL=follow global default, TRUE=enabled, FALSE=disabled';
COMMENT ON COLUMN model_groups.sticky_ttl_seconds IS 'Sticky TTL seconds: NULL=follow global default';
