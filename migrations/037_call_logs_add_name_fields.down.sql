ALTER TABLE call_logs
    DROP COLUMN IF EXISTS user_name,
    DROP COLUMN IF EXISTS dept_name,
    DROP COLUMN IF EXISTS studio_name;