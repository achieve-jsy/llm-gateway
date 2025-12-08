DROP INDEX IF EXISTS idx_users_employee_id;

ALTER TABLE users
    DROP COLUMN IF EXISTS employee_id,
    DROP COLUMN IF EXISTS job_family,
    DROP COLUMN IF EXISTS job_category,
    DROP COLUMN IF EXISTS job_title;
