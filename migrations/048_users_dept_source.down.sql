DROP TABLE IF EXISTS user_dept_conflicts;

ALTER TABLE users
    DROP COLUMN IF EXISTS dept_source,
    DROP COLUMN IF EXISTS dept_updated_by,
    DROP COLUMN IF EXISTS dept_updated_at;
