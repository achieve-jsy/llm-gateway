-- Remove sticky routing configuration from model_groups table
DROP INDEX IF EXISTS idx_model_groups_sticky_enabled;

ALTER TABLE model_groups
DROP COLUMN IF EXISTS sticky_ttl_seconds,
DROP COLUMN IF EXISTS sticky_enabled;
