-- atlas-migrations/20260713000000_add_sticky_to_groups.sql
-- Feature: session sticky routing for model groups
-- 新增 model_groups 的 sticky_enabled 和 sticky_ttl_seconds 字段

ALTER TABLE model_groups ADD COLUMN IF NOT EXISTS sticky_enabled BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE model_groups ADD COLUMN IF NOT EXISTS sticky_ttl_seconds INTEGER NOT NULL DEFAULT 3600;

CREATE INDEX IF NOT EXISTS idx_model_groups_sticky_enabled
  ON model_groups(sticky_enabled)
  WHERE sticky_enabled = TRUE AND deleted_at IS NULL;

COMMENT ON COLUMN model_groups.sticky_enabled IS 'Enable session stickiness for this group (routes same session to same model)';
COMMENT ON COLUMN model_groups.sticky_ttl_seconds IS 'TTL in seconds for sticky session mappings (default 3600 = 1 hour)';
