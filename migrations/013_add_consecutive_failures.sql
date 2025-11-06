-- 013_add_consecutive_failures.sql
-- 为 providers 表新增连续失败计数字段

ALTER TABLE providers ADD COLUMN IF NOT EXISTS consecutive_failures INT NOT NULL DEFAULT 0;

COMMENT ON COLUMN providers.consecutive_failures IS '健康检测连续失败次数，成功后重置为 0';
