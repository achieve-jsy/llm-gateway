-- 037_call_logs_add_name_fields.up.sql
-- 宽表快照：user_name / dept_name / studio_name 在采集时写入，避免查询 JOIN

ALTER TABLE call_logs
    ADD COLUMN IF NOT EXISTS user_name   VARCHAR(128),
    ADD COLUMN IF NOT EXISTS dept_name   VARCHAR(128),
    ADD COLUMN IF NOT EXISTS studio_name VARCHAR(128);