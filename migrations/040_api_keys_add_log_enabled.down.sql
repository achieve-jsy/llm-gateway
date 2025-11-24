-- 040 rollback
ALTER TABLE api_keys DROP COLUMN IF EXISTS log_enabled;
ALTER TABLE api_keys DROP COLUMN IF EXISTS studio_id;
