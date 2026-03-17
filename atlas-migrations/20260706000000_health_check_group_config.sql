-- atlas-migrations/20260706000000_health_check_group_config.sql
-- Feature: 模型组检测独立配置
-- 新增 health_check_configs 模型组检测相关字段

ALTER TABLE health_check_configs ADD COLUMN IF NOT EXISTS group_check_enabled BOOLEAN NOT NULL DEFAULT true;
ALTER TABLE health_check_configs ADD COLUMN IF NOT EXISTS group_check_interval_seconds INTEGER NOT NULL DEFAULT 300;