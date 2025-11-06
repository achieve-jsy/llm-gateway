-- 015_add_model_check_config.sql
-- 模型检测独立配置字段（与渠道检测解耦）

ALTER TABLE health_check_configs
    ADD COLUMN IF NOT EXISTS model_check_enabled BOOLEAN NOT NULL DEFAULT true,
    ADD COLUMN IF NOT EXISTS model_check_interval_seconds INT NOT NULL DEFAULT 300,
    ADD COLUMN IF NOT EXISTS model_check_failure_threshold INT NOT NULL DEFAULT 3;

COMMENT ON COLUMN health_check_configs.model_check_enabled IS '是否启用模型自动检测（独立于渠道检测）';
COMMENT ON COLUMN health_check_configs.model_check_interval_seconds IS '模型检测间隔（秒），默认300=5分钟';
COMMENT ON COLUMN health_check_configs.model_check_failure_threshold IS '模型连续失败自动禁用阈值';
