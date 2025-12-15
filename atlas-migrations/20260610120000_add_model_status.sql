-- atlas-migrations/20260610120000_add_model_status.sql
-- Feature: 001-model-auto-recovery 模型自动恢复
-- 新增 models.status, models.consecutive_recovery_successes 字段
-- 新增 model_status_logs 表
-- 新增 health_check_configs 恢复相关字段

-- 1. Model 表新增字段
ALTER TABLE models ADD COLUMN status INTEGER NOT NULL DEFAULT 1;
ALTER TABLE models ADD COLUMN consecutive_recovery_successes INTEGER NOT NULL DEFAULT 0;

-- 数据修复：将已禁用的模型标记为手动禁用
UPDATE models SET status = 2 WHERE enabled = false;

-- 2. 新增模型状态变更日志表
CREATE TABLE model_status_logs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    model_id UUID NOT NULL,
    model_name VARCHAR(200) NOT NULL,
    provider_id UUID NOT NULL,
    provider_name VARCHAR(200) NOT NULL,
    change_type VARCHAR(20) NOT NULL,
    reason VARCHAR(500) NOT NULL DEFAULT '',
    error_message TEXT DEFAULT '',
    operator_id UUID,
    operator_name VARCHAR(128) DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_model_status_logs_model_id ON model_status_logs(model_id);
CREATE INDEX idx_model_status_logs_created_at ON model_status_logs(created_at);
CREATE INDEX idx_model_status_logs_change_type ON model_status_logs(change_type);

-- 3. HealthCheckConfig 表新增字段
ALTER TABLE health_check_configs ADD COLUMN model_recovery_enabled BOOLEAN NOT NULL DEFAULT true;
ALTER TABLE health_check_configs ADD COLUMN model_recovery_success_threshold INTEGER NOT NULL DEFAULT 2;