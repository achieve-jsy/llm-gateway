-- 在 users 表直接加 HR 扩展字段，避免额外 JOIN
ALTER TABLE users
    ADD COLUMN IF NOT EXISTS employee_id  VARCHAR(64),
    ADD COLUMN IF NOT EXISTS job_family   VARCHAR(128),
    ADD COLUMN IF NOT EXISTS job_category VARCHAR(128),
    ADD COLUMN IF NOT EXISTS job_title    VARCHAR(128);

CREATE INDEX IF NOT EXISTS idx_users_employee_id ON users(employee_id);
