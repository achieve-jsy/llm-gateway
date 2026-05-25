-- 061 回滚：恢复 sticky_enabled 为 NOT NULL DEFAULT FALSE
UPDATE model_groups SET sticky_enabled = FALSE WHERE sticky_enabled IS NULL;
UPDATE model_groups SET sticky_ttl_seconds = 3600 WHERE sticky_ttl_seconds IS NULL;

ALTER TABLE model_groups ALTER COLUMN sticky_enabled SET NOT NULL;
ALTER TABLE model_groups ALTER COLUMN sticky_enabled SET DEFAULT FALSE;
ALTER TABLE model_groups ALTER COLUMN sticky_ttl_seconds SET NOT NULL;
ALTER TABLE model_groups ALTER COLUMN sticky_ttl_seconds SET DEFAULT 3600;

DELETE FROM system_configs WHERE key IN ('sticky_default_enabled', 'sticky_default_ttl_seconds');

DROP INDEX IF EXISTS idx_model_groups_sticky_enabled;
CREATE INDEX idx_model_groups_sticky_enabled ON model_groups(sticky_enabled) WHERE sticky_enabled = TRUE AND deleted_at IS NULL;
