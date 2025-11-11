-- 014_add_check_mode.sql
-- 健康检测配置增加 check_mode 字段

ALTER TABLE health_check_configs
    ADD COLUMN IF NOT EXISTS check_mode VARCHAR(20) NOT NULL DEFAULT 'connectivity';

COMMENT ON COLUMN health_check_configs.check_mode IS '检测模式：connectivity（仅连通性）/ model_forward（模型转发）/ both（两者都做）';
