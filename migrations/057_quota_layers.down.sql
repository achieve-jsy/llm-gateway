ALTER TABLE users DROP COLUMN IF EXISTS quota_rpm, DROP COLUMN IF EXISTS quota_max_concurrency;
ALTER TABLE model_groups DROP COLUMN IF EXISTS quota_rpm, DROP COLUMN IF EXISTS quota_max_concurrency;
ALTER TABLE models DROP COLUMN IF EXISTS quota_rpm, DROP COLUMN IF EXISTS quota_max_concurrency;
ALTER TABLE api_keys DROP COLUMN IF EXISTS quota_rpm, DROP COLUMN IF EXISTS quota_max_concurrency;
