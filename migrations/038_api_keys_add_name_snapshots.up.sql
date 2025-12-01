-- 038_api_keys_add_name_snapshots.up.sql
ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS user_name   VARCHAR(128);
ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS dept_name   VARCHAR(128);
ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS studio_name VARCHAR(128);