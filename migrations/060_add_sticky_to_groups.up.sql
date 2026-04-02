-- Add sticky routing configuration to model_groups table
ALTER TABLE model_groups
ADD COLUMN sticky_enabled BOOLEAN NOT NULL DEFAULT FALSE,
ADD COLUMN sticky_ttl_seconds INTEGER NOT NULL DEFAULT 3600;

-- Add index for enabled groups with sticky routing
CREATE INDEX idx_model_groups_sticky_enabled
ON model_groups(sticky_enabled)
WHERE sticky_enabled = TRUE AND deleted_at IS NULL;

COMMENT ON COLUMN model_groups.sticky_enabled IS 'Enable session stickiness for this group (routes same session to same model)';
COMMENT ON COLUMN model_groups.sticky_ttl_seconds IS 'TTL in seconds for sticky session mappings (default 3600 = 1 hour)';
