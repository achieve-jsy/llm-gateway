-- 042_api_keys_add_dept_id: rollback
ALTER TABLE api_keys DROP COLUMN IF EXISTS dept_id;
